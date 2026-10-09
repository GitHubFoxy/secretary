package ctl

import (
	"context"
	"github.com/beruseruko/secretary/internal/core"
	"testing"
)

func TestPreparedQueueSurvivesProductionRecovery(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	d := spawnLifecycleWorker(t, ctx, service, project)
	if _, _, _, e := store.RecordAttemptOutcome(ctx, d.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); e != nil {
		t.Fatal(e)
	}
	q, e := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: d.Worker.WorkerRef, Text: "/q pre-handoff", IdempotencyKey: "crash"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = store.PromoteQueuedWorkerMessage(ctx, q.QueuedMessages[0], "dispatch"); e != nil {
		t.Fatal(e)
	}
	if e = store.RecoverPhase4Attempts(ctx, core.Phase4AttemptRecoveryFunc(func(context.Context, core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
		return core.Phase4RecoveryUnknown, nil
	})); e != nil {
		t.Fatal(e)
	}
	if e = service.ProcessQueuedWorkerMessages(ctx); e != nil {
		t.Fatal(e)
	}
	cur, e := service.GetWorker(ctx, d.Worker.WorkerRef)
	if e != nil {
		t.Fatal(e)
	}
	if cur.QueuedMessages[0].State != "delivered" {
		t.Fatalf("pre-handoff queue lost: state=%s attempt=%s dispatches=%d", cur.QueuedMessages[0].State, cur.CurrentAttempt().State, service.Runtime.(*lifecycleRuntime).count("dispatch"))
	}
}

func TestClaimedQueueIsNotReplayedAfterProductionRecovery(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	queued, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q acceptance unknown", IdempotencyKey: "claimed-crash"})
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, err := store.PromoteQueuedWorkerMessage(ctx, queued.QueuedMessages[0], "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", details.Worker.ID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverPhase4Attempts(ctx, core.Phase4AttemptRecoveryFunc(func(context.Context, core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
		return core.Phase4RecoveryUnknown, nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.QueuedMessages[0].State != "blocked" || current.CurrentAttempt().State != core.AttemptInterrupted || service.Runtime.(*lifecycleRuntime).count("dispatch") != 1 {
		t.Fatalf("unknown execution replayed: %#v", current)
	}
}
