package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func phase4WorkerSpec() WorkerSpec {
	return WorkerSpec{
		WorkerRef:         "worker-phase4",
		Title:             "Fix header",
		Intent:            "Fix the mobile header",
		ProjectID:         "project-1",
		NodeID:            "node-1",
		HarnessInstanceID: "node-1/fx",
		PolicySnapshot:    `{"harness":"fx","model":"default"}`,
	}
}

func TestPhase4RetryHasOneOutcomePerAttemptAndOneResultPerTurn(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, first, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "fix it"})
	if err != nil {
		t.Fatal(err)
	}
	if worker.CurrentTurnID != turn.ID || first.WorkerID != worker.ID || first.NodeID != "node-1" {
		t.Fatalf("created worker/turn/attempt = %#v %#v %#v", worker, turn, first)
	}
	var legacyTasks int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`).Scan(&legacyTasks); err != nil {
		t.Fatal(err)
	}
	if legacyTasks != 0 {
		t.Fatalf("Phase 4 Worker created legacy Task rows: %d", legacyTasks)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	input := FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary", Diagnostics: "adapter reset"}, RetryCommandID: "retry-command-1"}
	finished, err := store.FinishAttempt(ctx, first.ID, input)
	if err != nil || finished.Duplicate || finished.Result != nil || finished.Outcome.Classification != OutcomeRetryable || finished.NextAttempt == nil {
		t.Fatalf("retryable finish=%#v err=%v", finished, err)
	}
	again, err := store.FinishAttempt(ctx, first.ID, input)
	if err != nil || !again.Duplicate || again.Outcome.ID != finished.Outcome.ID || again.NextAttempt == nil || again.NextAttempt.ID != finished.NextAttempt.ID {
		t.Fatalf("duplicate finish=%#v err=%v", again, err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil || len(entries) != 0 {
		t.Fatalf("retryable outcome leaked entries=%#v err=%v", entries, err)
	}

	second := *finished.NextAttempt
	if second.Number != 2 || second.TurnID != turn.ID {
		t.Fatalf("retry attempt=%#v", second)
	}
	repeatedRetry, err := store.RetryAttempt(ctx, turn.ID)
	if err != nil || repeatedRetry.ID != second.ID {
		t.Fatalf("repeated retry=%#v err=%v", repeatedRetry, err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	finalOutcome, finalResult, duplicate, err := store.RecordAttemptOutcome(ctx, second.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "fixed"})
	if err != nil || duplicate || finalResult == nil || finalOutcome.Status != OutcomeSucceeded {
		t.Fatalf("final outcome=%#v result=%#v duplicate=%v err=%v", finalOutcome, finalResult, duplicate, err)
	}
	_, _, duplicate, err = store.RecordAttemptOutcome(ctx, second.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "fixed"})
	if err != nil || !duplicate {
		t.Fatalf("duplicate final outcome duplicate=%v err=%v", duplicate, err)
	}
	repeatedRetryAfterCompletion, err := store.RetryAttempt(ctx, turn.ID)
	if err != nil || repeatedRetryAfterCompletion.ID != second.ID {
		t.Fatalf("late repeated retry=%#v err=%v", repeatedRetryAfterCompletion, err)
	}
	storedTurn, err := store.Turn(ctx, turn.ID)
	if err != nil || storedTurn.State != TurnSucceeded || storedTurn.ResultID != finalResult.ID {
		t.Fatalf("stored turn=%#v err=%v", storedTurn, err)
	}
	storedResult, err := store.Phase4Result(ctx, turn.ID)
	if err != nil || storedResult.ID != finalResult.ID {
		t.Fatalf("stored result=%#v err=%v", storedResult, err)
	}
	entries, err = store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil || len(entries) != 1 || entries[0].Kind != EntryWorkerResult || entries[0].Body != "fixed" {
		t.Fatalf("final entries=%#v err=%v", entries, err)
	}
	var outcomes, results int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempt_outcomes WHERE attempt_id IN (?, ?)`, first.ID, second.ID).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_results WHERE turn_id = ?`, turn.ID).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if outcomes != 2 || results != 1 {
		t.Fatalf("outcomes=%d results=%d", outcomes, results)
	}
	if _, err := store.RetryAttempt(ctx, turn.ID, "new-retry-after-final"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("new retry after final err=%v", err)
	}
}

func TestPhase4LifecycleCommandIntentRecoversCommittedAttempts(t *testing.T) {
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

	worker, _, first, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "spawn", IdempotencyKey: "spawn-key"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(kind string, attempt Phase4Attempt) {
		t.Helper()
		if _, err := store.db.ExecContext(ctx, `DELETE FROM phase4_worker_commands WHERE worker_id = ? AND attempt_id = ?`, worker.ID, attempt.ID); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		var openErr error
		store, openErr = Open(ctx, path)
		if openErr != nil {
			t.Fatal(openErr)
		}
		command, err := store.EnsureLifecycleCommandIntent(ctx, kind, worker.ID, attempt.ID)
		if err != nil || command.ID != lifecycleCommandID(kind, attempt.ID) || command.Kind != kind || command.State != WorkerCommandPending {
			t.Fatalf("recovered %s command=%#v err=%v", kind, command, err)
		}
		claimed, duplicate, err := store.ClaimWorkerCommand(ctx, kind, "attempt", worker.ID, attempt.ID)
		if err != nil || duplicate || claimed.ID != command.ID {
			t.Fatalf("claimed %s command=%#v duplicate=%v err=%v", kind, claimed, duplicate, err)
		}
	}
	check("dispatch", first)
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, first.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}

	idleTurn, idleAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "follow up", IdempotencyKey: "idle-key"})
	if err != nil {
		t.Fatal(err)
	}
	_ = idleTurn
	check("dispatch", idleAttempt)
	if _, err := store.SetPhase4AttemptActive(ctx, idleAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.InterruptPhase4Attempt(ctx, idleAttempt.ID, "lost", "lost node"); err != nil {
		t.Fatal(err)
	}
	resumeTurn, resumeAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "resume", IdempotencyKey: "resume-key", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resumeTurn
	check("resume", resumeAttempt)
}

func TestPhase4WorkerCommandClaimIsDurableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "command"})
	if err != nil {
		t.Fatal(err)
	}
	first, duplicate, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", worker.ID, attempt.ID)
	if err != nil || duplicate || first.State != WorkerCommandPending {
		t.Fatalf("first=%#v duplicate=%v err=%v", first, duplicate, err)
	}
	if _, err := store.MarkWorkerCommandDelivered(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	other, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	second, duplicate, err := other.ClaimWorkerCommand(ctx, "dispatch", "attempt", worker.ID, attempt.ID)
	if err != nil || !duplicate || second.ID != first.ID || second.State != WorkerCommandDelivered {
		t.Fatalf("second=%#v duplicate=%v err=%v", second, duplicate, err)
	}
}

func TestPhase4WorkerCommandReclaimsOnlyExpiredLeaseWithSameID(t *testing.T) {
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
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "command"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, duplicate, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", worker.ID, attempt.ID)
	if err != nil || duplicate {
		t.Fatalf("claimed=%#v duplicate=%v err=%v", claimed, duplicate, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if command, reclaimed, err := first.ReclaimWorkerCommand(ctx, claimed.ID, claimed.LeaseUntil.Add(-time.Nanosecond)); err != nil || reclaimed || command.ID != claimed.ID {
		t.Fatalf("live lease command=%#v reclaimed=%v err=%v", command, reclaimed, err)
	}

	var group sync.WaitGroup
	reclaimed := make(chan coreReclaim, 2)
	for _, candidate := range []*Store{first, second} {
		group.Add(1)
		go func(candidate *Store) {
			defer group.Done()
			command, ok, err := candidate.ReclaimWorkerCommand(ctx, claimed.ID, claimed.LeaseUntil)
			reclaimed <- coreReclaim{command: command, ok: ok, err: err}
		}(candidate)
	}
	group.Wait()
	close(reclaimed)
	count := 0
	for result := range reclaimed {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.command.ID != claimed.ID {
			t.Fatalf("reclaim changed command ID: %#v", result.command)
		}
		if result.ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reclaims=%d, want 1", count)
	}
}

type coreReclaim struct {
	command WorkerCommand
	ok      bool
	err     error
}

func TestPhase4RetryKeyBelongsToItsSourceAttempt(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, first, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "two retries"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	firstFinish, err := store.FinishAttempt(ctx, first.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary-a"},
		RetryCommandID:      "cmd-A",
	})
	if err != nil || firstFinish.NextAttempt == nil {
		t.Fatalf("first retry finish=%#v err=%v", firstFinish, err)
	}
	second := *firstFinish.NextAttempt
	if _, err := store.SetPhase4AttemptActive(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	secondFinish, err := store.FinishAttempt(ctx, second.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary-b"},
		RetryCommandID:      "cmd-B",
	})
	if err != nil || secondFinish.NextAttempt == nil {
		t.Fatalf("second retry finish=%#v err=%v", secondFinish, err)
	}
	third := *secondFinish.NextAttempt

	duplicate, err := store.FinishAttempt(ctx, first.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary-a"},
		RetryCommandID:      "cmd-A",
	})
	if err != nil || !duplicate.Duplicate || duplicate.NextAttempt == nil || duplicate.NextAttempt.ID != second.ID {
		t.Fatalf("correct duplicate=%#v err=%v", duplicate, err)
	}
	if _, err := store.FinishAttempt(ctx, second.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary-a"},
		RetryCommandID:      "cmd-A",
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("key from another source err=%v", err)
	}

	var attempts, outcomes int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempts WHERE turn_id = ?`, turn.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempt_outcomes WHERE attempt_id IN (?, ?)`, first.ID, second.ID).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || outcomes != 2 || third.ID == second.ID {
		t.Fatalf("retry records: attempts=%d outcomes=%d third=%#v", attempts, outcomes, third)
	}
}

func TestPhase4RetryOperationsMigrationBackfillsSourceAttempt(t *testing.T) {
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
	_, turn, first, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "migrate retry"})
	if err != nil {
		t.Fatal(err)
	}
	legacyNextID := "att-legacy-next"
	const createdAt = "2024-01-01T00:00:00Z"
	if _, err := store.db.ExecContext(ctx, `DROP TABLE phase4_retry_operations`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE phase4_retry_operations (
		idempotency_key TEXT PRIMARY KEY,
		turn_id TEXT NOT NULL REFERENCES turns(id),
		attempt_id TEXT NOT NULL UNIQUE REFERENCES phase4_attempts(id),
		created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO phase4_attempt_outcomes(id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, "out-legacy", first.ID, OutcomeFailed, OutcomeRetryable, "temporary", "", "", createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE phase4_attempts SET state = ? WHERE id = ?`, AttemptFailed, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, legacyNextID, first.WorkerID, turn.ID, 2, first.NodeID, first.HarnessInstanceID, AttemptStarting, first.CorrelationID, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO phase4_retry_operations(idempotency_key, turn_id, attempt_id, created_at) VALUES(?, ?, ?, ?)`, "legacy-key", turn.ID, legacyNextID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE turns SET current_attempt_id = ? WHERE id = ?`, legacyNextID, turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var sourceID, nextID string
	if err := store.db.QueryRowContext(ctx, `SELECT source_attempt_id, next_attempt_id FROM phase4_retry_operations WHERE idempotency_key = ?`, "legacy-key").Scan(&sourceID, &nextID); err != nil {
		t.Fatal(err)
	}
	if sourceID != first.ID || nextID != legacyNextID {
		t.Fatalf("migrated operation source=%q next=%q", sourceID, nextID)
	}
	next, err := store.RetryAttemptWithKey(ctx, turn.ID, "legacy-key")
	if err != nil || next.ID != legacyNextID {
		t.Fatalf("migrated retry lookup=%#v err=%v", next, err)
	}
	duplicate, err := store.FinishAttempt(ctx, first.ID, FinishAttemptInput{
		AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary"},
		RetryCommandID:      "legacy-key",
	})
	if err != nil || !duplicate.Duplicate || duplicate.NextAttempt == nil || duplicate.NextAttempt.ID != legacyNextID {
		t.Fatalf("migrated duplicate=%#v err=%v", duplicate, err)
	}
}

