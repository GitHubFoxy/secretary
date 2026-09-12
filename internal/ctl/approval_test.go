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
