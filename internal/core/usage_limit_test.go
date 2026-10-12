package core

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUsageLimit_AtomicScheduleCreationAndReplay(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	resetTime := time.Now().UTC().Add(10 * time.Minute)
	input := AttemptOutcomeInput{
		Status:          OutcomeFailed,
		Classification:  OutcomeRetryable,
		FailureCategory: FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "usage limit reached",
		ResetsAt:        &resetTime,
		QuotaScope:      "bucket_primary",
	}

	outcome, result, isDup, err := store.RecordAttemptOutcome(ctx, attempt.ID, input)
	if err != nil {
		t.Fatalf("record attempt outcome failed: %v", err)
	}
	if isDup {
		t.Fatal("expected fresh outcome, got duplicate")
	}
	if outcome.FailureCategory != FailureCategoryUsageLimit {
		t.Fatalf("expected usage limit category, got %s", outcome.FailureCategory)
	}
	// Crucial: no premature Result on suspended turn
	if result != nil {
		t.Fatalf("expected nil result on suspended turn, got %#v", result)
	}

	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}

	if details.Worker.Status != WorkerLimited {
		t.Fatalf("expected worker status %s, got %s", WorkerLimited, details.Worker.Status)
	}
	currentTurn := details.Turns[0]
	if currentTurn.State != TurnSuspended {
		t.Fatalf("expected turn state %s, got %s", TurnSuspended, currentTurn.State)
	}
	if len(details.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(details.Attempts))
	}
	nextAttempt := details.Attempts[1]
	if nextAttempt.State != AttemptScheduled {
		t.Fatalf("expected next attempt state %s, got %s", AttemptScheduled, nextAttempt.State)
	}
	if currentTurn.CurrentAttemptID != nextAttempt.ID {
		t.Fatalf("expected current attempt id to be next attempt %s, got %s", nextAttempt.ID, currentTurn.CurrentAttemptID)
	}

	if details.ActiveSchedule == nil {
		t.Fatal("expected active schedule to be populated")
	}
	sched := details.ActiveSchedule
	if sched.State != SchedulePending {
		t.Fatalf("expected schedule state %s, got %s", SchedulePending, sched.State)
	}
	expectedDue := resetTime.Add(60 * time.Second)
	if sched.ScheduledAt == nil || !sched.ScheduledAt.Equal(expectedDue) {
		t.Fatalf("expected scheduled_at %v, got %v", expectedDue, sched.ScheduledAt)
	}
	if sched.ContinuationPrompt != ContinuationPrompt {
		t.Fatalf("expected prompt %q, got %q", ContinuationPrompt, sched.ContinuationPrompt)
	}

	// Verify unclaimed lifecycle command intent
	cmd, found, err := store.FindWorkerCommand(ctx, "resume", "attempt", worker.ID, nextAttempt.ID)
	if err != nil || !found {
		t.Fatalf("expected pending resume command intent, err=%v, found=%v", err, found)
	}
	if cmd.State != WorkerCommandPending {
		t.Fatalf("expected command pending, got %s", cmd.State)
	}

	// Verify replay is idempotent
	outcome2, result2, isDup2, err2 := store.RecordAttemptOutcome(ctx, attempt.ID, input)
	if err2 != nil || !isDup2 || result2 != nil || outcome2.ID != outcome.ID {
		t.Fatalf("expected idempotent duplicate outcome, err=%v, dup=%v, result=%v", err2, isDup2, result2)
	}
}

func TestUsageLimit_ManualHoldOnUnknownResetOrPolicyOff(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	// 1. Without ResetsAt -> manual_hold
	input := AttemptOutcomeInput{
		Status:          OutcomeFailed,
		Classification:  OutcomeRetryable,
		FailureCategory: FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "usage limit reached without reset",
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, input); err != nil {
		t.Fatal(err)
	}

	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if details.ActiveSchedule == nil || details.ActiveSchedule.State != ScheduleManualHold {
		t.Fatalf("expected schedule state manual_hold, got %#v", details.ActiveSchedule)
	}
	if details.ActiveSchedule.ScheduledAt != nil {
		t.Fatalf("expected nil scheduled_at on manual_hold, got %v", details.ActiveSchedule.ScheduledAt)
	}
}

func TestUsageLimit_RecoveryCrashBoundaries(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	resetTime := time.Now().UTC().Add(5 * time.Minute)
	input := AttemptOutcomeInput{
		Status:          OutcomeFailed,
		Classification:  OutcomeRetryable,
		FailureCategory: FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "limit",
		ResetsAt:        &resetTime,
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, input); err != nil {
		t.Fatal(err)
	}

	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	sched := details.ActiveSchedule
	if sched == nil {
		t.Fatal("expected schedule")
	}

	// Claim schedule simulating a crash right after claim before command was delivered
	claimed, err := store.ClaimAttemptSchedule(ctx, sched.ID, time.Now().UTC())
	if err != nil || !claimed {
		t.Fatalf("claim failed: %v", err)
	}

	// RecoverPhase4Attempts should restore claimed schedule to pending
	if err := store.RecoverPhase4Attempts(ctx, Phase4AttemptRecoveryFunc(func(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error) { return Phase4RecoveryAlive, nil })); err != nil {
		t.Fatal(err)
	}

	schedUpdated, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil || !found {
		t.Fatalf("expected active schedule, err=%v, found=%v", err, found)
	}
	if schedUpdated.State != SchedulePending {
		t.Fatalf("expected recovered schedule state pending, got %s", schedUpdated.State)
	}
}

