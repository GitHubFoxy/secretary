package ctl

import (
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestWorkerServiceApprovalResponseUsesGenericCommandAndDedupesAction(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	activity := core.Activity{Metadata: core.ActivityMetadata{EventID: "permission-service", Node: "node", HarnessInstanceID: "node/fx", WorkerRef: details.Worker.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "service-request", Summary: "run shell"}}
	if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
		t.Fatal(err)
	}
	if details, err := service.GetWorker(ctx, details.Worker.WorkerRef); err != nil || details.Worker.Status != core.WorkerWaitingApproval {
		t.Fatalf("waiting details=%#v err=%v", details, err)
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	if _, err := service.RespondWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, RequestID: "service-request", Text: "approved", ClientID: "client-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RespondWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, RequestID: "service-request", Text: "approved", ClientID: "client-b"}); err != nil {
		t.Fatal(err)
	}
	if calls := runtime.count("respond"); calls != 1 {
		t.Fatalf("respond calls=%d", calls)
	}
	resolved, err := store.Approval(ctx, "service-request")
	if err != nil || resolved.State != core.ApprovalApproved || resolved.ResolvedBy != "client-a" {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
}

func TestWorkerServiceTrustedLocalApprovalHandsOffTypedResponse(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{EventID: "permission-local-service", Node: "node", HarnessInstanceID: "node/fx", WorkerRef: details.Worker.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "local-service-request", Summary: "run shell"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyTrustedLocalApproval(ctx, "local-service-request", core.TrustedLocalApprovalPolicy{Enabled: true, Explicit: true, LocalNode: true, Node: "node"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyTrustedLocalApproval(ctx, "local-service-request", core.TrustedLocalApprovalPolicy{Enabled: true, Explicit: true, LocalNode: true, Node: "node"}); err != nil {
		t.Fatal(err)
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	if runtime.count("respond") != 1 {
		t.Fatalf("trusted-local respond calls=%d", runtime.count("respond"))
	}
	approval, err := store.Approval(ctx, "local-service-request")
	if err != nil || approval.State != core.ApprovalApproved || approval.Response != "auto_approved" {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
}

func TestWorkerServiceApprovalResponseRecoversAfterDeliveredRespond(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{EventID: "permission-recovery", Node: "node", HarnessInstanceID: "node/fx", WorkerRef: details.Worker.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "recovery-request", Summary: "run shell"}}); err != nil {
		t.Fatal(err)
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "respond", "request:recovery-request", details.Worker.ID, attempt.ID)
	if err != nil || duplicate {
		t.Fatalf("claim duplicate=%v err=%v", duplicate, err)
	}
	if err := runtime.Respond(ctx, command.ID, details.Worker, attempt, "recovery-request", "denied"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandDelivered(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := service.RespondWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, RequestID: "recovery-request", Text: "denied", ClientID: "client"}); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.count("respond") != 1 {
		t.Fatalf("Respond calls=%d", runtime.count("respond"))
	}
	approval, err := store.Approval(ctx, "recovery-request")
	if err != nil || approval.State != core.ApprovalDenied {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
	final, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || len(final.Outcomes) != 1 || len(final.Results) != 1 || final.Worker.Status != core.WorkerIdle {
		t.Fatalf("final=%#v err=%v", final, err)
	}
}
