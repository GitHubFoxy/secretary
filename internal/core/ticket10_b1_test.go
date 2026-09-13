package core

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTicket10TerminalConversationEntriesCarryResultIdentityPerTurn(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ticket10-b1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, firstTurn, firstAttempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "worker-one", Intent: "inspect", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "private",
	}, TurnSpec{Input: "first"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.FinishAttempt(ctx, firstAttempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "same summary"}})
	if err != nil || first.Result == nil {
		t.Fatalf("finish first: result=%#v err=%v", first.Result, err)
	}
	secondTurn, secondAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "second"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.FinishAttempt(ctx, secondAttempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "same summary"}})
	if err != nil || second.Result == nil {
		t.Fatalf("finish second: result=%#v err=%v", second.Result, err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var results []ConversationEntry
	for _, entry := range entries {
		if entry.Kind == EntryWorkerResult {
			results = append(results, entry)
		}
	}
	if len(results) != 2 {
		t.Fatalf("worker result entries=%d want=2: %#v", len(results), results)
	}
	want := []struct{ turnID, resultID string }{{firstTurn.ID, first.Result.ID}, {secondTurn.ID, second.Result.ID}}
	for i, entry := range results {
		if entry.WorkerRef != worker.WorkerRef || entry.TurnID != want[i].turnID || entry.ResultID != want[i].resultID || entry.Body != "same summary" {
			t.Fatalf("entry[%d]=%#v want worker=%q turn=%q result=%q", i, entry, worker.WorkerRef, want[i].turnID, want[i].resultID)
		}
	}
}
