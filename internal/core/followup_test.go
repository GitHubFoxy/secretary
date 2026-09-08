package core

import (
	"context"
	"testing"
)

func TestFollowUpCreatesNewAttemptAndInputEntry(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "initial")
	if err != nil {
		t.Fatal(err)
	}
	_, _, first, err := store.AcceptDispatch(ctx, task.ID, "worker", "local", "session")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CompleteAttempt(ctx, first.ID, ResultSucceeded, "done"); err != nil {
		t.Fatal(err)
	}
	followUp, err := store.CreateFollowUpAttempt(ctx, task.ID, "continue with tests")
	if err != nil {
		t.Fatal(err)
	}
	if followUp.Number != 2 || followUp.State != AttemptStarting {
		t.Fatalf("follow-up=%#v", followUp)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Kind != EntryWorkerInput || entries[1].Body != "continue with tests" {
		t.Fatalf("entries=%#v", entries)
	}
}
