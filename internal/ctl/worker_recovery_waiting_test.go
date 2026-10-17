package ctl

import (
	"context"
	"errors"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestUnresolvedRemoteAttemptBlocksFollowUpAndQueueUntilTerminal(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	initial := details.CurrentAttempt()
	if initial == nil { t.Fatal("missing Attempt") }
	if _, err := store.SetPhase4AttemptActive(ctx, initial.ID); err != nil { t.Fatal(err) }
	resolver := core.Phase4AttemptRecoveryFunc(func(context.Context, core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
		return core.Phase4RecoveryAwaiting, nil
	})
	if err := store.RecoverPhase4Attempts(ctx, resolver); err != nil { t.Fatal(err) }
	_, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef: details.Worker.WorkerRef, Text: "repeat risky operation", IdempotencyKey: "recover-direct",
	})
	if !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("unresolved execution allowed direct follow-up: %v", err)
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef: details.Worker.WorkerRef, Text: "/q later", IdempotencyKey: "recover-queue",
	}); err != nil { t.Fatal(err) }
	runtime := service.Runtime.(*lifecycleRuntime)
	before := runtime.count("dispatch") + runtime.count("resume")
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil { t.Fatal(err) }
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if current.QueuedMessages[0].State != "pending" || runtime.count("dispatch") + runtime.count("resume") != before {
		t.Fatalf("queued work replayed before Node outcome: %#v", current)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, initial.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "real terminal result",
	}); err != nil { t.Fatal(err) }
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil { t.Fatal(err) }
	current, err = service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if current.QueuedMessages[0].State != "delivered" || runtime.count("dispatch") + runtime.count("resume") != before+1 {
		t.Fatalf("queued work not resumed after terminal Result: %#v", current)
	}
}
