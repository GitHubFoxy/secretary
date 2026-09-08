package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestRunnerCancelsTaskClosingOutsideServerProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "closing-runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "close me")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &Dispatcher{Store: store, Node: node.NewLocal(runtime{})}
	if _, _, err := dispatcher.Dispatch(ctx, task, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Store: store, Dispatcher: dispatcher, Conversation: conversation.ID, Interval: time.Millisecond}
	go runner.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, readErr := store.Task(ctx, task.ID)
		if readErr == nil && current.State == core.TaskClosed {
			return
		}
		time.Sleep(time.Millisecond)
	}
	current, _ := store.Task(ctx, task.ID)
	t.Fatalf("closing task was not canceled by Runner: %#v", current)
}

func TestRunnerDispatchesDurableTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "run me")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &Dispatcher{Store: store, Node: node.NewLocal(runtime{})}
	runner := Runner{Store: store, Dispatcher: dispatcher, Conversation: conversation.ID, Interval: time.Millisecond}
	go runner.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		current, readErr := store.Task(ctx, task.ID)
		if readErr == nil && current.State == core.TaskOpen {
			return
		}
		time.Sleep(time.Millisecond)
	}
	current, _ := store.Task(ctx, task.ID)
	t.Fatalf("task was not dispatched: %#v", current)
}
