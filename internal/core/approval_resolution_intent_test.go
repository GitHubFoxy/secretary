package core

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovalResolutionIntentAndAcceptedReceiptPreserveFirstApproveOrDeny(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		state ApprovalState
		other ApprovalState
	}{
		{name: "approve", state: ApprovalApproved, other: ApprovalDenied},
		{name: "deny", state: ApprovalDenied, other: ApprovalApproved},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := createApprovalIntentFixture(t, ctx, "resolution-"+testCase.name, nil)
			const savedResponse = "saved original response; do not expose or log"
			prepared, duplicate, err := fixture.store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, testCase.state, "owner-a", savedResponse)
			if err != nil || duplicate || prepared.State != ApprovalResolving {
				t.Fatalf("intent state=%q duplicate=%v err=%v", prepared.State, duplicate, err)
			}
			if prepared.ResolutionCommandID != fixture.command.ID || prepared.ResolutionState != testCase.state || prepared.ResolutionResponse != savedResponse || prepared.ResolutionResolvedBy != "owner-a" {
				t.Fatalf("durable intent did not preserve exact command decision")
			}
			if _, _, err := fixture.store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, testCase.other, "owner-b", "changed response"); !errors.Is(err, ErrApprovalResolutionConflict) {
				t.Fatalf("conflicting duplicate changed the first intent: %v", err)
			}

			wrongNode := approvalReceipt(fixture, WorkerCommandReceiptAccepted)
			if _, err := fixture.store.ReconcileWorkerCommandReceipt(ctx, "other-node", wrongNode); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("wrong Node receipt was accepted: %v", err)
			}
			wrongAttempt := approvalReceipt(fixture, WorkerCommandReceiptAccepted)
			wrongAttempt.AttemptID = "different-attempt"
			if _, err := fixture.store.ReconcileWorkerCommandReceipt(ctx, NodeReference(fixture.worker.NodeID), wrongAttempt); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("wrong Attempt receipt was accepted: %v", err)
			}

			if _, err := fixture.store.ReconcileWorkerCommandReceipt(ctx, NodeReference(fixture.worker.NodeID), approvalReceipt(fixture, WorkerCommandReceiptAccepted)); err != nil {
				t.Fatal(err)
			}
			resolved, err := fixture.store.Approval(ctx, fixture.approval.RequestID)
			if err != nil || resolved.State != testCase.state || resolved.Response != savedResponse || resolved.ResolvedBy != "owner-a" {
				t.Fatalf("accepted receipt did not commit saved decision: state=%q actor=%q err=%v", resolved.State, resolved.ResolvedBy, err)
			}
			changed, duplicate, err := fixture.store.ResolveApproval(ctx, fixture.approval.RequestID, testCase.other, "owner-b", "changed response")
			if err != nil || !duplicate || changed.State != testCase.state || changed.Response != savedResponse {
				t.Fatalf("later direct resolution replaced the first decision: state=%q duplicate=%v err=%v", changed.State, duplicate, err)
			}

			events, err := fixture.store.EventsRecent(ctx, 50)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(events)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), savedResponse) || strings.Contains(string(encoded), "changed response") {
				t.Fatal("approval response payload was copied into event logs")
			}
		})
	}
}

func TestApprovalExpiryAndLateReceiptPreserveCanonicalTerminalResult(t *testing.T) {
	ctx := context.Background()
	expires := time.Now().UTC().Add(time.Minute)
	fixture := createApprovalIntentFixture(t, ctx, "approval-expiry-late-ack", &expires)
	const savedResponse = "approve before the delayed ACK"
	if _, _, err := fixture.store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, ApprovalApproved, "owner", savedResponse); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.MarkWorkerCommandUncertain(ctx, fixture.command.ID, "ACK delayed"); err != nil {
		t.Fatal(err)
	}
	expired, duplicate, err := fixture.store.ExpireApproval(ctx, fixture.approval.RequestID, expires.Add(time.Hour))
	if err != nil || !duplicate || expired.State != ApprovalResolving {
		t.Fatalf("expiry overrode a durable resolution intent: state=%q duplicate=%v err=%v", expired.State, duplicate, err)
	}

	_, canonical, _, err := fixture.store.RecordAttemptOutcome(ctx, fixture.attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "canonical terminal result",
	})
	if err != nil || canonical == nil {
		t.Fatalf("record canonical Result before late ACK: result=%#v err=%v", canonical, err)
	}
	independentTurn, _, err := fixture.store.CreateTurn(ctx, fixture.worker.ID, TurnSpec{Input: "independent next turn"})
	if err != nil {
		t.Fatalf("create independent Worker Turn: %v", err)
	}
	if _, err := fixture.store.ReconcileWorkerCommandReceipt(ctx, NodeReference(fixture.worker.NodeID), approvalReceipt(fixture, WorkerCommandReceiptAccepted)); err != nil {
		t.Fatal(err)
	}
	resolved, err := fixture.store.Approval(ctx, fixture.approval.RequestID)
	if err != nil || resolved.State != ApprovalApproved || resolved.Response != savedResponse {
		t.Fatalf("late accepted receipt did not apply saved approve intent: state=%q err=%v", resolved.State, err)
	}
	turn, err := fixture.store.Turn(ctx, fixture.turn.ID)
	if err != nil || turn.State != TurnSucceeded || turn.ResultID != canonical.ID {
		t.Fatalf("late approval receipt revived/replaced terminal Turn: state=%q result=%q err=%v", turn.State, turn.ResultID, err)
	}
	worker, err := fixture.store.Worker(ctx, fixture.worker.ID)
	if err != nil || worker.Status != WorkerQueued || worker.CurrentTurnID != independentTurn.ID {
		t.Fatalf("late approval receipt changed the independent Turn: status=%q current_turn=%q err=%v", worker.Status, worker.CurrentTurnID, err)
	}
	independent, err := fixture.store.Turn(ctx, independentTurn.ID)
	if err != nil || independent.State != TurnStarting {
		t.Fatalf("late approval receipt changed independent Turn state: state=%q err=%v", independent.State, err)
	}
	_, duplicate, err = fixture.store.ExpireApproval(ctx, fixture.approval.RequestID, expires.Add(2*time.Hour))
	if err != nil || !duplicate {
		t.Fatalf("expiry replay changed resolved approval: duplicate=%v err=%v", duplicate, err)
	}
}

