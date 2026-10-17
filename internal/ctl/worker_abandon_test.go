package ctl

import (
	"context"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestOwnerAbandonsUnresolvedAttemptWithoutNodeAndCanCloseWorker(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.CurrentAttempt()
	if attempt == nil { t.Fatal("missing Attempt") }
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
	if err := store.RecoverPhase4Attempts(ctx, core.Phase4AttemptRecoveryFunc(
		func(context.Context, core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
			return core.Phase4RecoveryAwaiting, nil
		})); err != nil { t.Fatal(err) }
	if _, err := service.AbandonAttempt(ctx, details.Worker.WorkerRef, "different-attempt"); err == nil {
		t.Fatal("unrelated Attempt was abandoned")
	}
	recovered, err := service.AbandonAttempt(ctx, details.Worker.WorkerRef, attempt.ID)
	if err != nil { t.Fatal(err) }
	if recovered.Worker.Status != core.WorkerOffline || recovered.CurrentAttempt().State != core.AttemptInterrupted ||
		len(recovered.Results) != 1 || recovered.Results[0].FailureCode != ownerAbandonedUnknownExecution {
		t.Fatalf("abandoned state=%#v", recovered)
	}
	if _, err := service.AbandonAttempt(ctx, details.Worker.WorkerRef, attempt.ID); err != nil {
		t.Fatalf("idempotent abandon: %v", err)
	}
	if _, _, duplicate, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "delayed completion",
	}); err != nil || !duplicate {
		t.Fatalf("late Result was not deduplicated: %v %v", duplicate, err)
	}
	closed, err := service.CloseWorker(ctx, details.Worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if closed.Worker.Status != core.WorkerClosed || len(closed.Results) != 1 {
		t.Fatalf("abandoned Worker could not be closed: %#v", closed)
	}
}
