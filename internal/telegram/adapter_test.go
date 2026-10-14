package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
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
func (f *fakeServer) ResumeWorker(_ context.Context, _ string) error { return nil }

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
func (s *retryServer) ResumeWorker(context.Context, string) error             { return nil }

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

func TestTerminalResultMirrorsToGeneral(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	event := Event{Kind: "worker.completed", WorkerRef: "wrk_private", Title: "Погода", Text: "pong", TerminalIdentity: "turn:9"}
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
		case sent.ThreadID == 0 && sent.Text == "pong":
			general++
		default:
			t.Fatalf("unexpected terminal message=%#v", sent)
		}
	}
	for _, sent := range transport.sent {
		if !sent.DisableLinkPreview {
			t.Fatalf("Worker result allows link preview: %#v", sent)
		}
	}
	if topic != 1 || general != 1 {
		t.Fatalf("terminal mirror topic=%d general=%d sent=%#v", topic, general, transport.sent)
	}
}

func TestDelegatedTurnAcknowledgesBeforeWorkerCompletes(t *testing.T) {
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
	if len(transport.sent) != 1 || transport.sent[0].ThreadID != 0 || transport.sent[0].Text != "I&#39;ll create a Workerdone narrating" || transport.sent[0].DisableLinkPreview {
		t.Fatalf("missing delegation acknowledgement: %#v", transport.sent)
	}
	adapter.typingMu.Lock()
	adapter.lastTyping = map[string]time.Time{}
	adapter.typingMu.Unlock()
	before := len(transport.actions)
	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.activity", WorkerRef: "worker-1"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.actions) != before {
		t.Fatalf("Secretary typing continued after delegation: %#v", transport.actions)
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

func TestWorkerToolEventsRenderActualNamePreviewAndLifecycle(t *testing.T) {
	tool := "mcp__filesystem__read_file_with_a_long_but_real_name"
	for _, test := range []struct {
		kind, want string
		hasOutput  bool
	}{
		{"worker.tool_started", "▶ Запуск: " + tool + " src/config.toml", false},
		{"worker.tool_finished", "✓ Выполнено: " + tool + " src/config.toml (без вывода)", false},
		{"worker.tool_finished", "✓ Выполнено: " + tool + " src/config.toml (вывод скрыт)", true},
		{"worker.tool_failed", "✗ Ошибка: " + tool + " src/config.toml (без вывода)", false},
		{"worker.tool_failed", "✗ Ошибка: " + tool + " src/config.toml (вывод скрыт)", true},
	} {
		got := renderWorkerEvent(Event{Kind: test.kind, Tool: tool, ToolPreview: "src/config.toml", HasOutput: test.hasOutput, Text: "synthetic tool output must not appear"})
		if got != test.want || strings.Contains(got, "Worker запускает") || strings.Contains(got, "synthetic") {
			t.Fatalf("rendered tool event=%q want=%q", got, test.want)
		}
	}
	if got := renderWorkerEvent(Event{Kind: "worker.tool_started", ToolPreview: "src/config.toml"}); got != "" {
		t.Fatalf("missing tool identity rendered as event: %q", got)
	}
}

func TestWorkerToolCallIDCorrelationAndParallelExecution(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "state.json"))

	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Worker"}); err != nil {
		t.Fatal(err)
	}

	call1Start := Event{
		Kind: "worker.tool_started", WorkerRef: "worker-1", CallID: "call-1",
		Tool: "exec_command", ToolPreview: "python3 test.py",
	}
	call2Start := Event{
		Kind: "worker.tool_started", WorkerRef: "worker-1", CallID: "call-2",
		Tool: "exec_command", ToolPreview: "open https://example.com",
	}

	if err := adapter.HandleEvent(ctx, call1Start); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, call2Start); err != nil {
		t.Fatal(err)
	}

	call2Finish := Event{
		Kind: "worker.tool_finished", WorkerRef: "worker-1", CallID: "call-2",
		Tool: "exec_command", HasOutput: false,
	}
	if err := adapter.HandleEvent(ctx, call2Finish); err != nil {
		t.Fatal(err)
	}

	call1Finish := Event{
		Kind: "worker.tool_finished", WorkerRef: "worker-1", CallID: "call-1",
		Tool: "exec_command", HasOutput: true,
	}
	if err := adapter.HandleEvent(ctx, call1Finish); err != nil {
		t.Fatal(err)
	}

	if len(transport.sent) != 4 {
		t.Fatalf("sent messages count=%d want 4: %#v", len(transport.sent), transport.sent)
	}

	if transport.sent[0].Text != "▶ Запуск: exec_command python3 test.py" {
		t.Fatalf("call1 start message=%q", transport.sent[0].Text)
	}
	if transport.sent[1].Text != "▶ Запуск: exec_command open https://example.com" {
		t.Fatalf("call2 start message=%q", transport.sent[1].Text)
	}
	if transport.sent[2].Text != "✓ Выполнено: exec_command open https://example.com (без вывода)" {
		t.Fatalf("call2 finish message=%q", transport.sent[2].Text)
	}
	if transport.sent[3].Text != "✓ Выполнено: exec_command python3 test.py (вывод скрыт)" {
		t.Fatalf("call1 finish message=%q", transport.sent[3].Text)
	}
}

