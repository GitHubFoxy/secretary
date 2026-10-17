package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// This transport keeps the first CreateForumTopic in flight until explicitly
// released. GetUpdates is withheld until that forum request has started, so
// the test proves that polling continues WHILE topic recovery is blocked.
type stalledForumTransport struct {
	mu           sync.Mutex
	forumStarted chan struct{}
	releaseForum chan struct{}
	forumReady   chan struct{}
	sent         chan OutgoingMessage
	topicCalls   int
}

func (f *stalledForumTransport) GetUpdates(ctx context.Context, offset int64, _ time.Duration) ([]Update, error) {
	if offset <= 1 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.forumStarted:
			return []Update{{ID: 1, Message: &Message{ChatID: 100, FromID: 100, ChatType: "private", Text: "DM while forum blocked"}}}, nil
		}
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (f *stalledForumTransport) SendMessage(_ context.Context, msg OutgoingMessage) error {
	f.sent <- msg
	return nil
}
func (f *stalledForumTransport) SendChatAction(context.Context, int64, int64, string) error {
	return nil
}
func (f *stalledForumTransport) CreateForumTopic(ctx context.Context, chatID int64, name string) (ForumTopic, error) {
	f.mu.Lock()
	f.topicCalls++
	n := f.topicCalls
	f.mu.Unlock()
	if n == 1 {
		close(f.forumStarted)
		select {
		case <-ctx.Done():
			return ForumTopic{}, ctx.Err()
		case <-f.releaseForum:
			return ForumTopic{}, errors.New("temporary forum permissions failure")
		}
	}
	if n == 2 {
		close(f.forumReady)
	}
	return ForumTopic{ChatID: chatID, ThreadID: 42, Name: name}, nil
}

type isolatedDMServer struct{ delivered chan InboundMessage }
func (s *isolatedDMServer) SendMessage(_ context.Context, msg InboundMessage) error {
	s.delivered <- msg
	return nil
}
func (*isolatedDMServer) SendWorkerMessage(context.Context, WorkerMessage) error { return nil }
func (*isolatedDMServer) ResumeWorker(context.Context, string) error { return nil }