func TestLimitedWorker_TwoStoreCASRace(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test4.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	resetTime := time.Now().UTC().Add(5 * time.Minute)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status:          OutcomeFailed,
		Classification:  OutcomeRetryable,
		FailureCategory: FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "limit",
		ResetsAt:        &resetTime,
	}); err != nil {
		t.Fatal(err)
	}

	sched, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil || !found {
		t.Fatal("expected schedule")
	}

	var wg sync.WaitGroup
	var claimCount int
	var mu sync.Mutex

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := store.ClaimAttemptSchedule(ctx, sched.ID, time.Now().UTC())
			if err == nil && claimed {
				mu.Lock()
				claimCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if claimCount != 1 {
		t.Fatalf("expected exactly 1 winner in CAS race, got %d", claimCount)
	}
}

func TestUsageLimit_CancelAndClose(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "test5.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	resetTime := time.Now().UTC().Add(5 * time.Minute)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status:          OutcomeFailed,
		Classification:  OutcomeRetryable,
		FailureCategory: FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "limit",
		ResetsAt:        &resetTime,
	}); err != nil {
		t.Fatal(err)
	}

	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	nextAttempt := details.Attempts[1]

	// Cancel attempt schedule
	if err := store.CancelAttemptSchedule(ctx, details.ActiveSchedule.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	// Finalize next attempt with canceled Result
	outcome, result, isDup, err := store.RecordAttemptOutcome(ctx, nextAttempt.ID, AttemptOutcomeInput{
		Status:         OutcomeCanceled,
		Classification: OutcomeFinal,
		ErrorCode:      "canceled",
		FailureCode:    "canceled",
		Summary:        "Canceled before scheduled continuation",
	})
	if err != nil {
		t.Fatalf("cancel outcome failed: %v", err)
	}
	if isDup {
		t.Fatal("expected fresh outcome")
	}
	if outcome.Status != OutcomeCanceled {
		t.Fatalf("expected canceled outcome, got %s", outcome.Status)
	}
	if result == nil || result.Status != ResultCanceled {
		t.Fatalf("expected single canceled Result on suspended turn, got %#v", result)
	}

	updated, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Worker.Status != WorkerIdle {
		t.Fatalf("expected worker idle after cancel, got %s", updated.Worker.Status)
	}
	if updated.Turns[0].State != TurnCanceled {
		t.Fatalf("expected turn canceled, got %s", updated.Turns[0].State)
	}
}

