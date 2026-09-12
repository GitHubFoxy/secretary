package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkerRequestCreatesDurableApprovalAndTransitionsState(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID = "macbook"
	spec.HarnessInstanceID = "macbook/claude"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "edit file"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	activity := Activity{Metadata: ActivityMetadata{EventID: "permission-1", Node: "macbook", HarnessInstanceID: "macbook/claude", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: "request-1", Summary: "write to the repository"}}
	if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
		t.Fatal(err)
	}
	approval, err := store.Approval(ctx, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if approval.WorkerID != worker.ID || approval.TurnID != turn.ID || approval.AttemptID != attempt.ID || approval.NodeID != string(worker.NodeID) || approval.ProjectID != worker.ProjectID {
		t.Fatalf("approval binding=%#v", approval)
	}
	if approval.State != ApprovalPending {
		t.Fatalf("approval state=%q", approval.State)
	}
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if details.Worker.Status != WorkerWaitingApproval || details.Turns[0].State != TurnWaitingApproval {
		t.Fatalf("waiting state worker=%q turn=%q", details.Worker.Status, details.Turns[0].State)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
		t.Fatal(err)
	}
	approvals, err := store.Approvals(ctx)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("approvals=%#v err=%v", approvals, err)
	}
	events, err := store.EventsRecent(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "approval.requested" && event.AggregateID == approval.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("approval audit event missing: %#v", events)
	}
}

func TestApprovalResolutionIsIdempotentAndDeniedCannotRerun(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID = "remote"
	spec.HarnessInstanceID = "remote/fx"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "danger"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, Activity{Metadata: ActivityMetadata{EventID: "permission-2", Node: "remote", HarnessInstanceID: "remote/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: "request-2", Summary: "run command"}}); err != nil {
		t.Fatal(err)
	}
	first, duplicate, err := store.ResolveApproval(ctx, "request-2", ApprovalDenied, "client-1", "no")
	if err != nil || duplicate || first.State != ApprovalDenied {
		t.Fatalf("first=%#v duplicate=%v err=%v", first, duplicate, err)
	}
	second, duplicate, err := store.ResolveApproval(ctx, "request-2", ApprovalApproved, "client-2", "yes")
	if err != nil || !duplicate || second.State != ApprovalDenied || second.ResolvedBy != "client-1" {
		t.Fatalf("second=%#v duplicate=%v err=%v", second, duplicate, err)
	}
	third, duplicate, err := store.ResolveApproval(ctx, "request-2", ApprovalApproved, "client-1", "yes")
	if err != nil || !duplicate || third.State != ApprovalDenied {
		t.Fatalf("third=%#v duplicate=%v err=%v", third, duplicate, err)
	}
}

func TestApprovalAndInputSurviveStoreReconnectAndExpiredOrRevokedAreVisible(t *testing.T) {
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
	spec := phase4WorkerSpec()
	spec.NodeID = "remote"
	spec.HarnessInstanceID = "remote/fx"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, Activity{Metadata: ActivityMetadata{EventID: "input-1", Node: "remote", HarnessInstanceID: "remote/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityUserInputRequest, Request: &ActivityRequest{RequestID: "request-input", Summary: "which file?"}}); err != nil {
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
	approval, err := store.Approval(ctx, "request-input")
	if err != nil || approval.State != ApprovalPending || approval.Kind != ApprovalInput {
		t.Fatalf("reconnected approval=%#v err=%v", approval, err)
	}
	if _, _, err := store.ExpireApproval(ctx, "request-input", time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	approval, err = store.Approval(ctx, "request-input")
	if err != nil || approval.State != ApprovalExpired {
		t.Fatalf("expired approval=%#v err=%v", approval, err)
	}
}

func TestTrustedLocalApprovalRequiresExplicitPolicyAndAudits(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID = "local"
	spec.HarnessInstanceID = "local/fx"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, Activity{Metadata: ActivityMetadata{EventID: "permission-local", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: "request-local", Summary: "write locally"}}); err != nil {
		t.Fatal(err)
	}
	if approval, err := store.ApplyTrustedLocalApproval(ctx, "request-local", TrustedLocalApprovalPolicy{}); err != nil || approval.State != ApprovalPending {
		t.Fatalf("implicit auto approval: approval=%#v err=%v", approval, err)
	}
	if _, err := store.ApplyTrustedLocalApproval(ctx, "request-local", TrustedLocalApprovalPolicy{Enabled: true, Explicit: true, Node: "local"}); !errors.Is(err, ErrTrustedLocalApprovalDenied) {
		t.Fatalf("remote-like auto approval was accepted: %v", err)
	}
	approval, err := store.ApplyTrustedLocalApproval(ctx, "request-local", TrustedLocalApprovalPolicy{Enabled: true, Explicit: true, LocalNode: true, Node: "local"})
	if err != nil || approval.State != ApprovalApproved {
		t.Fatalf("explicit auto approval=%#v err=%v", approval, err)
	}
	for _, event := range mustEvents(t, store) {
		if event.Kind == "approval.auto_approved" && event.AggregateID == approval.ID {
			return
		}
	}
	t.Fatal("auto approval audit event missing")
}

func TestRevokedApprovalIsVisibleAndNotRerun(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID, spec.HarnessInstanceID = "remote", "remote/fx"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "revoke"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	activity := Activity{Metadata: ActivityMetadata{EventID: "permission-revoked", Node: "remote", HarnessInstanceID: "remote/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: "request-revoked", Summary: "delete"}}
	if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
		t.Fatal(err)
	}
	approval, duplicate, err := store.RevokeApproval(ctx, "request-revoked", "node revoked")
	if err != nil || duplicate || approval.State != ApprovalRevoked {
		t.Fatalf("approval=%#v duplicate=%v err=%v", approval, duplicate, err)
	}
	again, duplicate, err := store.ResolveApproval(ctx, "request-revoked", ApprovalApproved, "client", "approved")
	if err != nil || !duplicate || again.State != ApprovalRevoked {
		t.Fatalf("rerun approval=%#v duplicate=%v err=%v", again, duplicate, err)
	}
}

func mustEvents(t *testing.T, store *Store) []Event {
	t.Helper()
	events, err := store.EventsRecent(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return events
}
