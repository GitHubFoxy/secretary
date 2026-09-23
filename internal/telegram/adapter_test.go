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
	actions []ChatAction
	topics  []ForumTopic
	fail    bool
}

type ChatAction struct {
	ChatID   int64
	ThreadID int64
	Action   string
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
func (f *fakeTransport) SendChatAction(_ context.Context, chatID, threadID int64, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return context.DeadlineExceeded
	}
	f.actions = append(f.actions, ChatAction{ChatID: chatID, ThreadID: threadID, Action: action})
	return nil
}
func (f *fakeTransport) CreateForumTopic(_ context.Context, chatID int64, name string) (ForumTopic, error) {
	topic := ForumTopic{ChatID: chatID, ThreadID: int64(len(f.topics) + 1), Name: name}
	f.topics = append(f.topics, topic)
	return topic, nil
}

type reactionCall struct {
	chatID    int64
	messageID int64
	emoji     string
}

type recordingReactionTransport struct {
	*fakeTransport
	reactions []reactionCall
	err       error
}

func (t *recordingReactionTransport) SetMessageReaction(_ context.Context, chatID, messageID int64, emoji string) error {
	t.reactions = append(t.reactions, reactionCall{chatID: chatID, messageID: messageID, emoji: emoji})
	return t.err
}

type fakeServer struct {
	messages []InboundMessage
	workers  []WorkerMessage
}