func TestApprovalNodeFailureKeepsIntentForSameAnswerRetry(t *testing.T) {
	ctx := context.Background()
	fixture := createApprovalIntentFixture(t, ctx, "approval-node-failure", nil)
	const savedResponse = "approved original request"
	if _, _, err := fixture.store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, ApprovalApproved, "owner", savedResponse); err != nil {
		t.Fatal(err)
	}
	failed := approvalReceipt(fixture, WorkerCommandReceiptDenied)
	failed.ErrorCode = "runtime_session_unavailable"
	failed.ErrorMessage = "Node could not deliver the response"
	if _, err := fixture.store.ReconcileWorkerCommandReceipt(ctx, NodeReference(fixture.worker.NodeID), failed); err != nil {
		t.Fatal(err)
	}
	approval, err := fixture.store.Approval(ctx, fixture.approval.RequestID)
	if err != nil || approval.State != ApprovalResolving || approval.Response != "" {
		t.Fatalf("Node delivery failure finalized/replaced approval decision: state=%q err=%v", approval.State, err)
	}
	if _, _, err := fixture.store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, ApprovalDenied, "owner", "denied"); !errors.Is(err, ErrApprovalResolutionConflict) {
		t.Fatalf("changed decision replaced durable intent after Node failure: %v", err)
	}
	if _, retry, err := fixture.store.RetryWorkerCommand(ctx, fixture.command.ID, time.Now().UTC()); err != nil || !retry {
		t.Fatalf("retry same durable command: retry=%v err=%v", retry, err)
	}
	if approval, duplicate, err := fixture.store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, ApprovalApproved, "other-owner", savedResponse); err != nil || !duplicate || approval.State != ApprovalResolving {
		t.Fatalf("same decision retry was not idempotent: state=%q duplicate=%v err=%v", approval.State, duplicate, err)
	}
	if _, err := fixture.store.ReconcileWorkerCommandReceipt(ctx, NodeReference(fixture.worker.NodeID), approvalReceipt(fixture, WorkerCommandReceiptAccepted)); err != nil {
		t.Fatal(err)
	}
	resolved, err := fixture.store.Approval(ctx, fixture.approval.RequestID)
	if err != nil || resolved.State != ApprovalApproved || resolved.Response != savedResponse || resolved.ResolvedBy != "owner" {
		t.Fatalf("retry receipt did not commit original decision: state=%q actor=%q err=%v", resolved.State, resolved.ResolvedBy, err)
	}
}

func TestApprovalResolutionIntentSurvivesStoreReopenAndAcceptedReceipt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "approval-reopen.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedApprovalIntentFixture(t, ctx, store, "approval-intent-reopen", nil)
	const savedResponse = "original answer across restart"
	if _, _, err := store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, ApprovalDenied, "owner", savedResponse); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandUncertain(ctx, fixture.command.ID, "receipt not yet received"); err != nil {
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
	fixture.store = reopened
	approval, err := reopened.Approval(ctx, fixture.approval.RequestID)
	if err != nil || approval.State != ApprovalResolving || approval.ResolutionCommandID != fixture.command.ID || approval.ResolutionState != ApprovalDenied || approval.ResolutionResponse != savedResponse {
		t.Fatalf("resolution intent was not durable across reopen: state=%q err=%v", approval.State, err)
	}
	if _, err := reopened.ReconcileWorkerCommandReceipt(ctx, NodeReference(fixture.worker.NodeID), approvalReceipt(fixture, WorkerCommandReceiptAccepted)); err != nil {
		t.Fatal(err)
	}
	approval, err = reopened.Approval(ctx, fixture.approval.RequestID)
	if err != nil || approval.State != ApprovalDenied || approval.Response != savedResponse || approval.ResolvedBy != "owner" {
		t.Fatalf("reopened accepted receipt did not apply original intent: state=%q actor=%q err=%v", approval.State, approval.ResolvedBy, err)
	}
}

