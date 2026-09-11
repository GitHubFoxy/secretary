package secretary

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type fakeRuntime struct {
	session *fakeSession
	request node.StartRequest
}
type fakeSession struct {
	id         string
	steerable  bool
	prompts    chan string
	activities chan node.Activity
	results    chan node.Result
	closed     bool
}

func (r *fakeRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.request = request
	r.session = &fakeSession{id: "secretary-session", steerable: true, prompts: make(chan string, 4), activities: make(chan node.Activity, 4), results: make(chan node.Result, 4)}
	return r.session, nil
}
func (s *fakeSession) ID() string { return s.id }
func (s *fakeSession) Prompt(_ context.Context, text string) error {
	s.prompts <- text
	s.results <- node.Result{Status: "succeeded", Summary: "turn done"}
	return nil
}
func (s *fakeSession) Steer(_ context.Context, text string) (bool, error) {
	if !s.steerable {
		return false, nil
	}
	s.activities <- node.Activity{Kind: node.ActivityText, Text: text}
	return true, nil
}
func (s *fakeSession) Cancel(context.Context) error {
	s.results <- node.Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *fakeSession) Activity() <-chan node.Activity { return s.activities }
func (s *fakeSession) Result() <-chan node.Result     { return s.results }
func (s *fakeSession) Close() error                   { s.closed = true; return nil }

func TestRuntimeUsesCapabilityAndSteersActiveSecretary(t *testing.T) {
	rt := &fakeRuntime{}
	runtime := node.NewLocal(rt)
	secretary := NewRuntime(runtime, "cap-123")
	if err := secretary.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rt.request.WorkerRef != "secretary" || rt.request.Task == "" || contains(rt.request.Task, "cap-123") {
		t.Fatalf("request=%#v", rt.request)
	}
	if err := secretary.HandleMessage(context.Background(), "change direction"); err != nil {
		t.Fatal(err)
	}
	select {
	case activity := <-rt.session.activities:
		if activity.Text != "change direction" {
			t.Fatalf("activity=%#v", activity)
		}
	case <-time.After(time.Second):
		t.Fatal("steering was not delivered")
	}
}

func TestRuntimePersistsResponseEntries(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{}
	secretary := NewRuntime(node.NewLocal(rt), "cap")
	secretary.AttachConversation(store, conversation.ID)
	if err := secretary.Start(ctx); err != nil {
		t.Fatal(err)
	}
	rt.session.steerable = false
	if err := secretary.HandleMessage(ctx, "answer this"); err != nil {
		t.Fatal(err)
	}
	rt.session.results <- node.Result{Status: "succeeded", Summary: "initial ready"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, readErr := store.EntriesAfter(ctx, conversation.ID, 0)
		if readErr == nil && len(entries) == 1 && entries[0].Kind == core.EntrySecretary && entries[0].Body == "turn done" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	entries, _ := store.EntriesAfter(ctx, conversation.ID, 0)
	t.Fatalf("Secretary response was not persisted: %#v", entries)
}

func TestRuntimeQueuesFollowUpUntilIdle(t *testing.T) {
	rt := &fakeRuntime{}
	rt.session = nil
	local := node.NewLocal(rt)
	secretary := NewRuntime(local, "cap")
	if err := secretary.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	rt.session.steerable = false
	if err := secretary.HandleMessage(context.Background(), "/q follow up"); err != nil {
		t.Fatal(err)
	}
	// Initial Secretary turn reaches its safe boundary here.
	rt.session.results <- node.Result{Status: "succeeded", Summary: "ready"}
	select {
	case prompt := <-rt.session.prompts:
		if prompt != "follow up" {
			t.Fatalf("prompt=%q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("queued prompt was not delivered")
	}
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