func TestPhase4RetryKeyIsIdempotentAcrossLaterAttemptStates(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, first, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	firstFinish, err := store.FinishAttempt(ctx, first.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary"}, RetryCommandID: "retry-command-1"})
	if err != nil || firstFinish.NextAttempt == nil {
		t.Fatalf("first finish=%#v err=%v", firstFinish, err)
	}
	second := *firstFinish.NextAttempt
	repeated, err := store.RetryAttempt(ctx, turn.ID, "retry-command-1")
	if err != nil || repeated.ID != second.ID {
		t.Fatalf("repeated retry=%#v err=%v", repeated, err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	secondFinish, err := store.FinishAttempt(ctx, second.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary-again"}, RetryCommandID: "retry-command-2"})
	if err != nil || secondFinish.NextAttempt == nil {
		t.Fatalf("second finish=%#v err=%v", secondFinish, err)
	}
	repeated, err = store.RetryAttempt(ctx, turn.ID, "retry-command-1")
	if err != nil || repeated.ID != second.ID {
		t.Fatalf("late repeated retry=%#v err=%v", repeated, err)
	}
	third := *secondFinish.NextAttempt
	if third.ID == second.ID || third.Number != 3 {
		t.Fatalf("next retry=%#v", third)
	}
}

func TestPhase4FinalResultNotifiesObserverAfterCommit(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	var observed []ConversationEntry
	store.SetEntryObserver(func(entry ConversationEntry) {
		observed = append(observed, entry)
	})
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "notify"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	firstFinish, err := store.FinishAttempt(ctx, attempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeRetryable, ErrorCode: "temporary"}, RetryCommandID: "notify-retry"})
	if err != nil || firstFinish.NextAttempt == nil {
		t.Fatal(err)
	}
	if len(observed) != 0 {
		t.Fatalf("retryable outcome notified observer: %#v", observed)
	}
	second := *firstFinish.NextAttempt
	if _, err := store.SetPhase4AttemptActive(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, second.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 || observed[0].Kind != EntryWorkerResult || observed[0].Body != "done" {
		t.Fatalf("final observer entries=%#v", observed)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, second.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 {
		t.Fatalf("duplicate final notified observer: %#v", observed)
	}
}

func TestPhase4CloseWorkerRequiresTerminalTurnAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "close"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseWorker(ctx, worker.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("close active worker err=%v", err)
	}
	_, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeCanceled, Classification: OutcomeFinal, Summary: "stopped"})
	if err != nil || result == nil || result.FailureCode != "canceled" {
		t.Fatalf("canceled result=%#v err=%v", result, err)
	}
	closed, err := store.CloseWorker(ctx, worker.ID)
	if err != nil || closed.Status != WorkerClosed || !closed.Archived || closed.ClosedAt == nil {
		t.Fatalf("closed worker=%#v err=%v", closed, err)
	}
	repeated, err := store.CloseWorker(ctx, worker.ID)
	if err != nil || repeated.ID != closed.ID || repeated.ClosedAt == nil {
		t.Fatalf("repeated close=%#v err=%v", repeated, err)
	}
	if _, _, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "after close"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("turn after close err=%v", err)
	}
}

