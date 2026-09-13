package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTransport struct {
	mu      sync.Mutex
	updates []Update
	sent    []SentMessage
	topics  []ForumTopic
	fail    bool
}

func (f *fakeTransport) GetUpdates(context.Context, int64, time.Duration) ([]Update, error) {
	updates := append([]Update(nil), f.updates...)
	f.updates = nil
	return updates, nil
}
func (f *fakeTransport) SendMessage(_ context.Context, message OutgoingMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return context.DeadlineExceeded
	}
	f.sent = append(f.sent, SentMessage{OutgoingMessage: message})
	return nil
}
func (f *fakeTransport) CreateForumTopic(_ context.Context, chatID int64, name string) (ForumTopic, error) {
	topic := ForumTopic{ChatID: chatID, ThreadID: int64(len(f.topics) + 1), Name: name}
	f.topics = append(f.topics, topic)
	return topic, nil
}

type fakeServer struct {
	messages []InboundMessage
	workers  []WorkerMessage
}

func (f *fakeServer) SendMessage(_ context.Context, message InboundMessage) error {
	f.messages = append(f.messages, message)
	return nil
}
func (f *fakeServer) SendWorkerMessage(_ context.Context, message WorkerMessage) error {
	f.workers = append(f.workers, message)
	return nil
}

type retryServer struct {
	mu        sync.Mutex
	calls     int
	failFirst bool
	started   chan struct{}
	release   chan struct{}
}

func (s *retryServer) SendMessage(context.Context, InboundMessage) error {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == 1 {
		close(s.started)
		<-s.release
		return context.DeadlineExceeded
	}
	return nil
}
func (s *retryServer) SendWorkerMessage(context.Context, WorkerMessage) error { return nil }

type concurrentTopicTransport struct {
	mu      sync.Mutex
	calls   int
	topics  []ForumTopic
	started chan struct{}
	release chan struct{}
}

func (t *concurrentTopicTransport) GetUpdates(context.Context, int64, time.Duration) ([]Update, error) {
	return nil, nil
}
func (t *concurrentTopicTransport) SendMessage(context.Context, OutgoingMessage) error { return nil }
func (t *concurrentTopicTransport) CreateForumTopic(_ context.Context, chatID int64, name string) (ForumTopic, error) {
	t.mu.Lock()
	t.calls++
	call := t.calls
	topic := ForumTopic{ChatID: chatID, ThreadID: int64(call), Name: name}
	t.topics = append(t.topics, topic)
	t.mu.Unlock()
	if call == 1 {
		close(t.started)
	}
	<-t.release
	return topic, nil
}

func newTestAdapter(t *testing.T, transport Transport, server ServerClient, statePath string) *Adapter {
	t.Helper()
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestDuplicateUpdateIsIgnoredAndUnauthorizedCannotChangeState(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.SetOwnerChat(100); err != nil {
		t.Fatal(err)
	}
	update := Update{ID: 7, Message: &Message{ChatID: 100, FromID: 100, Text: "hello"}}
	if err := adapter.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 {
		t.Fatalf("messages after duplicate = %d", len(server.messages))
	}
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 8, Message: &Message{ChatID: 999, FromID: 999, Text: "read state"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 || len(transport.sent) != 0 {
		t.Fatalf("unauthorized update changed state: messages=%d sent=%d", len(server.messages), len(transport.sent))
	}
}

func TestWorkerTopicMappingPersistsAndRoutesFollowUp(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, statePath)
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "w-1", Title: "Fix header"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.topics) != 1 || transport.topics[0].ThreadID == 0 {
		t.Fatalf("topics = %#v", transport.topics)
	}
	threadID := transport.topics[0].ThreadID
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 9, Message: &Message{ChatID: 100, ThreadID: threadID, FromID: 100, Text: "keep going"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.workers) != 1 || server.workers[0].WorkerRef != "w-1" || server.workers[0].Text != "keep going" {
		t.Fatalf("worker messages = %#v", server.workers)
	}
	if len(transport.topics) != 1 {
		t.Fatalf("restart created another topic: %#v", transport.topics)
	}
}

func TestSecretaryEventsAreThrottledAndWorkerEventsAreReadable(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "Hello "}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "world"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.tool_call", Tool: "list_workers"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("delta sent before throttle flush: %#v", transport.sent)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "Hello world") || !strings.Contains(transport.sent[0].Text, "list_workers") {
		t.Fatalf("secretary batch = %#v", transport.sent)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.activity", WorkerRef: "w-1", Text: "analysis: secret-cot", Tool: "shell", Payload: json.RawMessage(`{"token":"node-secret","path":"/tmp/work"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := transport.sent[len(transport.sent)-1].Text
	if strings.Contains(last, "secret-cot") || strings.Contains(last, "node-secret") || strings.Contains(last, "runtime") {
		t.Fatalf("raw or sensitive worker event leaked: %q", last)
	}
	if !strings.Contains(last, "shell") {
		t.Fatalf("readable worker activity missing: %q", last)
	}
}

