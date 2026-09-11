package core

import (
	"context"
	"testing"
)

func TestChildTasksAreBoundedAndResultsStayOutOfConversation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateTask(ctx, conversation.ID, "parent")
	if err != nil {
		t.Fatal(err)
	}
	_, parentBinding, parentAttempt, err := store.AcceptDispatch(ctx, parent.ID, "parent-worker", "local", "parent-session", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetAttemptActive(ctx, parentAttempt.ID); err != nil {
		t.Fatal(err)
	}
	var children []Task
	for i := 0; i < MaxChildrenPerAttempt; i++ {
		child, err := store.CreateChildTask(ctx, parentBinding.WorkerRef, parentAttempt.ID, "child")
		if err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
	}
	if _, err := store.CreateChildTask(ctx, parentBinding.WorkerRef, parentAttempt.ID, "too many"); err == nil {
		t.Fatal("expected child limit")
	}
	child := children[0]
	_, binding, attempt, err := store.AcceptDispatch(ctx, child.ID, child.ID, "local", "child-session", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if binding.ParentBindingID != parentBinding.ID || binding.ParentAttemptID != parentAttempt.ID {
		t.Fatalf("binding=%#v", binding)
	}
	if _, err := store.SetAttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteAttempt(ctx, attempt.ID, ResultSucceeded, "child done"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("child leaked conversation entry: %#v", entries)
	}
	details, err := store.TaskDetails(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Children) != MaxChildrenPerAttempt {
		t.Fatalf("children=%d", len(details.Children))
	}
	if details.Children[0].Task.State != TaskClosed || len(details.Children[0].Results) != 1 {
		t.Fatalf("child details=%#v", details.Children[0])
	}
}
