package core

import (
	"context"
	"testing"
)

func TestWorkerCapabilityIsScopedAndRotated(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "child work")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.IssueWorkerCapability(ctx, task.ID, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := store.AuthorizeWorkerCapability(ctx, "worker-1", first)
	if err != nil || !allowed {
		t.Fatalf("first allowed=%v err=%v", allowed, err)
	}
	if allowed, err := store.AuthorizeWorkerCapability(ctx, "worker-2", first); err != nil || allowed {
		t.Fatalf("wrong worker allowed=%v err=%v", allowed, err)
	}
	second, err := store.IssueWorkerCapability(ctx, task.ID, "worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.AuthorizeWorkerCapability(ctx, "worker-1", first); err != nil || allowed {
		t.Fatalf("rotated token allowed=%v err=%v", allowed, err)
	}
	if allowed, err := store.AuthorizeWorkerCapability(ctx, "worker-1", second); err != nil || !allowed {
		t.Fatalf("second allowed=%v err=%v", allowed, err)
	}
}
