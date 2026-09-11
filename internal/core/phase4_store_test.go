package core

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
