package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/config"
)

type voiceTransport struct {
	*fakeTransport
	downloads int
	size      int64
}

func (v *voiceTransport) GetFile(context.Context, string) (TelegramFile, error) {
	return TelegramFile{FilePath: "test.ogg", FileSize: v.size}, nil
}
func (v *voiceTransport) DownloadFile(context.Context, string, int64) ([]byte, error) {
	v.downloads++
	return []byte("audio"), nil
}

type transcribeFunc func(context.Context, []byte) (string, error)

func (f transcribeFunc) Transcribe(c context.Context, b []byte) (string, error) { return f(c, b) }

type formatFunc func(context.Context, string) (string, error)

func (f formatFunc) Format(c context.Context, s string) (string, error) { return f(c, s) }
func voiceUpdate(id, thread int64) Update {
	return Update{ID: id, Message: &Message{ChatID: 100, FromID: 100, ThreadID: thread, MessageID: id + 10, Voice: &Voice{FileID: "voice", Duration: 10, FileSize: 5}}}
}
func voiceAdapter(t *testing.T, path string, tr *voiceTransport, s ServerClient, asr ASRTranscriber, formatter TextFormatter, show bool) *Adapter {
	t.Helper()
	a, err := New(Config{StatePath: path, OwnerChatID: 100, ASRTranscriber: asr, Formatter: formatter, ASR: config.ASRConfig{ShowASRInChat: show, Formatter: config.FormatterConfig{Enabled: formatter != nil}}}, tr, s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestVoiceTranscriptRoutingReplayAndFlag(t *testing.T) {
	for _, show := range []bool{false, true} {
		t.Run(map[bool]string{false: "hidden", true: "shown"}[show], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			tr := &voiceTransport{fakeTransport: &fakeTransport{}}
			server := &fakeServer{}
			calls := 0
			asr := transcribeFunc(func(context.Context, []byte) (string, error) { calls++; return "не удаляй 3 файла", nil })
			a := voiceAdapter(t, path, tr, server, asr, formatFunc(func(context.Context, string) (string, error) { return "Не удаляй 3 файла.", nil }), show)
			if err := a.HandleUpdate(context.Background(), voiceUpdate(1, 0)); err != nil {
				t.Fatal(err)
			}
			if len(server.messages) != 1 || server.messages[0].Body != "Не удаляй 3 файла." {
				t.Fatalf("delivery=%+v", server.messages)
			}
			transcript, ok := a.Transcript("1")
			if !ok || transcript.Raw == "" || transcript.Formatted == "" || transcript.MessageID != 11 {
				t.Fatalf("transcript=%+v", transcript)
			}
			if show {
				if len(tr.sent) != 1 || tr.sent[0].Text != server.messages[0].Body || tr.sent[0].ReplyToMessageID != 11 {
					t.Fatalf("shown=%+v", tr.sent)
				}
			} else if len(tr.sent) != 0 {
				t.Fatal("hidden transcript leaked")
			}
			a = voiceAdapter(t, path, tr, server, asr, nil, show)
			if err := a.HandleUpdate(context.Background(), voiceUpdate(1, 0)); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(server.messages) != 1 {
				t.Fatal("replay repeated inference/delivery")
			}
		})
	}
}
func TestVoiceUnauthorizedLimitsAndFallback(t *testing.T) {
	tr := &voiceTransport{fakeTransport: &fakeTransport{}}
	server := &fakeServer{}
	calls := 0
	a := voiceAdapter(t, filepath.Join(t.TempDir(), "state"), tr, server, transcribeFunc(func(context.Context, []byte) (string, error) { calls++; return "не удаляй 3 файла", nil }), formatFunc(func(context.Context, string) (string, error) { return "удаляй 3 файла", nil }), false)
	u := voiceUpdate(1, 0)
	u.Message.FromID = 200
	if err := a.HandleUpdate(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u = voiceUpdate(2, 0)
	u.Message.ThreadID = 900
	if err := a.HandleUpdate(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	u = voiceUpdate(3, 0)
	u.Message.Voice.Duration = 121
	if err := a.HandleUpdate(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || tr.downloads != 0 {
		t.Fatal("unauthorized/oversized input processed")
	}
	if err := a.HandleUpdate(context.Background(), voiceUpdate(4, 0)); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 || server.messages[0].Body != "не удаляй 3 файла" {
		t.Fatalf("fallback=%+v", server.messages)
	}
	if len(tr.sent) != 2 {
		t.Fatalf("notices=%+v", tr.sent)
	}
}

type orderedServer struct {
	mu    sync.Mutex
	ids   []string
	ready chan string
	fail  bool
}

func (s *orderedServer) SendMessage(_ context.Context, m InboundMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("retry")
	}
	s.ids = append(s.ids, m.ExternalMessageID)
	s.ready <- m.ExternalMessageID
	return nil
}
func (s *orderedServer) SendWorkerMessage(_ context.Context, m WorkerMessage) error {
	return s.SendMessage(context.Background(), InboundMessage{ExternalMessageID: m.ExternalMessageID})
}
func TestVoiceQueueFIFOIndependentTopicAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	tr := &voiceTransport{fakeTransport: &fakeTransport{}}
	server := &orderedServer{ready: make(chan string, 10)}
	started := make(chan struct{})
	release := make(chan struct{})
	a := voiceAdapter(t, path, tr, server, transcribeFunc(func(ctx context.Context, _ []byte) (string, error) {
		close(started)
		select {
		case <-release:
			return "voice", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}), nil, false)
	a.state.Topics["w"] = TopicMapping{WorkerRef: "w", ChatID: 100, ThreadID: 7}
	text := Update{ID: 2, Message: &Message{ChatID: 100, FromID: 100, Text: "cancel"}}
	other := Update{ID: 3, Message: &Message{ChatID: 100, FromID: 100, ThreadID: 7, Text: "other"}}
	if err := a.enqueueInputs([]Update{voiceUpdate(1, 0), text, other}, true); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- a.drainInputs(context.Background()) }()
	<-started
	select {
	case id := <-server.ready:
		if id != "3" {
			t.Fatalf("overtook=%s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("other topic blocked")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	ids := append([]string(nil), server.ids...)
	server.mu.Unlock()
	if len(ids) != 3 || ids[1] != "1" || ids[2] != "2" {
		t.Fatalf("order=%v", ids)
	}
	// Pending text survived offset advancement and can be delivered after restart.
	next := Update{ID: 4, Message: &Message{ChatID: 100, FromID: 100, Text: "persisted"}}
	if err := a.enqueueInputs([]Update{next}, true); err != nil {
		t.Fatal(err)
	}
	a = voiceAdapter(t, path, tr, server, nil, nil, false)
	if err := a.drainInputs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.state.Offset != 4 || len(a.state.PendingInputs) != 0 {
		t.Fatalf("restart state=%+v", a.state)
	}
}

func TestVoiceRetryReusesTranscriptAndNotices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	tr := &voiceTransport{fakeTransport: &fakeTransport{}}
	server := &orderedServer{ready: make(chan string, 10), fail: true}
	calls := 0
	asr := transcribeFunc(func(context.Context, []byte) (string, error) { calls++; return "исходный текст", nil })
	a := voiceAdapter(t, path, tr, server, asr, formatFunc(func(context.Context, string) (string, error) { return "", errors.New("unavailable") }), false)
	if err := a.HandleUpdate(context.Background(), voiceUpdate(1, 0)); err == nil {
		t.Fatal("server failure hidden")
	}
	if len(tr.sent) != 1 {
		t.Fatalf("notices=%v", tr.sent)
	}
	server.fail = false
	a = voiceAdapter(t, path, tr, server, asr, nil, false)
	if err := a.drainInputs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || tr.downloads != 1 || len(tr.sent) != 1 || len(server.ids) != 1 {
		t.Fatalf("retry calls=%d downloads=%d notices=%d delivery=%v", calls, tr.downloads, len(tr.sent), server.ids)
	}
}

func TestFailedVoiceReleasesFollowingText(t *testing.T) {
	tr := &voiceTransport{fakeTransport: &fakeTransport{}}
	s := &fakeServer{}
	a := voiceAdapter(t, filepath.Join(t.TempDir(), "state"), tr, s, transcribeFunc(func(context.Context, []byte) (string, error) { return "", errors.New("asr failed") }), nil, false)
	if err := a.enqueueInputs([]Update{voiceUpdate(1, 0), {ID: 2, Message: &Message{ChatID: 100, FromID: 100, Text: "following"}}}, true); err != nil {
		t.Fatal(err)
	}
	if err := a.drainInputs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.messages) != 1 || s.messages[0].Body != "following" || len(a.state.PendingInputs) != 0 {
		t.Fatal("ASR error blocked queue")
	}
}
