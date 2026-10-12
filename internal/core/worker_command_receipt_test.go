package core

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLateWorkerCommandReceiptReconcilesExactResultAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, origin, worker, turn, attempt, command := createReceiptFixture(t, ctx, path, "accepted-worker", "macbook")
	if _, err := store.MarkWorkerCommandUncertain(ctx, command.ID, "response acknowledgement timed out"); err != nil {
		t.Fatal(err)
	}

	_, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "finished before receipt",
	})
	if err != nil || result == nil {
		t.Fatalf("record Result before receipt: result=%#v err=%v", result, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID); err != nil || !related {
		t.Fatalf("uncertain exact Worker Turn Result was not joined: related=%v err=%v", related, err)
	}

	wrongNode := workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptAccepted)
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, "other-node", wrongNode); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("receipt from wrong Node was accepted: %v", err)
	}
	wrongTurn := workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptAccepted)
	wrongTurn.TurnID = "unrelated-turn"
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, "macbook", wrongTurn); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("receipt for wrong Worker Turn was accepted: %v", err)
	}
	wrongAttempt := workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptAccepted)
	wrongAttempt.AttemptID = "unrelated-attempt"
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, "macbook", wrongAttempt); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("receipt for wrong Attempt was accepted: %v", err)
	}
	current, found, err := store.FindWorkerCommand(ctx, "respond", "request:approval-request", worker.ID, attempt.ID)
	if err != nil || !found || current.State != WorkerCommandUncertain {
		t.Fatalf("invalid receipt changed command: command=%#v err=%v", current, err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	receipt := workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptAccepted)
	current, err = store.ReconcileWorkerCommandReceipt(ctx, "macbook", receipt)
	if err != nil || current.State != WorkerCommandDelivered {
		t.Fatalf("late accepted receipt did not durably deliver exact command: command=%#v err=%v", current, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	current, err = store.ReconcileWorkerCommandReceipt(ctx, "macbook", receipt)
	if err != nil || current.State != WorkerCommandDelivered {
		t.Fatalf("duplicate receipt after restart was not idempotent: command=%#v err=%v", current, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID); err != nil || !related {
		t.Fatalf("late receipt lost the exact Result relation: related=%v err=%v", related, err)
	}
	var linkedTurnID, linkedResultID string
	if err := store.db.QueryRowContext(ctx, `SELECT worker_turn_id, result_id FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?`, origin.ID, origin.InputID).Scan(&linkedTurnID, &linkedResultID); err != nil || linkedTurnID != turn.ID || linkedResultID != result.ID {
		t.Fatalf("receipt linked wrong Result: turn=%q result=%q want turn=%q result=%q err=%v", linkedTurnID, linkedResultID, turn.ID, result.ID, err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?`, origin.ID, origin.InputID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate receipts changed exact relation count: count=%d err=%v", count, err)
	}

	denied := receipt
	denied.State = WorkerCommandReceiptDenied
	denied.ErrorCode = "response_rejected"
	denied.ErrorMessage = "Node rejected the response"
	if current, err = store.ReconcileWorkerCommandReceipt(ctx, "macbook", denied); err != nil || current.State != WorkerCommandDelivered {
		t.Fatalf("stale denial superseded accepted receipt: command=%#v err=%v", current, err)
	}
}

func TestLateAcceptedRespondReceiptResumesOnlyItsNeedsInputTurn(t *testing.T) {
	ctx := context.Background()
	store, _, _, turn, attempt, command := createReceiptFixture(t, ctx, filepath.Join(t.TempDir(), "secretary.db"), "resume-worker", "macbook")
	defer store.Close()
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandUncertain(ctx, command.ID, "response acknowledgement timed out"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, "macbook", workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptAccepted)); err != nil {
		t.Fatal(err)
	}
	var turnState TurnState
	var workerState WorkerStatus
	if err := store.db.QueryRowContext(ctx, `SELECT t.state, w.status FROM turns t JOIN workers w ON w.id = t.worker_id WHERE t.id = ?`, turn.ID).Scan(&turnState, &workerState); err != nil {
		t.Fatal(err)
	}
	if turnState != TurnActive || workerState != WorkerWorking {
		t.Fatalf("accepted response did not resume exact Worker Turn: turn=%q worker=%q", turnState, workerState)
	}
}

func TestAuthoritativeWorkerCommandDenialIsNotSupersededByLateAcceptance(t *testing.T) {
	ctx := context.Background()
	store, origin, _, turn, attempt, command := createReceiptFixture(t, ctx, filepath.Join(t.TempDir(), "secretary.db"), "denied-worker", "macbook")
	defer store.Close()
	if _, err := store.MarkWorkerCommandUncertain(ctx, command.ID, "response acknowledgement timed out"); err != nil {
		t.Fatal(err)
	}
	denied := workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptDenied)
	denied.ErrorCode = "runtime_does_not_accept_response"
	denied.ErrorMessage = "runtime rejected the response before delivery"
	current, err := store.ReconcileWorkerCommandReceipt(ctx, "macbook", denied)
	if err != nil || current.State != WorkerCommandFailed {
		t.Fatalf("authoritative denial was not preserved: command=%#v err=%v", current, err)
	}
	current, err = store.MarkWorkerCommandUncertain(ctx, command.ID, "late caller timeout")
	if err != nil || current.State != WorkerCommandFailed {
		t.Fatalf("caller timeout downgraded an authoritative denial: command=%#v err=%v", current, err)
	}
	accepted := workerCommandReceipt(command, turn, attempt, WorkerCommandReceiptAccepted)
	current, err = store.ReconcileWorkerCommandReceipt(ctx, "macbook", accepted)
	if err != nil || current.State != WorkerCommandFailed {
		t.Fatalf("late acceptance superseded authoritative denial: command=%#v err=%v", current, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID); err != nil || related {
		t.Fatalf("denied response linked a Worker Result: related=%v err=%v", related, err)
	}

	// An explicit user retry reopens this exact command; a receipt for that
	// retried handoff may then supersede the previous denial.
	if retried, retry, err := store.RetryWorkerCommand(ctx, command.ID, time.Now().UTC()); err != nil || !retry || retried.State != WorkerCommandPending {
		t.Fatalf("explicit response retry: command=%#v retry=%v err=%v", retried, retry, err)
	}
	if current, err = store.MarkWorkerCommandUncertain(ctx, command.ID, "retry acknowledgement timed out"); err != nil || current.State != WorkerCommandUncertain {
		t.Fatalf("retry timeout was not kept uncertain: command=%#v err=%v", current, err)
	}
	current, err = store.ReconcileWorkerCommandReceipt(ctx, "macbook", accepted)
	if err != nil || current.State != WorkerCommandDelivered {
		t.Fatalf("accepted receipt for explicit retry did not supersede denial: command=%#v err=%v", current, err)
	}
}

func createReceiptFixture(t *testing.T, ctx context.Context, path, workerRef string, node NodeReference) (*Store, SecretaryTurn, Worker, Turn, Phase4Attempt, WorkerCommand) {
	t.Helper()
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(filepath.Dir(path), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "answer this Worker result")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, origin.ID); err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: workerRef, Intent: "complete the response", ProjectID: "project", NodeID: string(node), HarnessInstanceID: string(node) + "/fx",
	}, TurnSpec{Input: "complete the response"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	command, _, err := store.ClaimWorkerCommand(ctx, "respond", "request:approval-request", worker.ID, attempt.ID, SecretaryOriginIdentity{
		PersonID: person.ID, Capability: capability, SecretaryTurnID: origin.ID, InputID: origin.InputID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, origin, worker, turn, attempt, command
}

func workerCommandReceipt(command WorkerCommand, turn Turn, attempt Phase4Attempt, state WorkerCommandReceiptState) WorkerCommandReceipt {
	return WorkerCommandReceipt{
		CommandID: command.ID, Kind: "respond", TurnID: turn.ID, AttemptID: attempt.ID,
		State: state, ErrorCode: "", ErrorMessage: "",
	}
}

func TestMarkWorkerCommandUncertainSanitizesStoredAndLegacyError(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, _, _, _, _, command := createReceiptFixture(t, ctx, path, "uncertain-sanitize", "macbook")
	message := "runtime context deadline exceeded; Authorization: Bearer uncertain-private-token Digest username=owner, response=uncertain-digest-secret " + strings.Repeat("long-context ", 120)
	updated, err := store.MarkWorkerCommandUncertain(ctx, command.ID, message)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != WorkerCommandUncertain || len([]rune(updated.LastError)) > 1000 || strings.Contains(updated.LastError, "uncertain-private-token") || strings.Contains(updated.LastError, "uncertain-digest-secret") {
		t.Fatalf("uncertain error was not safely bounded: state=%q runes=%d error=%q", updated.State, len([]rune(updated.LastError)), updated.LastError)
	}

	// Legacy rows bypassed the new write boundary; the common command scanner
	// must still protect every read and replay path.
	legacy := "legacy Bearer legacy-private-token Digest username=owner, response=legacy-digest-secret " + strings.Repeat("oversized ", 140)
	if _, err := store.db.ExecContext(ctx, `UPDATE phase4_worker_commands SET last_error = ? WHERE id = ?`, legacy, command.ID); err != nil {
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
	read, found, err := reopened.FindWorkerCommand(ctx, command.Kind, command.DedupeKey, command.WorkerID, command.AttemptID)
	if err != nil || !found {
		t.Fatalf("read uncertain command: command=%#v found=%v err=%v", read, found, err)
	}
	if read.State != WorkerCommandUncertain || len([]rune(read.LastError)) > 1000 || strings.Contains(read.LastError, "legacy-private-token") || strings.Contains(read.LastError, "legacy-digest-secret") {
		t.Fatalf("legacy uncertain error leaked after reopen: state=%q runes=%d error=%q", read.State, len([]rune(read.LastError)), read.LastError)
	}
}

func TestReconcileWorkerCommandReceipt_SteeringDeniedPreservesFailureAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, _, worker, turn, attempt, _ := createReceiptFixture(t, ctx, path, "steer-worker", "macbook")
	defer store.Close()

	steeringCommand, _, err := store.ClaimWorkerCommand(ctx, "steering", "steering:update-1", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}

	receipt := WorkerCommandReceipt{
		CommandID:       steeringCommand.ID,
		Kind:            "steering",
		TurnID:          turn.ID,
		AttemptID:       attempt.ID,
		State:           WorkerCommandReceiptDenied,
		ErrorCode:       "usage_limit_exceeded",
		ErrorMessage:    "Quota exceeded",
		FailureCategory: FailureCategoryUsageLimit,
		ProviderCode:    "usage_limit_exceeded",
		Technical:       "usage limit hit: retry after 1h",
	}

	updated, err := store.ReconcileWorkerCommandReceipt(ctx, "macbook", receipt)
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != WorkerCommandFailed {
		t.Fatalf("updated state = %q, want %q", updated.State, WorkerCommandFailed)
	}

	outcome, err := store.WorkerCommandReceiptOutcome(ctx, steeringCommand.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.FailureCategory != FailureCategoryUsageLimit || outcome.ProviderCode != "usage_limit_exceeded" || outcome.Technical != "usage limit hit: retry after 1h" {
		t.Fatalf("immediate outcome = %#v", outcome)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	reopenedOutcome, err := reopened.WorkerCommandReceiptOutcome(ctx, steeringCommand.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedOutcome.FailureCategory != FailureCategoryUsageLimit || reopenedOutcome.ProviderCode != "usage_limit_exceeded" || reopenedOutcome.Technical != "usage limit hit: retry after 1h" {
		t.Fatalf("reopened outcome = %#v", reopenedOutcome)
	}

	// Verify that steering rejection did NOT finalize the active attempt:
	attemptRecord, err := reopened.Phase4Attempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if attemptRecord.State != AttemptActive {
		t.Fatalf("attempt state = %q, want %q (steering rejection must not finalize active attempt)", attemptRecord.State, AttemptActive)
	}

	// Verify that no Result exists yet before native terminal outcome:
	_, err = reopened.Phase4Result(ctx, turn.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for Result before native terminal, got: %v", err)
	}

	// Native terminal forms the single Result:
	_, terminalResult, _, err := reopened.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status:         OutcomeSucceeded,
		Classification: OutcomeFinal,
		Summary:        "completed successfully after steering rejection",
	})
	if err != nil || terminalResult == nil {
		t.Fatalf("native terminal failed after rejected steering: result=%#v err=%v", terminalResult, err)
	}
	if terminalResult.Status != ResultSucceeded {
		t.Fatalf("result status = %q, want %q", terminalResult.Status, ResultSucceeded)
	}
	if terminalResult.Summary != "completed successfully after steering rejection" {
		t.Fatalf("result summary = %q", terminalResult.Summary)
	}

	savedResult, err := reopened.Phase4Result(ctx, turn.ID)
	if err != nil {
		t.Fatalf("Phase4Result returned err=%v, want savedResult", err)
	}
	if savedResult.Status != ResultSucceeded {
		t.Fatalf("savedResult status = %q, want %q", savedResult.Status, ResultSucceeded)
	}
}

func TestReconcileWorkerCommandReceipt_DeniedSteeringDoesNotBlockDeliveredQueueFIFO(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, _, worker, _, attempt1, _ := createReceiptFixture(t, ctx, path, "fifo-worker", "macbook")
	defer store.Close()

	// 1. Enqueue two follow-up messages
	msg1, err := store.EnqueueWorkerMessage(ctx, worker.ID, "first follow-up", "key-q1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.EnqueueWorkerMessage(ctx, worker.ID, "second follow-up", "key-q2")
	if err != nil {
		t.Fatal(err)
	}

	// 2. Finish initial attempt, worker becomes idle
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt1.ID, AttemptOutcomeInput{
		Status:         OutcomeSucceeded,
		Classification: OutcomeFinal,
		Summary:        "initial attempt done",
	}); err != nil {
		t.Fatal(err)
	}

	// 3. Promote msg1 -> Turn 2, Attempt 2
	turn2, attempt2, err := store.PromoteQueuedWorkerMessage(ctx, msg1, "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt2.ID); err != nil {
		t.Fatal(err)
	}
	// Mark msg1 as delivered (delivery to runtime succeeded)
	if err := store.CompleteQueuedWorkerMessage(ctx, msg1.ID, "delivered", ""); err != nil {
		t.Fatal(err)
	}

	// Verify msg1 is delivered, msg2 is pending
	queued, err := store.QueuedWorkerMessages(ctx, worker.ID)
	if err != nil || len(queued) != 2 {
		t.Fatalf("queued messages=%#v err=%v", queued, err)
	}
	if queued[0].State != "delivered" || queued[1].State != "pending" {
		t.Fatalf("queued states: msg1=%q msg2=%q", queued[0].State, queued[1].State)
	}

	// 4. Client issues steering on active Attempt 2, and steering is DENIED by Node
	steeringCommand, _, err := store.ClaimWorkerCommand(ctx, "steering", "steering:update-1", worker.ID, attempt2.ID)
	if err != nil {
		t.Fatal(err)
	}
	deniedReceipt := WorkerCommandReceipt{
		CommandID:       steeringCommand.ID,
		Kind:            "steering",
		TurnID:          turn2.ID,
		AttemptID:       attempt2.ID,
		State:           WorkerCommandReceiptDenied,
		ErrorCode:       "usage_limit_exceeded",
		ErrorMessage:    "Quota exceeded",
		FailureCategory: FailureCategoryUsageLimit,
		ProviderCode:    "usage_limit_exceeded",
		Technical:       "usage limit hit",
	}
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, NodeReference("macbook"), deniedReceipt); err != nil {
		t.Fatal(err)
	}

	// 5. CRITICAL CHECK: Denied steering MUST NOT alter delivered or pending queue messages!
	queuedAfter, err := store.QueuedWorkerMessages(ctx, worker.ID)
	if err != nil || len(queuedAfter) != 2 {
		t.Fatalf("queuedAfter err=%v len=%d", err, len(queuedAfter))
	}
	if queuedAfter[0].State != "delivered" {
		t.Fatalf("msg1 state after steering denial = %q, want delivered (steering denial must not block delivered follow-up!)", queuedAfter[0].State)
	}
	if queuedAfter[1].State != "pending" {
		t.Fatalf("msg2 state after steering denial = %q, want pending", queuedAfter[1].State)
	}

	// 6. Native terminal successfully completes Attempt 2
	_, terminalResult, _, err := store.RecordAttemptOutcome(ctx, attempt2.ID, AttemptOutcomeInput{
		Status:         OutcomeSucceeded,
		Classification: OutcomeFinal,
		Summary:        "attempt 2 completed successfully",
	})
	if err != nil || terminalResult == nil || terminalResult.Status != ResultSucceeded {
		t.Fatalf("attempt 2 native terminal failed: result=%#v err=%v", terminalResult, err)
	}

	// 7. Verify reopen preserves states
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	reopenedQueued, err := reopened.QueuedWorkerMessages(ctx, worker.ID)
	if err != nil || len(reopenedQueued) != 2 {
		t.Fatalf("reopenedQueued err=%v len=%d", err, len(reopenedQueued))
	}
	if reopenedQueued[0].State != "delivered" || reopenedQueued[1].State != "pending" {
		t.Fatalf("reopened states: msg1=%q msg2=%q", reopenedQueued[0].State, reopenedQueued[1].State)
	}

	// 8. Queue FIFO continues: msg2 can now be promoted without being blocked!
	turn3, attempt3, err := reopened.PromoteQueuedWorkerMessage(ctx, reopenedQueued[1], "dispatch")
	if err != nil {
		t.Fatalf("queue FIFO blocked: failed to promote msg2 after native terminal: %v", err)
	}
	if turn3.ID == "" || attempt3.ID == "" {
		t.Fatalf("expected valid turn3 and attempt3 from queue FIFO promotion")
	}
}
