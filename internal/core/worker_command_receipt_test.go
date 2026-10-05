package core

import (
	"context"
	"errors"
	"path/filepath"
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