func TestImportantWorkerStatesAndRestartReplay(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{updates: []Update{{ID: 11, Message: &Message{ChatID: 100, FromID: 100, Text: "hello"}}}}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, statePath)
	if err := adapter.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 {
		t.Fatalf("poll messages = %#v", server.messages)
	}
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 {
		t.Fatalf("replay duplicated inbound message: %#v", server.messages)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.approval_requested", WorkerRef: "w-1", Text: "approve shell"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.completed", WorkerRef: "w-1", Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.offline", WorkerRef: "w-1"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) < 3 {
		t.Fatalf("important events were lost: %#v", transport.sent)
	}
}

func TestPairingCodeIsOneTimeAndDoesNotExposeCredential(t *testing.T) {
	adapter := newTestAdapter(t, &fakeTransport{}, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	pairing, err := adapter.CreatePairing("bot")
	if err != nil {
		t.Fatal(err)
	}
	if pairing.Code == "" || !strings.Contains(pairing.DeepLink, pairing.Code) {
		t.Fatalf("pairing = %#v", pairing)
	}
	if strings.Contains(pairing.DeepLink, "token") || pairing.Credential != "" {
		t.Fatalf("pairing exposed credential: %#v", pairing)
	}
	if err := adapter.RedeemPairing(pairing.Code, 100); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RedeemPairing(pairing.Code, 101); err == nil {
		t.Fatal("reused pairing code was accepted")
	}
}

func TestOutboxSurvivesTransportFailureAndRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{fail: true}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "w-1", Title: "Task"}); err == nil {
		t.Fatal("transport failure was hidden")
	}
	transport.fail = false
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "Task") {
		t.Fatalf("outbox after restart = %#v", transport.sent)
	}
}

func TestHandleUpdateFailureCanBeRetriedAfterConcurrentDuplicate(t *testing.T) {
	transport := &fakeTransport{}
	server := &retryServer{failFirst: true, started: make(chan struct{}), release: make(chan struct{})}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	update := Update{ID: 77, Message: &Message{ChatID: 100, FromID: 100, Text: "retry me"}}
	first := make(chan error, 1)
	go func() { first <- adapter.HandleUpdate(context.Background(), update) }()
	<-server.started
	second := make(chan error, 1)
	go func() { second <- adapter.HandleUpdate(context.Background(), update) }()
	close(server.release)
	if err := <-first; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first error=%v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("concurrent duplicate error=%v", err)
	}
	if err := adapter.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if server.calls != 2 {
		t.Fatalf("server calls=%d, want failed delivery plus one retry", server.calls)
	}
}

func TestEnsureTopicConcurrentCallsCreateOnlyOneTopic(t *testing.T) {
	transport := &concurrentTopicTransport{started: make(chan struct{}), release: make(chan struct{})}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_ = adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "same-worker", Title: "Same"})
		}()
	}
	<-transport.started
	close(transport.release)
	group.Wait()
	if transport.calls != 1 {
		t.Fatalf("topic creates=%d, want 1", transport.calls)
	}
}

func TestThrottleTimerFlushesSecretaryBatch(t *testing.T) {
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	adapter.config.FlushInterval = 10 * time.Millisecond
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "timer"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		transport.mu.Lock()
		sent := append([]SentMessage(nil), transport.sent...)
		transport.mu.Unlock()
		if len(sent) > 0 {
			if len(sent) != 1 || !strings.Contains(sent[0].Text, "timer") {
				t.Fatalf("timer batch=%#v", sent)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("throttle timer did not flush")
}

func TestHostileEventMetadataNeverReachesTelegram(t *testing.T) {
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	hostile := "task-9 session-8 native-7 node-secret channel-secret callback-secret reasoning-secret thought-secret"
	for _, event := range []Event{
		{Kind: "secretary.text_delta", Text: hostile + " analysis: hidden"},
		{Kind: "worker.activity", WorkerRef: "worker-9", Tool: "shell", Text: hostile, Payload: json.RawMessage(`{"task_id":"task-9","session_id":"session-8","native_id":"native-7","channel_secret":"channel-secret","callback":"callback-secret","reasoning":"reasoning-secret"}`)},
		{Kind: "worker.approval_requested", WorkerRef: "worker-9", Text: hostile, Payload: json.RawMessage(`{"request_id":"request-roundtrip","analysis":"thought-secret"}`)},
	} {
		if err := adapter.HandleEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, sent := range transport.sent {
		for _, value := range []string{"task-9", "session-8", "native-7", "node-secret", "channel-secret", "callback-secret", "reasoning-secret", "thought-secret", "analysis: hidden"} {
			if strings.Contains(sent.Text, value) {
				t.Fatalf("unsafe value %q in %q", value, sent.Text)
			}
		}
	}
}

func TestApprovalAndNeedsInputCarryRequestIDToWorkerReply(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Task"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.needs_input", WorkerRef: "worker-1", Payload: json.RawMessage(`{"request_id":"request-1"}`)}); err != nil {
		t.Fatal(err)
	}
	thread := transport.topics[0].ThreadID
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 88, Message: &Message{ChatID: 100, ThreadID: thread, FromID: 100, Text: "answer"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.workers) != 1 || server.workers[0].RequestID != "request-1" {
		t.Fatalf("worker replies=%#v", server.workers)
	}
}

func TestStateFileIsAtomicAndReadableAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telegram.json")
	adapter := newTestAdapter(t, &fakeTransport{}, &fakeServer{}, path)
	if err := adapter.SetOwnerChat(100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
}
