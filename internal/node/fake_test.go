package node

import (
	"context"
	"errors"
	"os"
	"testing"
)

type fakeRuntime struct {
	nonSteerable bool
	session      *fakeSession
	workspace    string
}

func (r *fakeRuntime) Start(_ context.Context, request StartRequest) (Session, error) {
	r.workspace = request.Workspace
	r.session = &fakeSession{id: "session-" + request.WorkerRef, nonSteerable: r.nonSteerable, activity: make(chan Activity, 4), result: make(chan Result, 1), queued: make(chan string, 2)}
	r.session.activity <- Activity{Kind: ActivityStatus, Text: "ready"}
	return r.session, nil
}

type fakeSession struct {
	id           string
	nonSteerable bool
	activity     chan Activity
	result       chan Result
	queued       chan string
	cancelled    bool
	closed       bool
}

func (s *fakeSession) ID() string                                 { return s.id }
func (s *fakeSession) Prompt(context.Context, string) error       { return nil }
func (s *fakeSession) Queue(_ context.Context, text string) error { s.queued <- text; return nil }
func (s *fakeSession) Steer(_ context.Context, text string) (bool, error) {
	if s.nonSteerable {
		return false, nil
	}
	s.activity <- Activity{Kind: ActivityText, Text: text}
	return true, nil
}
func (s *fakeSession) Cancel(_ context.Context) error {
	if s.closed {
		return errors.New("closed")
	}
	s.cancelled = true
	s.result <- Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *fakeSession) Activity() <-chan Activity { return s.activity }
func (s *fakeSession) Result() <-chan Result     { return s.result }
func (s *fakeSession) Close() error              { s.closed = true; return nil }

func TestLocalNodeAcceptsOnlyReadyRuntime(t *testing.T) {
	runtime := &fakeRuntime{}
	node := NewLocal(runtime)
	session, err := node.Dispatch(context.Background(), StartRequest{WorkerRef: "phone-42", Task: "research"})
	if err != nil || session.ID() != "session-phone-42" {
		t.Fatalf("session=%v err=%v", session, err)
	}
	if info, err := os.Stat(runtime.workspace); err != nil || !info.IsDir() {
		t.Fatalf("workspace %q was not created: %v", runtime.workspace, err)
	}
	activity := <-session.Activity()
	if activity.Kind != ActivityStatus || activity.Text != "ready" {
		t.Fatalf("activity=%#v", activity)
	}
	if _, err := node.Dispatch(context.Background(), StartRequest{WorkerRef: "phone-42"}); err == nil {
		t.Fatal("expected duplicate worker error")
	}
}

func TestLocalNodeQueuesNonSteerableInput(t *testing.T) {
	runtime := &fakeRuntime{nonSteerable: true}
	node := NewLocal(runtime)
	session, err := node.Dispatch(context.Background(), StartRequest{WorkerRef: "phone-42"})
	if err != nil {
		t.Fatal(err)
	}
	injected, err := session.Steer(context.Background(), "wait for idle")
	if err != nil || injected {
		t.Fatalf("steer injected=%v err=%v", injected, err)
	}
	queue, ok := session.(Queueer)
	if !ok {
		t.Fatal("fake session does not support queue")
	}
	if err := queue.Queue(context.Background(), "follow up"); err != nil {
		t.Fatal(err)
	}
	if got := <-runtime.session.queued; got != "follow up" {
		t.Fatalf("queued=%q", got)
	}
}

func TestLocalNodeForwardsSteerAndCancel(t *testing.T) {
	runtime := &fakeRuntime{}
	node := NewLocal(runtime)
	session, err := node.Dispatch(context.Background(), StartRequest{WorkerRef: "phone-42"})
	if err != nil {
		t.Fatal(err)
	}
	injected, err := session.Steer(context.Background(), "increase budget")
	if err != nil || !injected {
		t.Fatalf("steer injected=%v err=%v", injected, err)
	}
	if got := <-session.Activity(); got.Text != "ready" {
		t.Fatalf("first activity=%#v", got)
	}
	if got := <-session.Activity(); got.Text != "increase budget" {
		t.Fatalf("steer activity=%#v", got)
	}
	if err := session.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result := <-session.Result(); result.Status != "canceled" {
		t.Fatalf("result=%#v", result)
	}
}
