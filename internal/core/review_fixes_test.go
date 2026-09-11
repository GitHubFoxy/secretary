package core

import (
	"context"
	"testing"
)

func TestFinishAttemptRetryableIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, first, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "retry atomically"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}

	finished, err := store.FinishAttempt(ctx, first.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{
			Status:         OutcomeFailed,
			Classification: OutcomeRetryable,
			ErrorCode:      "temporary",
			Diagnostics:    "adapter reset",
		},
		RetryCommandID: "retry-command-atomic",
	})
	if err != nil || finished.Duplicate || finished.Result != nil || finished.NextAttempt == nil {
		t.Fatalf("finish=%#v err=%v", finished, err)
	}
	if finished.Outcome.Classification != OutcomeRetryable || finished.NextAttempt.Number != 2 {
		t.Fatalf("retry finish=%#v", finished)
	}
	currentTurn, err := store.Turn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentWorker, err := store.Worker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	currentFirst, err := store.Phase4Attempt(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentFirst.State != AttemptFailed || currentTurn.CurrentAttemptID != finished.NextAttempt.ID || currentTurn.State != TurnStarting || currentWorker.Status != WorkerWorking || currentWorker.CurrentTurnID != turn.ID {
		t.Fatalf("inconsistent retry state: attempt=%#v turn=%#v worker=%#v", currentFirst, currentTurn, currentWorker)
	}

	repeated, err := store.FinishAttempt(ctx, first.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary"},
		RetryCommandID:      "retry-command-atomic",
	})
	if err != nil || !repeated.Duplicate || repeated.Outcome.ID != finished.Outcome.ID || repeated.NextAttempt == nil || repeated.NextAttempt.ID != finished.NextAttempt.ID {
		t.Fatalf("duplicate finish=%#v err=%v", repeated, err)
	}
	var attempts, outcomes int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempts WHERE turn_id = ?`, turn.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempt_outcomes WHERE attempt_id = ?`, first.ID).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || outcomes != 1 {
		t.Fatalf("duplicate created records: attempts=%d outcomes=%d", attempts, outcomes)
	}
}

func TestRecordAttemptOutcomeRejectsRetryableWithoutAtomicNextAttempt(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "reject unsafe retry"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary"})
	if err == nil {
		t.Fatal("keyless retryable outcome was accepted")
	}
	storedAttempt, err := store.Phase4Attempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedAttempt.State != AttemptStarting {
		t.Fatalf("unsafe retry mutated attempt: %#v", storedAttempt)
	}
}

func TestFinishAttemptFinalIsIdempotentAfterTerminalTurn(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "finish once"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	input := FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}}
	first, err := store.FinishAttempt(ctx, attempt.ID, input)
	if err != nil || first.Result == nil || first.Duplicate {
		t.Fatalf("first final=%#v err=%v", first, err)
	}
	second, err := store.FinishAttempt(ctx, attempt.ID, input)
	if err != nil || !second.Duplicate || second.Result == nil || second.Result.ID != first.Result.ID {
		t.Fatalf("duplicate final=%#v err=%v", second, err)
	}
	storedTurn, err := store.Turn(ctx, turn.ID)
	if err != nil || !storedTurn.State.Terminal() || storedTurn.ResultID != first.Result.ID {
		t.Fatalf("terminal turn=%#v err=%v", storedTurn, err)
	}
}

func TestPhase4RecoveryUsesExplicitResolver(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, aliveAttempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "alive"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, aliveAttempt.ID); err != nil {
		t.Fatal(err)
	}
	unknownSpec := phase4WorkerSpec()
	unknownSpec.WorkerRef = "worker-phase4-unknown"
	_, unknownTurn, unknownAttempt, err := store.CreateWorker(ctx, conversation.ID, unknownSpec, TurnSpec{Input: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverPhase4Attempts(ctx, nil); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.Phase4Attempt(ctx, unknownAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.State != AttemptStarting || unknownTurn.CurrentAttemptID != unknownAttempt.ID {
		t.Fatalf("no-probe recovery changed state: %#v", unchanged)
	}

	resolver := Phase4AttemptRecoveryFunc(func(_ context.Context, attempt Phase4Attempt) (Phase4RecoveryDecision, error) {
		if attempt.ID == aliveAttempt.ID {
			return Phase4RecoveryAlive, nil
		}
		return Phase4RecoveryUnknown, nil
	})
	if err := store.RecoverPhase4Attempts(ctx, resolver); err != nil {
		t.Fatal(err)
	}
	alive, err := store.Phase4Attempt(ctx, aliveAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if alive.State != AttemptActive {
		t.Fatalf("alive attempt changed: %#v", alive)
	}
	unknown, err := store.Phase4Attempt(ctx, unknownAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.State != AttemptInterrupted {
		t.Fatalf("unknown attempt=%#v", unknown)
	}
	result, err := store.Phase4Result(ctx, unknownTurn.ID)
	if err != nil || result.Status != ResultInterrupted {
		t.Fatalf("unknown result=%#v err=%v", result, err)
	}
	_, _, duplicate, err := store.RecordAttemptOutcome(ctx, unknownAttempt.ID, AttemptOutcomeInput{Status: OutcomeInterrupted, Classification: OutcomeFinal, Summary: "late"})
	if err != nil || !duplicate {
		t.Fatalf("repeated unknown recovery duplicate=%v err=%v", duplicate, err)
	}
}