type blockingSendTransport struct {
	mu      sync.Mutex
	sent    []SentMessage
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (t *blockingSendTransport) GetUpdates(context.Context, int64, time.Duration) ([]Update, error) {
	return nil, nil
}
func (t *blockingSendTransport) SendMessage(_ context.Context, message OutgoingMessage) error {
	t.once.Do(func() { close(t.started) })
	<-t.release
	t.mu.Lock()
	t.sent = append(t.sent, SentMessage{OutgoingMessage: message})
	t.mu.Unlock()
	return nil
}
func (t *blockingSendTransport) CreateForumTopic(context.Context, int64, string) (ForumTopic, error) {
	return ForumTopic{}, nil
}
func (t *blockingSendTransport) SendChatAction(context.Context, int64, int64, string) error {
	return nil
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

type retryPollingTransport struct {
	mu    sync.Mutex
	calls int
	ready chan struct{}
}

func (t *retryPollingTransport) GetUpdates(context.Context, int64, time.Duration) ([]Update, error) {
	t.mu.Lock()
	t.calls++
	calls := t.calls
	t.mu.Unlock()
	if calls < 3 {
		return nil, context.DeadlineExceeded
	}
	select {
	case <-t.ready:
	default:
		close(t.ready)
	}
	return nil, nil
}
func (t *retryPollingTransport) SendMessage(context.Context, OutgoingMessage) error { return nil }
func (t *retryPollingTransport) SendChatAction(context.Context, int64, int64, string) error {
	return nil
}
func (t *retryPollingTransport) CreateForumTopic(context.Context, int64, string) (ForumTopic, error) {
	return ForumTopic{}, nil
}

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
func (t *concurrentTopicTransport) SendChatAction(context.Context, int64, int64, string) error {
	return nil
}
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

func TestClientMessageMirrorsToGeneralOnceWithoutTelegramEcho(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	event := Event{EventID: "entry-event-1", Sequence: 1, Kind: "message.saved", Source: "client:pi-client", Text: "Hi"}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	adapter = newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	telegramEvent := event
	telegramEvent.EventID = "entry-event-2"
	telegramEvent.Source = "client:telegram-adapter"
	telegramEvent.Text = "Do not echo"
	if err := adapter.HandleEvent(context.Background(), telegramEvent); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("sent=%#v, want one General mirror", transport.sent)
	}
	message := transport.sent[0]
	if message.ChatID != 100 || message.ThreadID != 0 || message.Text != "Hi" {
		t.Fatalf("mirrored message=%#v", message)
	}
}

func TestTerminalResultMirrorsToGeneralWithWorkerLabel(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	event := Event{Kind: "worker.completed", WorkerRef: "worker-9", Text: "pong", TerminalIdentity: "turn:9"}
	if err := adapter.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var topic, general int
	for _, sent := range transport.sent {
		switch {
		case sent.ThreadID != 0 && sent.Text == "pong":
			topic++
		case sent.ThreadID == 0 && sent.Text == "worker-9:\npong":
			general++
		default:
			t.Fatalf("unexpected terminal message=%#v", sent)
		}
	}
	if topic != 1 || general != 1 {
		t.Fatalf("terminal mirror topic=%d general=%d sent=%#v", topic, general, transport.sent)
	}
}

func TestDelegatedTurnSendsNothingToGeneral(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	ctx := context.Background()
	for _, event := range []Event{
		{Kind: "secretary.turn.started"},
		{Kind: "secretary.text_delta", Text: "I'll create a Worker"},
		{Kind: "secretary.tool_call", Tool: "mcp_secretary_spawn_worker"},
		{Kind: "secretary.text_delta", Text: "done narrating"},
		{Kind: "secretary.turn.finished"},
	} {
		if err := adapter.HandleEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("delegated narration reached General: %#v", transport.sent)
	}
}

func TestQuestionTurnSendsSingleMessageOnFinished(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	ctx := context.Background()
	if err := adapter.HandleEvent(ctx, Event{Kind: "secretary.turn.started"}); err != nil {
		t.Fatal(err)
	}
	for _, delta := range []string{"I’ll create", " a", " new", " Worker"} {
		if err := adapter.HandleEvent(ctx, Event{Kind: "secretary.text_delta", Text: delta}); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("interim flush split the turn: %#v", transport.sent)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "I’ll create a new Worker" {
		t.Fatalf("question turn batch=%#v", transport.sent)
	}
}

func TestGeneralThreadRoutesToSecretaryAndUnknownThreadDrops(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	ctx := context.Background()
	if err := adapter.HandleUpdate(ctx, Update{ID: 60, Message: &Message{ChatID: 100, ThreadID: 1, FromID: 100, Text: "general via thread"}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(ctx, Update{ID: 61, Message: &Message{ChatID: 100, ThreadID: 7, FromID: 100, Text: "unknown thread"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 || server.messages[0].Body != "general via thread" {
		t.Fatalf("thread routing messages=%#v", server.messages)
	}
}

func TestTypingActionOnInboundAndFlush(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 50, Message: &Message{ChatID: 100, FromID: 100, Text: "hello"}}); err != nil {
		t.Fatal(err)
	}
	if len(transport.actions) != 1 || transport.actions[0] != (ChatAction{ChatID: 100, ThreadID: 1, Action: "typing"}) {
		t.Fatalf("typing after inbound=%#v", transport.actions)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.started"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.actions) != 1 {
		t.Fatalf("typing while turn open without new content should stay throttled=%#v", transport.actions)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "working"}); err != nil {
		t.Fatal(err)
	}
	adapter.typingMu.Lock()
	adapter.lastTyping = map[string]time.Time{}
	adapter.typingMu.Unlock()
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.actions) != 2 {
		t.Fatalf("typing while pending=%#v", transport.actions)
	}
	if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "working") {
		t.Fatalf("pending flush=%#v", transport.sent)
	}
}

func TestAcceptedSecretaryMessageGetsBestEffortReaction(t *testing.T) {
	for _, reactionErr := range []error{nil, errors.New("reaction unavailable")} {
		transport := &recordingReactionTransport{fakeTransport: &fakeTransport{}, err: reactionErr}
		server := &fakeServer{}
		adapter, err := New(Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
		if err != nil {
			t.Fatal(err)
		}
		update := Update{ID: 7, Message: &Message{ChatID: 100, FromID: 100, MessageID: 42, Text: "hello"}}
		if err := adapter.HandleUpdate(context.Background(), update); err != nil {
			t.Fatalf("reaction error %v blocked message: %v", reactionErr, err)
		}
		if len(server.messages) != 1 {
			t.Fatalf("server messages=%#v", server.messages)
		}
		if len(transport.reactions) != 1 || transport.reactions[0] != (reactionCall{chatID: 100, messageID: 42, emoji: "👀"}) {
			t.Fatalf("reactions=%#v", transport.reactions)
		}
	}
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
	if len(transport.sent) != 0 {
		t.Fatalf("secretary batch sent before turn finished: %#v", transport.sent)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "Hello world") {
		t.Fatalf("secretary batch = %#v", transport.sent)
	}
	if strings.Contains(transport.sent[0].Text, "list_workers") || strings.Contains(transport.sent[0].Text, "Шаги:") {
		t.Fatalf("secretary tool call leaked into General: %q", transport.sent[0].Text)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.activity", WorkerRef: "w-1", Text: "analysis: secret-cot", Tool: "shell", Payload: json.RawMessage(`{"token":"node-secret","path":"/tmp/work"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("worker activity must stay out of topics: %#v", transport.sent)
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
	event := Event{Sequence: 1, Kind: "worker.completed", WorkerRef: "w-1", Text: "Task", TerminalIdentity: "turn:1"}
	if err := adapter.HandleDurableEvent(context.Background(), event); err == nil {
		t.Fatal("transport failure was hidden")
	}
	transport.fail = false
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.HandleDurableEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := restarted.HandleDurableEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 2 || transport.sent[0].Text != "Task" || transport.sent[1].Text != "w-1:\nTask" {
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
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
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

func TestDurableSecretaryEventSurvivesRestartBeforeThrottleFlush(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 1, Kind: "secretary.text_delta", Text: "survive restart"}); err != nil {
		t.Fatal(err)
	}
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.HandleDurableEvent(context.Background(), Event{Sequence: 2, Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "survive restart") {
		t.Fatalf("durable secretary batch=%#v", transport.sent)
	}
}

func TestApprovalResolvedClearsRequestForNormalFollowUp(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	for _, event := range []Event{
		{Kind: "worker.created", WorkerRef: "worker-approval", Title: "Task"},
		{Kind: "worker.approval_requested", WorkerRef: "worker-approval", Payload: json.RawMessage(`{"request_id":"approval-1"}`), Text: "approve"},
		{Kind: "approval.resolved", WorkerRef: "worker-approval", Payload: json.RawMessage(`{"request_id":"approval-1","response":"approved"}`), Text: "approved"},
	} {
		if err := adapter.HandleEvent(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	threadID := transport.topics[0].ThreadID
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 91, Message: &Message{ChatID: 100, ThreadID: threadID, FromID: 100, Text: "normal follow-up"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.workers) != 1 || server.workers[0].RequestID != "" {
		t.Fatalf("follow-up request mapping=%#v", server.workers)
	}
}

func TestSecretaryEventsBeforePairingDoNotCreateChatZeroOutbox(t *testing.T) {
	transport := &fakeTransport{}
	adapter, err := New(Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), FlushInterval: time.Minute}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "before pairing"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("pre-pairing sent=%#v", transport.sent)
	}
	data, err := os.ReadFile(adapter.config.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	for _, message := range state.Outbox {
		if message.ChatID == 0 {
			t.Fatalf("pre-pairing poisoned outbox=%#v", state.Outbox)
		}
	}
	if err := adapter.SetOwnerChat(100); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].ChatID != 100 {
		t.Fatalf("replayed after pairing=%#v", transport.sent)
	}
}

func TestTelegramSanitizerRecursivelyRedactsPublicForbiddenValues(t *testing.T) {
	input := `{"safe":"kept","nested":{"safe":"task-9","native":"native-session","credential":"credential-secret","channel":"channel-secret","text":"internal reasoning"},"array":[{"allowed":"thought-secret"}]}`
	clean := safeText(input)
	for _, forbidden := range []string{"task-9", "native-session", "credential-secret", "channel-secret", "internal reasoning", "thought-secret"} {
		if strings.Contains(clean, forbidden) {
			t.Fatalf("sanitizer leaked %q in %q", forbidden, clean)
		}
	}
	if !strings.Contains(clean, "kept") {
		t.Fatalf("sanitizer removed allowed value: %q", clean)
	}
	if delta := safeDelta("raw <think>secret</think> analysis: hidden"); strings.Contains(delta, "secret") || strings.Contains(delta, "analysis") {
		t.Fatalf("unsafe delta=%q", delta)
	}
	if delta := safeDelta(" "); delta != " " {
		t.Fatalf("whitespace delta=%q, want one space", delta)
	}
}

func TestDefaultPollIntervalIsTwoSeconds(t *testing.T) {
	adapter := newTestAdapter(t, &fakeTransport{}, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	if adapter.config.PollInterval != 2*time.Second {
		t.Fatalf("poll interval=%s, want 2s", adapter.config.PollInterval)
	}
}

func TestRunImmediatelyStartsNextLongPollAfterSuccess(t *testing.T) {
	transport := &retryPollingTransport{ready: make(chan struct{})}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	adapter.config.PollInterval = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- adapter.Run(ctx) }()
	select {
	case <-transport.ready:
	case <-time.After(time.Second):
		t.Fatal("polling did not reach a successful response")
	}
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		transport.mu.Lock()
		calls := transport.calls
		transport.mu.Unlock()
		if calls >= 4 {
			cancel()
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("Run error=%v", err)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-result
	t.Fatal("Run waited for PollInterval after a successful long poll")
}

func TestRunRetriesTransientPollingErrors(t *testing.T) {
	transport := &retryPollingTransport{ready: make(chan struct{})}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	adapter.config.PollInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- adapter.Run(ctx) }()
	select {
	case <-transport.ready:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("polling did not retry to success")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
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

func TestDurableEventRecoveryDoesNotDuplicatePendingBatch(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	state := map[string]any{
		"version":                1,
		"owner_chat_id":          int64(100),
		"last_event_seq":         int64(0),
		"processed_updates":      map[string]time.Time{},
		"topics":                 map[string]TopicMapping{},
		"pairings":               map[string]any{},
		"pending_secretary_text": "recover once",
		"pending_event_seqs":     []int64{1},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 1, Kind: "secretary.text_delta", Text: "recover once"}); err != nil {
		t.Fatal(err)
	}
	if got := adapter.LastEventSeq(); got != 1 {
		t.Fatalf("last event sequence = %d, want 1", got)
	}
	if err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 2, Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "recover once" {
		t.Fatalf("recovered batch = %#v", transport.sent)
	}
}

func TestWorkerActivityNeverReachesTopics(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.activity", WorkerRef: "worker-1", Tool: "shell"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("activity messages reached topics: %#v", transport.sent)
	}
}

func TestTerminalOutboxRestartDoesNotReplayAfterDrain(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{fail: true}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	event := Event{Sequence: 1, Kind: "worker.completed", WorkerRef: "worker-1", Text: "done", TerminalIdentity: "turn:1"}
	if err := adapter.HandleDurableEvent(context.Background(), event); err == nil {
		t.Fatal("expected terminal transport failure")
	}

	transport.fail = false
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.HandleDurableEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("terminal messages after replay = %#v", transport.sent)
	}
}

func TestTelegramSanitizerStrictPlainTextFixtures(t *testing.T) {
	fixtures := []string{
		"analysis: hidden",
		"reasoning = hidden",
		"thought: hidden",
		"password=hidden",
		"token: hidden",
		"secret = hidden",
		"credential: hidden",
		"task=task-123 session: session-123 native=native-123 channel: channel-123",
		"sk-live-secret ghp_live-secret xoxb-live-secret xoxb_live_secret",
		"<think>raw chain of thought</think>",
		`{"nested":{"analysis":"internal","safe":"visible"},"items":[{"password":"hidden"}]}`,
	}
	for _, fixture := range fixtures {
		clean := safeText(fixture)
		for _, forbidden := range []string{"hidden", "task-123", "session-123", "native-123", "channel-123", "sk-live-secret", "ghp_live-secret", "xoxb-live-secret", "xoxb_live_secret", "raw chain of thought", "internal"} {
			if strings.Contains(clean, forbidden) {
				t.Fatalf("sanitizer leaked %q for %q: %q", forbidden, fixture, clean)
			}
		}
		if strings.Contains(clean, "analysis") || strings.Contains(clean, "reasoning") || strings.Contains(clean, "thought") || strings.Contains(clean, "password") || strings.Contains(clean, "token") || strings.Contains(clean, "secret") || strings.Contains(clean, "credential") || strings.Contains(clean, "session") || strings.Contains(clean, "native") || strings.Contains(clean, "channel") {
			t.Fatalf("sanitizer leaked marker for %q: %q", fixture, clean)
		}
	}
}

func TestDurableBatchRestartDoesNotReplayAlreadyOutboxedMessage(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{fail: true}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "send once"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err == nil {
		t.Fatal("expected transport failure")
	}

	transport.fail = false
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "send once" {
		t.Fatalf("replayed durable batch = %#v", transport.sent)
	}
}

func TestFlushSerializesConcurrentEventWithoutReplayingPrefix(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &blockingSendTransport{started: make(chan struct{}), release: make(chan struct{})}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "prefix"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}

	flushDone := make(chan error, 1)
	go func() { flushDone <- adapter.Flush(context.Background()) }()
	<-transport.started
	eventDone := make(chan error, 1)
	go func() {
		eventDone <- adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "suffix"})
	}()

	time.Sleep(50 * time.Millisecond)
	close(transport.release)
	if err := <-flushDone; err != nil {
		t.Fatal(err)
	}
	if err := <-eventDone; err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	transport.mu.Lock()
	sent := append([]SentMessage(nil), transport.sent...)
	transport.mu.Unlock()
	if len(sent) != 2 || sent[0].Text != "prefix" || sent[1].Text != "suffix" {
		t.Fatalf("sent batches = %#v", sent)
	}
}