func TestT04RunDMAndFlushContinueWhileForumSyncBlockedAndRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := &stalledForumTransport{
		forumStarted: make(chan struct{}),
		releaseForum: make(chan struct{}),
		forumReady: make(chan struct{}),
		sent: make(chan OutgoingMessage, 4),
	}
	srv := &isolatedDMServer{delivered: make(chan InboundMessage, 1)}
	group := int64(-200)
	a, err := New(Config{
		StatePath: filepath.Join(t.TempDir(), "state.json"),
		OwnerChatID: 100, GroupChatID: &group,
		ExistingWorkers: func(context.Context) ([]WorkerTopic, error) {
			return []WorkerTopic{{WorkerRef: "existing", Title: "Existing worker"}}, nil
		},
		PollInterval: 10 * time.Millisecond, FlushInterval: time.Hour,
	}, tr, srv)
	if err != nil { t.Fatal(err) }
	// A pending Secretary batch must flush even if topic creation hangs.
	if err := a.HandleDurableEvent(ctx, Event{Sequence: 1, Kind: "secretary.text_delta", Text: "Secretary reply"}); err != nil { t.Fatal(err) }
	if err := a.HandleDurableEvent(ctx, Event{Sequence: 2, Kind: "secretary.turn.finished"}); err != nil { t.Fatal(err) }
	runDone := make(chan error, 1)
	go func() { runDone <- a.Run(ctx) }()
	await := func(label string, channel interface{}) {
		t.Helper()
		switch c := channel.(type) {
		case <-chan struct{}:
			select { case <-c: case <-time.After(3*time.Second): t.Fatalf("%s: timeout", label) }
		case <-chan InboundMessage:
			select {
			case msg := <-c:
				if msg.Body != "DM while forum blocked" { t.Fatalf("%s: unexpected DM %#v", label, msg) }
			case <-time.After(3*time.Second): t.Fatalf("%s: timeout", label)
			}
		}
	}
	await("forum started", (<-chan struct{})(tr.forumStarted))
	await("DM delivered while forum still blocked", (<-chan InboundMessage)(srv.delivered))
	var destinations = map[int64]bool{}
	for len(destinations) < 2 {
		select {
		case msg := <-tr.sent:
			if msg.Text != "Secretary reply" { t.Fatalf("unexpected Flush payload %#v", msg) }
			destinations[msg.ChatID] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("Flush did not complete with broken forum: %#v", destinations)
		}
	}
	if !destinations[100] || !destinations[-200] {
		t.Fatalf("Secretary outbox did not flush to both destinations: %#v", destinations)
	}
	// Release the stalled request: the first call fails, but topic sync retries
	// independently and can recover without a new inbound update.
	close(tr.releaseForum)
	await("forum topic recovery retry", (<-chan struct{})(tr.forumReady))
	cancel()
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) { t.Fatalf("Run exit: %v", err) }
	case <-time.After(3*time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestT04DistinctDurableEventsSameRequestIDAreNotSentAgain(t *testing.T) {
	cases := []struct{
		name string
		firstKind string
		replayedKind string
		payload json.RawMessage
	}{
		{name: "needs-input", firstKind: "worker.needs_input", replayedKind: "approval.requested", payload: json.RawMessage(`{"request_id":"req-1","kind":"input"}`)},
		{name: "approval", firstKind: "worker.approval_requested", replayedKind: "approval.requested", payload: json.RawMessage(`{"request_id":"req-1","kind":"permission"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name,func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "state.json")
			tr := &fakeTransport{}
			a := newT04Adapter(t, path, tr, &fakeServer{}, -200)
			first := Event{Sequence: 1, EventID: "first", Kind: tc.firstKind, WorkerRef: "w1", Payload: tc.payload}
			if err := a.HandleDurableEvent(ctx, first); err != nil { t.Fatal(err) }
			if len(tr.sent) != 1 || len(a.state.Delivered) != 0 || len(a.state.RequestNotified) != 1 {
				t.Fatalf("first request must deliver once and compact event receipt: sent=%#v receipts=%#v semantic=%#v", tr.sent, a.state.Delivered, a.state.RequestNotified)
			}
			a = newT04Adapter(t, path, tr, &fakeServer{}, -200)
			replayed := first
			replayed.Kind = tc.replayedKind
			replayed.Sequence = 2
			replayed.EventID = "different-event"
			if err := a.HandleDurableEvent(ctx, replayed); err != nil { t.Fatal(err) }
			if len(tr.sent) != 1 || a.LastEventSeq() != 2 {
				t.Fatalf("same request produced duplicate notification: sent=%#v cursor=%d", tr.sent, a.LastEventSeq())
			}
			different := first
			different.Sequence = 3
			different.EventID = "new-request"
			different.Payload = json.RawMessage(`{"request_id":"req-2","kind":"permission"}`)
			if err := a.HandleDurableEvent(ctx, different); err != nil { t.Fatal(err) }
			if len(tr.sent) != 2 || a.state.Topics["w1"].PendingRequestID != "req-2" {
				t.Fatalf("new request incorrectly suppressed: sent=%#v pending=%#v", tr.sent, a.state.Topics["w1"])
			}
			oldAgain := replayed
			oldAgain.Sequence = 4
			oldAgain.EventID = fmt.Sprintf("stale-%s", tc.name)
			if err := a.HandleDurableEvent(ctx, oldAgain); err != nil { t.Fatal(err) }
			if len(tr.sent) != 2 || a.state.Topics["w1"].PendingRequestID != "req-2" {
				t.Fatalf("stale request replay resurrected old pending ID: sent=%#v pending=%#v", tr.sent, a.state.Topics["w1"])
			}
		})
	}
}

func TestT04FailedRequestDeliveryIsRetriedBeforeSemanticReceipt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	tr := &t04FlakyTransport{failAt: 1}
	a := newT04Adapter(t, path, tr, &fakeServer{}, -200)
	event := Event{Sequence: 1, Kind: "worker.needs_input", EventID: "request", WorkerRef: "w1", Payload: json.RawMessage(`{"request_id":"id-1"}`)}
	if err := a.HandleDurableEvent(ctx, event); err == nil {
		t.Fatal("expected initial topic send failure")
	}
	if len(a.state.RequestNotified) != 0 || a.LastEventSeq() != 0 {
		t.Fatalf("premature semantic receipt: %#v seq=%d", a.state.RequestNotified, a.LastEventSeq())
	}
	tr.failAt = 0
	a = newT04Adapter(t, path, tr, &fakeServer{}, -200)
	if err := a.HandleDurableEvent(ctx, event); err != nil { t.Fatal(err) }
	if len(tr.sent) != 1 || len(a.state.RequestNotified) != 1 || a.LastEventSeq() != 1 {
		t.Fatalf("failed request wasn't safely retried: sent=%#v receipts=%#v", tr.sent, a.state.RequestNotified)
	}
}
