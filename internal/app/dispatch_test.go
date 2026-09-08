package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type runtime struct{}
type session struct{ result chan node.Result }

func (runtime) Start(_ context.Context, _ node.StartRequest) (node.Session, error) {
	return &session{result: make(chan node.Result, 1)}, nil
}
func (s *session) ID() string                                  { return "runtime-session" }
func (s *session) Prompt(context.Context, string) error        { return nil }
func (s *session) Steer(context.Context, string) (bool, error) { return true, nil }
func (s *session) Cancel(context.Context) error {
	s.result <- node.Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *session) Activity() <-chan node.Activity { return make(chan node.Activity) }
func (s *session) Result() <-chan node.Result     { return s.result }
func (s *session) Close() error                   { return nil }

func TestDispatcherPersistsTerminalResult(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, _ := store.CreatePersonWithConversation(ctx)
	task, _ := store.CreateTask(ctx, conversation.ID, "work")
	d := Dispatcher{Store: store, Node: node.NewLocal(runtime{})}
	_, attempt, err := d.Dispatch(ctx, task, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := d.Node.Session("worker-1")
	s.(*session).result <- node.Result{Status: "succeeded", Summary: "done"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, _ := store.EntriesAfter(ctx, conversation.ID, 0)
		if len(entries) == 1 && entries[0].Body == "done" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("result for %s was not persisted", attempt.ID)
}