func TestUsageLimit_RepeatTerminalUsageLimitOutcome_Idempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "repeat_terminal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	envelope := AttemptOutcomeEnvelope{
		EventID:           "env-1",
		Node:              "local",
		HarnessInstanceID: "local/codex",
		WorkerRef:         worker.WorkerRef,
		TurnID:            attempt.TurnID,
		AttemptID:         attempt.ID,
		Status:            OutcomeFailed,
		Classification:    OutcomeRetryable,
		FailureCategory:   FailureCategoryUsageLimit,
		FailureCode:       "usage_limit_exceeded",
		Summary:           "Usage limit reached - manual hold",
		OccurredAt:        now,
	}

	// First call records usage limit outcome (manual hold because no resetsAt)
	outcome1, result1, isDup1, err1 := store.RecordNodeAttemptOutcome(ctx, envelope)
	if err1 != nil {
		t.Fatalf("first FinishAttempt failed: %v", err1)
	}
	if isDup1 {
		t.Fatal("first call should not be duplicate")
	}
	if outcome1.Status != OutcomeFailed {
		t.Fatalf("expected failed outcome, got %s", outcome1.Status)
	}
	if result1 != nil {
		t.Fatalf("expected nil result on retryable/suspended attempt, got %#v", result1)
	}

	// Repeat call with identical envelope must succeed as duplicate (not ErrInvalidTransition)
	outcome2, result2, isDup2, err2 := store.RecordNodeAttemptOutcome(ctx, envelope)
	if err2 != nil {
		t.Fatalf("repeat FinishAttempt failed: %v", err2)
	}
	if !isDup2 {
		t.Fatal("repeat call should return isDuplicate = true")
	}
	if outcome2.ID != outcome1.ID {
		t.Fatalf("expected same outcome ID %s, got %s", outcome1.ID, outcome2.ID)
	}
	if result2 != nil {
		t.Fatalf("expected nil result on duplicate, got %#v", result2)
	}

	// Now cancel the limited worker attempt to give it a terminal Result
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	nextAttempt := details.Attempts[1]
	cancelOutcome, cancelResult, cancelErr := store.CancelLimitedWorkerAttempt(ctx, worker.ID, nextAttempt.ID, "Canceled")
	if cancelErr != nil {
		t.Fatalf("CancelLimitedWorkerAttempt failed: %v", cancelErr)
	}
	if cancelResult == nil || cancelResult.Status != ResultCanceled {
		t.Fatalf("expected canceled result, got %#v", cancelResult)
	}
	_ = cancelOutcome

	// Calling FinishAttempt again for nextAttempt with usage limit must return duplicate with existing Result (not ErrInvalidTransition)
	repeatNextEnv := AttemptOutcomeEnvelope{
		EventID:           "env-2",
		Node:              "local",
		HarnessInstanceID: "local/codex",
		WorkerRef:         worker.WorkerRef,
		TurnID:            nextAttempt.TurnID,
		AttemptID:         nextAttempt.ID,
		Status:            OutcomeFailed,
		Classification:    OutcomeRetryable,
		FailureCategory:   FailureCategoryUsageLimit,
		FailureCode:       "usage_limit_exceeded",
		Summary:           "Usage limit after cancellation",
		OccurredAt:        now,
	}
	outcome3, result3, isDup3, err3 := store.RecordNodeAttemptOutcome(ctx, repeatNextEnv)
	if err3 != nil {
		t.Fatalf("repeat FinishAttempt on terminal attempt failed: %v", err3)
	}
	if !isDup3 {
		t.Fatal("expected isDuplicate = true on terminal attempt")
	}
	if result3 == nil || result3.ID != cancelResult.ID {
		t.Fatalf("expected existing terminal result %v, got %v", cancelResult, result3)
	}
	_ = outcome3
}

func TestUsageLimit_CancelLimitedWorkerAttempt_AtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "cancel_atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
	}, TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	resetTime := time.Now().UTC().Add(15 * time.Minute)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status:          OutcomeFailed,
		Classification:  OutcomeRetryable,
		FailureCategory: FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "usage limit reached",
		ResetsAt:        &resetTime,
	}); err != nil {
		t.Fatal(err)
	}

	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	nextAttempt := details.Attempts[1]

	// Perform atomic CancelLimitedWorkerAttempt
	outcome, result, err := store.CancelLimitedWorkerAttempt(ctx, worker.ID, nextAttempt.ID, "Canceled before auto-resume")
	if err != nil {
		t.Fatalf("CancelLimitedWorkerAttempt failed: %v", err)
	}
	if outcome.Status != OutcomeCanceled || outcome.Classification != OutcomeFinal {
		t.Fatalf("expected canceled final outcome, got %#v", outcome)
	}
	if result == nil || result.Status != ResultCanceled || result.Summary != "Canceled before auto-resume" {
		t.Fatalf("expected canceled result with summary, got %#v", result)
	}

	// Verify all states atomically transitioned
	detailsAfter, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if detailsAfter.Worker.Status != WorkerIdle {
		t.Fatalf("expected worker idle, got %s", detailsAfter.Worker.Status)
	}
	if detailsAfter.Turns[0].State != TurnCanceled {
		t.Fatalf("expected turn canceled, got %s", detailsAfter.Turns[0].State)
	}
	if detailsAfter.Turns[0].ResultID != result.ID {
		t.Fatalf("expected turn result_id %s, got %s", result.ID, detailsAfter.Turns[0].ResultID)
	}
	if detailsAfter.ActiveSchedule != nil {
		t.Fatalf("expected no active schedule after cancellation, got %#v", detailsAfter.ActiveSchedule)
	}

	// Check phase4_worker_commands for resume command failed
	cmd, found, cmdErr := store.FindWorkerCommand(ctx, "resume", "attempt", worker.ID, nextAttempt.ID)
	if cmdErr != nil || !found {
		t.Fatalf("expected command, err=%v, found=%v", cmdErr, found)
	}
	if cmd.State != WorkerCommandFailed || cmd.LastError != "canceled_before_handoff" {
		t.Fatalf("expected failed command with canceled_before_handoff, got state=%s, lastError=%s", cmd.State, cmd.LastError)
	}

	// Second call must be idempotent and return same outcome and result without error
	outcome2, result2, err2 := store.CancelLimitedWorkerAttempt(ctx, worker.ID, nextAttempt.ID, "Canceled again")
	if err2 != nil {
		t.Fatalf("second call failed: %v", err2)
	}
	if outcome2.ID != outcome.ID {
		t.Fatalf("expected same outcome ID %s, got %s", outcome.ID, outcome2.ID)
	}
	if result2 == nil || result2.ID != result.ID {
		t.Fatalf("expected same result ID %s, got %#v", result.ID, result2)
	}
}
