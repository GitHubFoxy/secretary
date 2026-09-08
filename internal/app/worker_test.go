package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestWorkerControllerPersistsFollowUpBeforeQueueing(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "initial")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &Dispatcher{Store: store, Node: node.NewLocal(runtime{})}
	_, _, err = dispatcher.Dispatch(ctx, task, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	workerSession, _ := dispatcher.Node.Session(task.ID)
	fake := workerSession.(*session)
	fake.result <- node.Result{Status: "succeeded", Summary: "first result"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		details, readErr := store.TaskDetails(ctx, task.ID)
		if readErr == nil && len(details.Results) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	controller := WorkerController{Store: store, Node: dispatcher.Node}
	attempt, err := controller.Queue(ctx, task.ID, "follow-up input")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Number != 2 || attempt.State != core.AttemptStarting {
		t.Fatalf("attempt=%#v", attempt)
	}
	if got := <-fake.queued; got != "follow-up input" {
		t.Fatalf("queued=%q", got)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Kind != core.EntryWorkerInput {
		t.Fatalf("entries=%#v", entries)
	}
}
