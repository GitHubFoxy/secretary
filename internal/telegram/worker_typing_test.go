package telegram

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type contextWaitingTypingTransport struct {
	started chan struct{}
	once    sync.Once
}

func (*contextWaitingTypingTransport) GetUpdates(context.Context, int64, time.Duration) ([]Update, error) {
	return nil, nil
}

func (*contextWaitingTypingTransport) SendMessage(context.Context, OutgoingMessage) error { return nil }

func (t *contextWaitingTypingTransport) SendChatAction(ctx context.Context, _, _ int64, _ string) error {
	t.once.Do(func() { close(t.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (*contextWaitingTypingTransport) CreateForumTopic(context.Context, int64, string) (ForumTopic, error) {
	return ForumTopic{}, nil
}

func TestWorkerTypingRefreshTracksParallelAttemptsAndTerminalEvents(t *testing.T) {
	ctx := context.Background()
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	clock := time.Now().UTC()
	adapter.now = func() time.Time { return clock }

	for _, attemptID := range []string{"attempt-1", "attempt-2"} {
		if err := adapter.HandleDurableEvent(ctx, Event{Sequence: int64(len(adapter.state.WorkerTyping) + 1), Kind: "worker.started", WorkerRef: "worker-1", AttemptID: attemptID}); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.actions) != 1 || transport.actions[0] != (ChatAction{ChatID: 100, ThreadID: 1, Action: "typing"}) {
		t.Fatalf("initial typing actions=%#v", transport.actions)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.activity", WorkerRef: "worker-1", AttemptID: "attempt-1"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("quiet worker activity emitted messages: %#v", transport.sent)
	}
	adapter.refreshWorkerTyping(ctx)
	if len(transport.actions) != 1 {
		t.Fatalf("typing throttle was bypassed: %#v", transport.actions)
	}

	clock = clock.Add(4 * time.Second)
	adapter.refreshWorkerTyping(ctx)
	if len(transport.actions) != 2 {
		t.Fatalf("active attempt was not refreshed: %#v", transport.actions)
	}
	adapter.typingMu.Lock()
	adapter.lastTyping = map[string]time.Time{}
	adapter.typingMu.Unlock()
	if err := adapter.HandleDurableEvent(ctx, Event{Sequence: 3, Kind: "worker.failed", WorkerRef: "worker-1", AttemptID: "attempt-1"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.actions) != 2 {
		t.Fatalf("terminal notification started typing: %#v", transport.actions)
	}
	clock = clock.Add(4 * time.Second)
	adapter.refreshWorkerTyping(ctx)
	if len(transport.actions) != 3 {
		t.Fatalf("terminal cleanup stopped another attempt: %#v", transport.actions)
	}
	if err := adapter.HandleDurableEvent(ctx, Event{Sequence: 4, Kind: "worker.canceled", WorkerRef: "worker-1", AttemptID: "attempt-2"}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(4 * time.Second)
	adapter.refreshWorkerTyping(ctx)
	if len(transport.actions) != 3 {
		t.Fatalf("typing continued after all attempts ended: %#v", transport.actions)
	}
}

func TestWorkerTypingSurvivesRestartAndTextlessTerminalReplay(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	started := Event{Sequence: 1, EventID: "started-1", Kind: "worker.started", WorkerRef: "worker-1", AttemptID: "attempt-1"}
	if err := adapter.HandleDurableEvent(ctx, started); err != nil {
		t.Fatal(err)
	}

	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := restarted.HandleDurableEvent(ctx, started); err != nil {
		t.Fatal(err)
	}
	if len(transport.actions) != 1 {
		t.Fatalf("duplicate start was processed after restart: %#v", transport.actions)
	}
	restarted.typingMu.Lock()
	restarted.lastTyping = map[string]time.Time{}
	restarted.typingMu.Unlock()
	restarted.refreshWorkerTyping(ctx)
	if len(transport.actions) != 2 || transport.actions[1].ThreadID != 1 {
		t.Fatalf("persisted active attempt did not refresh in its topic: %#v", transport.actions)
	}

	terminal := Event{Sequence: 2, EventID: "outcome-1", Kind: "", WorkerRef: "worker-1", AttemptID: "attempt-1", WorkerAttemptTerminal: true}
	if err := restarted.HandleDurableEvent(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	if err := restarted.HandleDurableEvent(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	restarted.typingMu.Lock()
	restarted.lastTyping = map[string]time.Time{}
	restarted.typingMu.Unlock()
	restarted.refreshWorkerTyping(ctx)
	if len(transport.actions) != 2 {
		t.Fatalf("textless terminal replay left typing active: %#v", transport.actions)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("textless terminal outcome emitted a message: %#v", transport.sent)
	}
}

func TestWorkerTypingRefreshLoopRenewsTyping(t *testing.T) {
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.started", WorkerRef: "worker-1", AttemptID: "attempt-1"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		adapter.refreshWorkerTypingLoop(ctx)
	}()
	time.Sleep(4200 * time.Millisecond)
	cancel()
	<-done

	transport.mu.Lock()
	actions := append([]ChatAction(nil), transport.actions...)
	transport.mu.Unlock()
	if len(actions) != 2 {
		t.Fatalf("typing refresh actions=%#v, want initial and periodic refresh", actions)
	}
}

func TestWorkerTypingRefreshLoopStopsWhenContextIsCanceled(t *testing.T) {
	transport := &contextWaitingTypingTransport{started: make(chan struct{})}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	adapter.mu.Lock()
	adapter.state.WorkerTyping[workerTypingKey("worker-1", "attempt-1")] = workerTypingAttempt{WorkerRef: "worker-1", AttemptID: "attempt-1", ChatID: 100, ThreadID: 1}
	adapter.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		adapter.refreshWorkerTypingLoop(ctx)
	}()
	select {
	case <-transport.started:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("typing refresh did not reach transport")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("typing refresh loop did not stop after context cancellation")
	}
}