func TestPhase4RejectsSecondActiveTurnAndImmutableBinding(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "second"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second active Turn err=%v", err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	followUp, followAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "second"})
	if err != nil || followUp.ID == turn.ID || followAttempt.NodeID != worker.NodeID || followAttempt.HarnessInstanceID != worker.HarnessInstanceID {
		t.Fatalf("follow-up turn=%#v attempt=%#v err=%v", followUp, followAttempt, err)
	}
	stored, err := store.Worker(ctx, worker.ID)
	if err != nil || stored.ProjectID != "project-1" || stored.NodeID != "node-1" || stored.HarnessInstanceID != "node-1/fx" || stored.PolicySnapshot == "" {
		t.Fatalf("immutable Worker=%#v err=%v", stored, err)
	}
}

func TestPhase4RejectsInvalidOutcomeWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := []AttemptOutcomeInput{
		{Status: OutcomeSucceeded, Classification: OutcomeRetryable, Summary: "not allowed"},
		{Status: OutcomeCanceled, Classification: OutcomeRetryable, Summary: "not allowed"},
		{Status: OutcomeFailed, Classification: OutcomeFinal},
		{Status: OutcomeInterrupted, Classification: OutcomeRetryable, Summary: "not allowed"},
		{Status: AttemptOutcomeStatus("unknown"), Classification: OutcomeFinal, Summary: "not allowed"},
	}
	for _, input := range invalid {
		if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, input); err == nil {
			t.Fatalf("invalid outcome accepted: %#v", input)
		}
	}
	storedAttempt, err := store.Phase4Attempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedAttempt.State != AttemptStarting {
		t.Fatalf("invalid outcomes changed Attempt: %#v", storedAttempt)
	}
	if _, err := store.AttemptOutcome(ctx, attempt.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid outcomes stored outcome: %v", err)
	}
	if _, err := store.Phase4Result(ctx, storedAttempt.TurnID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid outcomes stored Result: %v", err)
	}
}