func TestWorkerToolWithoutCallIDNeverMatchesByNameAlone(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "state.json"))

	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Worker"}); err != nil {
		t.Fatal(err)
	}

	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.tool_started", WorkerRef: "worker-1", CallID: "call-1",
		Tool: "exec_command", ToolPreview: "first command",
	}); err != nil {
		t.Fatal(err)
	}

	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.tool_finished", WorkerRef: "worker-1", CallID: "",
		Tool: "exec_command", HasOutput: false,
	}); err != nil {
		t.Fatal(err)
	}

	if len(transport.sent) != 2 {
		t.Fatalf("sent count=%d want 2", len(transport.sent))
	}
	if transport.sent[1].Text != "✓ Выполнено: exec_command (без вывода)" {
		t.Fatalf("id-less result unexpectedly borrowed preview: %q", transport.sent[1].Text)
	}
}

func TestWorkerToolReplayDeduplication(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, path)

	created := Event{Sequence: 1, Kind: "worker.created", WorkerRef: "worker-1", Title: "Worker"}
	start := Event{Sequence: 2, Kind: "worker.tool_started", WorkerRef: "worker-1", CallID: "c1", Tool: "shell", ToolPreview: "ls"}
	finish := Event{Sequence: 3, Kind: "worker.tool_finished", WorkerRef: "worker-1", CallID: "c1", Tool: "shell", HasOutput: false}

	for _, ev := range []Event{created, start, finish} {
		if err := adapter.HandleDurableEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 2 {
		t.Fatalf("sent count=%d want 2", len(transport.sent))
	}

	adapter = newTestAdapter(t, transport, &fakeServer{}, path)
	for _, ev := range []Event{created, start, finish} {
		if err := adapter.HandleDurableEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 2 {
		t.Fatalf("duplicate messages after restart replay: sent=%d want 2", len(transport.sent))
	}
}

func TestWorkerTopicShowsExecutionNodeIDWhenKnown(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "state.json"))

	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.created", WorkerRef: "worker-1", Title: "Task 1",
		NodeID: "execution-node-primary", TaskPrompt: "Run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Узел: execution-node-primary\n\nRun tests" || !transport.sent[0].DisableLinkPreview {
		t.Fatalf("prompt with node ID delivery: %#v", transport.sent)
	}

	transport.sent = nil
	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.created", WorkerRef: "worker-2", Title: "Task 2",
		NodeID: "", TaskPrompt: "Run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Run tests" || !transport.sent[0].DisableLinkPreview {
		t.Fatalf("prompt without node ID delivery: %#v", transport.sent)
	}

	transport.sent = nil
	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.created", WorkerRef: "worker-3", Title: "Task 3",
		NodeID: "/Users/secret/path/node", TaskPrompt: "Run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Run tests" {
		t.Fatalf("prompt with filesystem path in node ID leaked path: %#v", transport.sent)
	}

	transport.sent = nil
	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.created", WorkerRef: "worker-4", Title: "Task 4",
		NodeID: "sk-token12345", TaskPrompt: "Run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Run tests" {
		t.Fatalf("token-like node ID must be rejected: %#v", transport.sent)
	}

	transport.sent = nil
	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.created", WorkerRef: "worker-5", Title: "Task 5",
		NodeID: "node-primary\ninjected", TaskPrompt: "Run tests",
	}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Run tests" {
		t.Fatalf("newline in node ID must be rejected: %#v", transport.sent)
	}
}

