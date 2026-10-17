package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestHeartbeatMissingPreviouslyReportedAttemptMarksOnlyItsWorkerOffline(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "heartbeat-missing.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	if _, err := store.EnrollNode(ctx, "remote"); err != nil { t.Fatal(err) }
	inventory := registryInventoryFixture("remote", time.Now().UTC())
	if err := store.MarkNodeConnected(ctx, "remote", inventory); err != nil { t.Fatal(err) }

	type runningWorker struct {
		worker Worker
		turn Turn
		attempt Phase4Attempt
	}
	makeWorker := func(ref string) runningWorker {
		t.Helper()
		worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
			WorkerRef: ref, Intent: "test liveness", ProjectID: "project",
			NodeID: "remote", HarnessInstanceID: "remote/fx",
		}, TurnSpec{Input: "perform task"})
		if err != nil { t.Fatal(err) }
		if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
		return runningWorker{worker, turn, attempt}
	}
	lost := makeWorker("lost-worker")
	healthy := makeWorker("healthy-worker")
	waitingInput := makeWorker("waiting-input-worker")
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, waitingInput.attempt.ID); err != nil { t.Fatal(err) }
	has := func(r runningWorker) NodeActiveAttempt {
		return NodeActiveAttempt{WorkerRef: r.worker.WorkerRef, TurnID: r.turn.ID, AttemptID: r.attempt.ID}
	}
	check := func(r runningWorker, want WorkerStatus) {
		t.Helper()
		details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, r.worker.WorkerRef)
		if err != nil { t.Fatal(err) }
		if details.Worker.Status != want || details.CurrentAttempt().State != AttemptActive {
			t.Fatalf("%s status=%s attempt=%s, want %s/active", r.worker.WorkerRef,
				details.Worker.Status, details.CurrentAttempt().State, want)
		}
	}
	initial := NodeHeartbeat{Capacity: 3, ActiveAttempts: []NodeActiveAttempt{has(lost), has(healthy), has(waitingInput)}}
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, initial); err != nil { t.Fatal(err) }
	check(lost, WorkerWorking)
	check(healthy, WorkerWorking)
	check(waitingInput, WorkerNeedsInput)

	// The socket stays online. The native process has stopped reporting two
	// previously confirmed Attempts, but continues to report the third one.
	remaining := NodeHeartbeat{Capacity: 3, ActiveAttempts: []NodeActiveAttempt{has(healthy)}}
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, remaining); err != nil { t.Fatal(err) }
	check(lost, WorkerOffline)
	check(healthy, WorkerWorking)
	check(waitingInput, WorkerOffline)
	state, err := store.NodeRecord(ctx, "remote")
	if err != nil || !state.Online { t.Fatalf("the Node disconnected unexpectedly: %#v %v", state, err) }
	if result, err := store.Phase4Result(ctx, lost.turn.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("heartbeat disappearance fabricated a Result: %#v %v", result, err)
	}

	// An unrelated/stale identity must not revive the lost native session.
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, NodeHeartbeat{Capacity: 3,
		ActiveAttempts: []NodeActiveAttempt{has(healthy), {WorkerRef: lost.worker.WorkerRef, TurnID: lost.turn.ID, AttemptID: "stale"}}}); err != nil {
		t.Fatal(err)
	}
	check(lost, WorkerOffline)

	// An exact fresh binding can restore the waiting-input semantics, and
	// another disappearance returns it to offline without finalizing it.
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, NodeHeartbeat{Capacity: 3,
		ActiveAttempts: []NodeActiveAttempt{has(healthy), has(waitingInput)}}); err != nil { t.Fatal(err) }
	check(waitingInput, WorkerNeedsInput)
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, remaining); err != nil { t.Fatal(err) }
	check(waitingInput, WorkerOffline)

	// A terminal Result that arrives AFTER disappearance must still win.
	late := AttemptOutcomeEnvelope{
		EventID: "terminal-after-disappearance", Node: "remote", HarnessInstanceID: "remote/fx",
		WorkerRef: lost.worker.WorkerRef, TurnID: lost.turn.ID, AttemptID: lost.attempt.ID,
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "late final output",
	}
	if _, _, duplicate, err := store.RecordNodeAttemptOutcome(ctx, late); err != nil || duplicate {
		t.Fatalf("late terminal Result rejected: duplicate=%v err=%v", duplicate, err)
	}
	if _, _, duplicate, err := store.RecordNodeAttemptOutcome(ctx, late); err != nil || !duplicate {
		t.Fatalf("late Result replay not idempotent: duplicate=%v err=%v", duplicate, err)
	}
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, lost.worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if details.Worker.Status != WorkerIdle || details.CurrentAttempt().State != AttemptSucceeded || len(details.Results) != 1 {
		t.Fatalf("terminal Result did not settle offline Worker exactly once: %#v", details)
	}
	check(healthy, WorkerWorking)
}
