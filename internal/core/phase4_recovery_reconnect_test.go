package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPhase4RecoveryAwaitsRemoteNodeAndAcceptsLateOutcomeOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "remote"); err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "recovered-worker", Intent: "produce file", ProjectID: "project",
		NodeID: "remote", HarnessInstanceID: "remote/fx", PolicySnapshot: "{}",
	}, TurnSpec{Input: "produce file"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.MarkAllNodesOffline(ctx); err != nil {
		t.Fatal(err)
	}
	resolver := Phase4AttemptRecoveryFunc(func(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error) {
		return Phase4RecoveryAwaiting, nil
	})
	for i := 0; i < 2; i++ {
		if err := reopened.RecoverPhase4Attempts(ctx, resolver); err != nil {
			t.Fatal(err)
		}
	}
	current, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.Worker.Status != WorkerOffline || current.CurrentAttempt().State != AttemptActive {
		t.Fatalf("unknown remote operation must remain unresolved: %#v", current)
	}
	if _, err := reopened.Phase4Result(ctx, turn.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("recovery fabricated a terminal Result: %v", err)
	}
	inventory := registryInventoryFixture("remote", time.Now().UTC())
	// A stale or foreign heartbeat must not revive the Worker.
	if err := reopened.UpdateNodeHeartbeat(ctx, "remote", inventory, NodeHeartbeat{
		Capacity: 1, ActiveAttempts: []NodeActiveAttempt{{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: "wrong-attempt"}},
	}); err != nil {
		t.Fatal(err)
	}
	current, _ = reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if current.Worker.Status != WorkerOffline {
		t.Fatal("foreign heartbeat revived an unresolved Worker")
	}
	if err := reopened.UpdateNodeHeartbeat(ctx, "remote", inventory, NodeHeartbeat{
		Capacity: 1, ActiveAttempts: []NodeActiveAttempt{{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	current, _ = reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if current.Worker.Status != WorkerWorking {
		t.Fatalf("matching heartbeat did not restore liveness: %s", current.Worker.Status)
	}
	late := AttemptOutcomeEnvelope{
		EventID: "late-event", Node: "remote", HarnessInstanceID: "remote/fx",
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "real output",
	}
	if _, _, duplicate, err := reopened.RecordNodeAttemptOutcome(ctx, late); err != nil || duplicate {
		t.Fatalf("late outcome rejected: duplicate=%v err=%v", duplicate, err)
	}
	if _, _, duplicate, err := reopened.RecordNodeAttemptOutcome(ctx, late); err != nil || !duplicate {
		t.Fatalf("replayed outcome not idempotent: duplicate=%v err=%v", duplicate, err)
	}
	entries, err := reopened.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Kind == EntryWorkerResult {
			count++
			if entry.ResultID == "" || entry.Body != "real output" {
				t.Fatalf("wrong terminal projection: %#v", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("late/replayed outcomes created %d Results, want one", count)
	}
	if err := reopened.MarkNodeDisconnected(ctx, "remote"); err != nil {
		t.Fatal(err)
	}
	current, _ = reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if current.Worker.Status != WorkerIdle || current.CurrentAttempt().State != AttemptSucceeded {
		t.Fatalf("terminal Worker changed after disconnect: %#v", current)
	}
}