func TestPhase4RecoveryCreatesExplicitInterruptedResultAndBackup(t *testing.T) {
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
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "uncertain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecoverPhase4Attempts(ctx, Phase4AttemptRecoveryFunc(func(_ context.Context, _ Phase4Attempt) (Phase4RecoveryDecision, error) {
		return Phase4RecoveryUnknown, nil
	})); err != nil {
		t.Fatal(err)
	}
	outcome, result, duplicate, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeInterrupted, Classification: OutcomeFinal, FailureCode: "runtime_session_uncertain", Summary: "uncertain"})
	if err != nil || !duplicate || outcome.Status != OutcomeInterrupted || result == nil {
		t.Fatalf("repeated recovery outcome=%#v result=%#v duplicate=%v err=%v", outcome, result, duplicate, err)
	}
	backupInfo, err := os.Stat(path + ".backup")
	if err != nil {
		t.Fatalf("database backup missing: %v", err)
	}
	if backupInfo.Size() == 0 {
		t.Fatal("database backup is empty")
	}
	backupDB, err := sql.Open("sqlite", path+".backup")
	if err != nil {
		t.Fatal(err)
	}
	defer backupDB.Close()
	var people int
	if err := backupDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM persons`).Scan(&people); err != nil {
		t.Fatal(err)
	}
	if people != 1 {
		t.Fatalf("backup people=%d, want 1", people)
	}
}