func TestApprovalIntentRestartRecoveryReleasesStaleLeaseWithoutSending(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "approval-pending-restart.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := seedApprovalIntentFixture(t, ctx, store, "approval-pending-restart", nil)
	const savedResponse = "deny original action"
	if _, _, err := store.BeginApprovalResolution(ctx, fixture.approval.RequestID, fixture.command.ID, ApprovalDenied, "owner", savedResponse); err != nil {
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
	recovered, err := reopened.RecoverApprovalResolutionCommands(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("recovered stale Approval handoffs=%d err=%v", recovered, err)
	}
	command, found, err := reopened.FindWorkerCommand(ctx, "respond", "request:"+fixture.approval.RequestID, fixture.worker.ID, fixture.attempt.ID)
	if err != nil || !found || command.ID != fixture.command.ID || command.State != WorkerCommandUncertain || !command.LeaseUntil.IsZero() {
		t.Fatalf("restart did not release the existing command for explicit retry: found=%v state=%q err=%v", found, command.State, err)
	}
	approval, err := reopened.Approval(ctx, fixture.approval.RequestID)
	if err != nil || approval.State != ApprovalResolving || approval.ResolutionCommandID != fixture.command.ID || approval.ResolutionState != ApprovalDenied || approval.ResolutionResponse != savedResponse {
		t.Fatalf("restart changed saved Approval intent: state=%q err=%v", approval.State, err)
	}
	if command.State == WorkerCommandDelivered {
		t.Fatal("restart recovery automatically sent the uncertain command")
	}
}

func TestDirectApprovalAfterResultCannotReviveTerminalTurn(t *testing.T) {
	ctx := context.Background()
	fixture := createApprovalIntentFixture(t, ctx, "approval-terminal-direct", nil)
	_, canonical, _, err := fixture.store.RecordAttemptOutcome(ctx, fixture.attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "already finished",
	})
	if err != nil || canonical == nil {
		t.Fatalf("record Result: result=%#v err=%v", canonical, err)
	}
	approval, _, err := fixture.store.ResolveApproval(ctx, fixture.approval.RequestID, ApprovalApproved, "owner", "approve")
	if err != nil || approval.State != ApprovalApproved {
		t.Fatalf("resolve approval: state=%q err=%v", approval.State, err)
	}
	turn, err := fixture.store.Turn(ctx, fixture.turn.ID)
	if err != nil || turn.State != TurnSucceeded || turn.ResultID != canonical.ID {
		t.Fatalf("approval resolution revived/replaced terminal Turn: state=%q result=%q err=%v", turn.State, turn.ResultID, err)
	}
	worker, err := fixture.store.Worker(ctx, fixture.worker.ID)
	if err != nil || worker.Status != WorkerIdle {
		t.Fatalf("approval resolution changed terminal Worker status: status=%q err=%v", worker.Status, err)
	}
}

type approvalIntentFixture struct {
	store    *Store
	worker   Worker
	turn     Turn
	attempt  Phase4Attempt
	approval Approval
	command  WorkerCommand
}

func createApprovalIntentFixture(t *testing.T, ctx context.Context, requestID string, expiresAt *time.Time) approvalIntentFixture {
	t.Helper()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "approval-intent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return seedApprovalIntentFixture(t, ctx, store, requestID, expiresAt)
}

func seedApprovalIntentFixture(t *testing.T, ctx context.Context, store *Store, requestID string, expiresAt *time.Time) approvalIntentFixture {
	t.Helper()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID, spec.HarnessInstanceID = "approval-node", "approval-node/fx"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "approval intent fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	activity := Activity{Metadata: ActivityMetadata{
		EventID: "approval-event-" + requestID, Node: NodeReference(worker.NodeID), HarnessInstanceID: HarnessInstanceID(worker.HarnessInstanceID),
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
	}, Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: requestID, Summary: "synthetic permission", ExpiresAt: expiresAt}}
	if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
		t.Fatal(err)
	}
	approval, err := store.Approval(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
	if err != nil || duplicate {
		t.Fatalf("claim response command: duplicate=%v err=%v", duplicate, err)
	}
	return approvalIntentFixture{store: store, worker: worker, turn: turn, attempt: attempt, approval: approval, command: command}
}

func approvalReceipt(f approvalIntentFixture, state WorkerCommandReceiptState) WorkerCommandReceipt {
	return WorkerCommandReceipt{
		CommandID: f.command.ID, Kind: "respond", TurnID: f.turn.ID, AttemptID: f.attempt.ID,
		State: state,
	}
}
