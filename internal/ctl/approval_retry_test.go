package ctl

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestWorkerServiceRetriesSavedApprovalIntentAfterRestartWithSameCommand(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "approval-retry.db")
	ctx, store, service, project := newWorkerServiceAt(t, path)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	const requestID = "approval-retry-after-restart"
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{
		EventID: "approval-retry-event", Node: core.NodeReference(details.Worker.NodeID),
		HarnessInstanceID: core.HarnessInstanceID(details.Worker.HarnessInstanceID), WorkerRef: details.Worker.WorkerRef,
		TurnID: attempt.TurnID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
	}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: requestID, Summary: "synthetic permission"}}); err != nil {
		t.Fatal(err)
	}

	// The service has no runtime after BeginApprovalResolution commits. This
	// models a server stopping between the durable intent and Node handoff.
	service.Runtime = nil
	if _, err := service.RespondWorker(ctx, MessageWorkerRequest{
		WorkerRef: details.Worker.WorkerRef, Text: "deny", RequestID: requestID, ClientID: "owner-client",
	}); err == nil {
		t.Fatal("expected the initial handoff to stop before reaching the Node")
	}
	approval, err := store.Approval(ctx, requestID)
	if err != nil || approval.State != core.ApprovalResolving || approval.ResolutionResponse != "deny" {
		t.Fatalf("initial durable intent was not kept: state=%q err=%v", approval.State, err)
	}
	command, found, err := store.FindWorkerCommand(ctx, "respond", "request:"+requestID, details.Worker.ID, attempt.ID)
	if err != nil || !found || command.ID != approval.ResolutionCommandID {
		t.Fatalf("original response command missing: found=%v err=%v", found, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.RecoverApprovalResolutionCommands(ctx); err != nil {
		t.Fatal(err)
	}
	service.Store = reopened
	retryRuntime := &approvalRetryRecorder{calls: make(chan approvalRetryCall, 1)}
	service.Runtime = retryRuntime
	recovered, err := service.RetryApprovalResolution(ctx, requestID, "owner-client")
	if err != nil || len(recovered.Approvals) != 1 || recovered.Approvals[0].State != core.ApprovalDenied {
		t.Fatalf("explicit retry did not finalize the saved decision: approvals=%d err=%v", len(recovered.Approvals), err)
	}
	call := <-retryRuntime.calls
	if call.commandID != command.ID || call.worker.ID != details.Worker.ID || call.attempt.ID != attempt.ID || call.requestID != requestID || call.response != "deny" {
		t.Fatal("retry did not reuse the exact saved command, Attempt, and response")
	}
	current, found, err := reopened.FindWorkerCommand(ctx, "respond", "request:"+requestID, details.Worker.ID, attempt.ID)
	if err != nil || !found || current.ID != command.ID || current.State != core.WorkerCommandDelivered || retryRuntime.count() != 1 {
		t.Fatalf("retry created or sent a second command: found=%v state=%q calls=%d err=%v", found, current.State, retryRuntime.count(), err)
	}
}

type approvalRetryCall struct {
	commandID string
	worker    core.Worker
	attempt   core.Phase4Attempt
	requestID string
	response  string
}

type approvalRetryRecorder struct {
	calls chan approvalRetryCall
	sent  atomic.Int32
}

func (*approvalRetryRecorder) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error {
	return nil
}
func (*approvalRetryRecorder) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return nil
}
func (r *approvalRetryRecorder) Respond(_ context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt, requestID, response string) error {
	r.sent.Add(1)
	r.calls <- approvalRetryCall{commandID: commandID, worker: worker, attempt: attempt, requestID: requestID, response: response}
	return nil
}
func (*approvalRetryRecorder) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error {
	return nil
}
func (*approvalRetryRecorder) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return nil
}
func (r *approvalRetryRecorder) count() int { return int(r.sent.Load()) }