func TestWorkerToolAttemptIsolationReusedCallID(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "state.json"))

	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Worker"}); err != nil {
		t.Fatal(err)
	}

	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.tool_started", WorkerRef: "worker-1", AttemptID: "att-1",
		CallID: "call-1", Tool: "exec_command", ToolPreview: "cat foo.txt",
	}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.tool_finished", WorkerRef: "worker-1", AttemptID: "att-1",
		CallID: "call-1", Tool: "exec_command", HasOutput: false,
	}); err != nil {
		t.Fatal(err)
	}

	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.tool_started", WorkerRef: "worker-1", AttemptID: "att-2",
		CallID: "call-1", Tool: "exec_command", ToolPreview: "cat bar.txt",
	}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{
		Kind: "worker.tool_finished", WorkerRef: "worker-1", AttemptID: "att-2",
		CallID: "call-1", Tool: "exec_command", HasOutput: true,
	}); err != nil {
		t.Fatal(err)
	}

	if len(transport.sent) != 4 {
		t.Fatalf("sent messages count=%d want 4 (attempt 2 was suppressed): %#v", len(transport.sent), transport.sent)
	}
	if transport.sent[2].Text != "▶ Запуск: exec_command cat bar.txt" {
		t.Fatalf("att-2 start message=%q", transport.sent[2].Text)
	}
	if transport.sent[3].Text != "✓ Выполнено: exec_command cat bar.txt (вывод скрыт)" {
		t.Fatalf("att-2 finish message=%q", transport.sent[3].Text)
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
	if len(transport.sent) != 2 || transport.sent[0].Text != "Task" || transport.sent[1].Text != "Task" {
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
	defer func() {
		if err := adapter.Flush(context.Background()); err != nil {
			t.Error(err)
		}
	}()
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
	if len(transport.sent) != 2 || transport.sent[0].ThreadID == 0 || transport.sent[1].ThreadID != 0 {
		t.Fatalf("terminal must reach Topic and General exactly once: %#v", transport.sent)
	}
	if err := restarted.HandleDurableEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 2 {
		t.Fatalf("duplicate terminal delivery: %#v", transport.sent)
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

func TestAcknowledgementReplayAfterSendCheckpointBeforeCursorSave(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, path)
	event := Event{EventID: "ack-entry-event", Sequence: 2, Kind: "secretary.acknowledged", Text: "Сейчас проверю"}
	if err := adapter.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if adapter.LastEventSeq() != 0 || len(adapter.state.Outbox) != 0 || len(transport.sent) != 1 {
		t.Fatal("send checkpoint fixture invalid")
	}
	adapter = newTestAdapter(t, transport, &fakeServer{}, path)
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || adapter.LastEventSeq() != 2 {
		t.Fatalf("ack replay duplicated persisted send: %#v", transport.sent)
	}
}

type errorSteerServer struct {
	fakeServer
	err            error
	workerMessages []WorkerMessage
}

func (s *errorSteerServer) SendWorkerMessage(ctx context.Context, m WorkerMessage) error {
	s.workerMessages = append(s.workerMessages, m)
	return s.err
}

func TestWorkerSteerErrorSendsServiceMessageWithoutPollingDrop(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	server := &errorSteerServer{
		err: errors.New(`telegram: server returned 400 Bad Request: {"error":"usage_limit_exceeded: Usage limit reached","category":"usage_limit","provider_code":"usage_limit_exceeded"}`),
	}
	adapter := newTestAdapter(t, transport, server, path)
	adapter.state.Topics["w1"] = TopicMapping{
		WorkerRef: "w1",
		ChatID:    100,
		ThreadID:  42,
	}

	update := Update{
		ID: 10,
		Message: &Message{
			ChatID:   100,
			FromID:   100,
			ThreadID: 42,
			Text:     "continue task",
		},
	}

	err := adapter.HandleUpdate(ctx, update)
	if err != nil {
		t.Fatalf("expected nil error on steering failure, got: %v", err)
	}

	transport.mu.Lock()
	sent := append([]SentMessage(nil), transport.sent...)
	transport.mu.Unlock()

	if len(sent) != 1 {
		t.Fatalf("expected 1 service message sent, got: %d", len(sent))
	}
	if !strings.Contains(sent[0].Text, "Лимит использования исчерпан (usage_limit_exceeded)") {
		t.Fatalf("unexpected service message text: %s", sent[0].Text)
	}
	if sent[0].ThreadID != 42 {
		t.Fatalf("expected service message to thread 42, got: %d", sent[0].ThreadID)
	}

	adapter.mu.Lock()
	_, processed := adapter.state.Processed["10"]
	pendingCount := len(adapter.state.PendingInputs)
	adapter.mu.Unlock()

	if !processed {
		t.Fatal("update 10 was not marked as processed")
	}
	if pendingCount != 0 {
		t.Fatalf("expected 0 pending inputs, got: %d", pendingCount)
	}
}

func TestWorkerSteerErrorPreservesResetTime(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	server := &errorSteerServer{
		err: errors.New(`telegram: server returned 400 Bad Request: {"error":"usage_limit_exceeded: try again at 4:37 PM","category":"usage_limit","provider_code":"usage_limit_exceeded"}`),
	}
	adapter := newTestAdapter(t, transport, server, path)
	adapter.state.Topics["w1"] = TopicMapping{
		WorkerRef: "w1",
		ChatID:    100,
		ThreadID:  42,
	}

	update := Update{
		ID: 11,
		Message: &Message{
			ChatID:   100,
			FromID:   100,
			ThreadID: 42,
			Text:     "continue task",
		},
	}

	err := adapter.HandleUpdate(ctx, update)
	if err != nil {
		t.Fatalf("expected nil error on steering failure, got: %v", err)
	}

	transport.mu.Lock()
	sent := append([]SentMessage(nil), transport.sent...)
	transport.mu.Unlock()

	if len(sent) != 1 {
		t.Fatalf("expected 1 service message sent, got: %d", len(sent))
	}
	if !strings.Contains(sent[0].Text, "try again at 4:37 PM") {
		t.Fatalf("expected reset time preserved in service message, got: %s", sent[0].Text)
	}
	if !strings.Contains(sent[0].Text, "Лимит использования исчерпан (usage_limit_exceeded)") {
		t.Fatalf("expected usage limit header in service message, got: %s", sent[0].Text)
	}
}

func TestWorkerSteerErrorReadsCompleteUnicodeJSONOverHTTP(t *testing.T) {
	reset := " Попробуйте снова в 4:37 PM"
	prefix := "Лимит."
	errorMessage := prefix + strings.Repeat("界", 996-utf8.RuneCountInString(prefix+reset)) + reset
	technical := strings.Repeat("界", 1000)
	body, err := json.Marshal(map[string]string{
		"error": errorMessage, "category": "usage_limit", "provider_code": "usage_limit_exceeded", "technical": technical,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) <= 4096 {
		t.Fatalf("fixture body is only %d bytes; want to cover the old 4096-byte cap", len(body))
	}
	if utf8.RuneCountInString(errorMessage) != 996 || utf8.RuneCountInString(technical) != 1000 {
		t.Fatalf("unexpected bounded payload sizes: error=%d technical=%d", utf8.RuneCountInString(errorMessage), utf8.RuneCountInString(technical))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workers/w1/message" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	transport := &fakeTransport{}
	client := &HTTPServerClient{BaseURL: server.URL, Credential: "fixture-credential"}
	adapter := newTestAdapter(t, transport, client, filepath.Join(t.TempDir(), "telegram.json"))
	adapter.state.Topics["w1"] = TopicMapping{WorkerRef: "w1", ChatID: 100, ThreadID: 42}
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 12, Message: &Message{
		ChatID: 100, FromID: 100, ThreadID: 42, Text: "continue task",
	}}); err != nil {
		t.Fatalf("HandleUpdate: %v", err)
	}
	transport.mu.Lock()
	sent := append([]SentMessage(nil), transport.sent...)
	transport.mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("sent %d service messages, want one", len(sent))
	}
	if !strings.Contains(sent[0].Text, "Лимит использования исчерпан (usage_limit_exceeded)") || !strings.Contains(sent[0].Text, errorMessage) || !strings.Contains(sent[0].Text, reset) {
		t.Fatalf("Telegram lost the complete typed failure/reset explanation: %q", sent[0].Text)
	}
}

func TestUncertainWorkerSteerKeepsTelegramInputPendingForIdempotentReplay(t *testing.T) {
	server := &errorSteerServer{err: errors.New(`telegram: server returned 503 Service Unavailable: {"error":"Worker command outcome is uncertain","outcome":"uncertain","retryable":true}`)}
	adapter := newTestAdapter(t, &fakeTransport{}, server, filepath.Join(t.TempDir(), "telegram.json"))
	adapter.state.Topics["w1"] = TopicMapping{WorkerRef: "w1", ChatID: 100, ThreadID: 42}
	update := Update{ID: 13, Message: &Message{ChatID: 100, FromID: 100, ThreadID: 42, Text: "continue task"}}

	for attempt := 0; attempt < 2; attempt++ {
		if err := adapter.HandleUpdate(context.Background(), update); err == nil {
			t.Fatal("uncertain steering error was treated as a terminal rejection")
		}
	}
	adapter.mu.Lock()
	_, processed := adapter.state.Processed["13"]
	pending := append([]Update(nil), adapter.state.PendingInputs...)
	adapter.mu.Unlock()
	if processed || len(pending) != 1 || pending[0].ID != update.ID {
		t.Fatalf("uncertain Telegram update was dropped or acknowledged: processed=%v pending=%#v", processed, pending)
	}
	if len(server.workerMessages) != 2 || server.workerMessages[0].ExternalMessageID != "13" || server.workerMessages[1].ExternalMessageID != "13" {
		t.Fatalf("replay did not preserve the stable server idempotency identity: %#v", server.workerMessages)
	}
}

func TestWorkerSteerErrorReplaySafeDoesNotDuplicate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	server := &errorSteerServer{
		err: errors.New(`telegram: server returned 400 Bad Request: {"error":"usage_limit_exceeded: Usage limit reached","category":"usage_limit","provider_code":"usage_limit_exceeded"}`),
	}
	adapter := newTestAdapter(t, transport, server, path)
	adapter.state.Topics["w1"] = TopicMapping{
		WorkerRef: "w1",
		ChatID:    100,
		ThreadID:  42,
	}

	update := Update{
		ID: 12,
		Message: &Message{
			ChatID:   100,
			FromID:   100,
			ThreadID: 42,
			Text:     "continue task",
		},
	}

	err := adapter.HandleUpdate(ctx, update)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	transport.mu.Lock()
	countFirst := len(transport.sent)
	transport.mu.Unlock()
	if countFirst != 1 {
		t.Fatalf("expected 1 service message sent, got %d", countFirst)
	}

	reopened := newTestAdapter(t, transport, server, path)
	err = reopened.HandleUpdate(ctx, update)
	if err != nil {
		t.Fatalf("expected nil error on replay, got: %v", err)
	}

	transport.mu.Lock()
	countSecond := len(transport.sent)
	transport.mu.Unlock()

	if countSecond != 1 {
		t.Fatalf("expected exactly 1 service message sent across replays, got %d", countSecond)
	}
}
