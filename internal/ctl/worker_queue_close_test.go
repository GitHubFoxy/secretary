package ctl

import (
	"context"
	"errors"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"testing"
	"time"
)

type heldQueueRuntime struct {
	lifecycleRuntime
	ready   chan struct{}
	release chan struct{}
}

func (r *heldQueueRuntime) Dispatch(ctx context.Context, _ string, _ core.Worker, _ core.Turn, _ core.Phase4Attempt, _ core.DispatchResolution) error {
	close(r.ready)
	select {
	case <-r.release:
		r.add("dispatch")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *heldQueueRuntime) Cancel(ctx context.Context, _ string, _ core.Worker, attempt core.Phase4Attempt) error {
	if r.count("dispatch") == 0 {
		return node.ErrRuntimeSessionUnavailable
	}
	r.add("cancel")
	return r.publishCanceled(ctx, attempt)
}
func TestCloseOrdersQueuedHandoffBeforeCancellation(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q queued work", IdempotencyKey: "held"}); err != nil {
		t.Fatal(err)
	}
	runtime := &heldQueueRuntime{lifecycleRuntime: lifecycleRuntime{store: store}, ready: make(chan struct{}), release: make(chan struct{})}
	service.Runtime = runtime
	pumpDone := make(chan error, 1)
	go func() { pumpDone <- service.ProcessQueuedWorkerMessages(ctx) }()
	<-runtime.ready
	closeDone := make(chan error, 1)
	closing := service
	go func() { _, err := closing.CloseWorker(ctx, details.Worker.WorkerRef); closeDone <- err }()
	select {
	case err := <-closeDone:
		close(runtime.release)
		<-pumpDone
		t.Fatalf("Close overtook unaccepted queue handoff: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	during, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if during.QueuedMessages[0].State == "canceled" {
		t.Fatal("queue says canceled while its handoff can still execute")
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, err := closing.CloseWorker(timeoutCtx, details.Worker.WorkerRef); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked Close ignored deadline: %v", err)
	}
	close(runtime.release)
	if err := <-pumpDone; err != nil {
		t.Fatal(err)
	}
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	closed, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || closed.Worker.Status != core.WorkerClosed || runtime.count("cancel") != 1 {
		t.Fatalf("closed=%#v err=%v", closed, err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil && !errors.Is(err, core.ErrInvalidTransition) {
		t.Fatal(err)
	}
	if runtime.count("dispatch") != 1 {
		t.Fatal("closed queue executed again")
	}
}

func TestCloseCancelsPreparedQueueBeforeAnyRuntimeHandoff(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	queued, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q never execute", IdempotencyKey: "close-prepared"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PromoteQueuedWorkerMessage(ctx, queued.QueuedMessages[0], "dispatch"); err != nil {
		t.Fatal(err)
	}
	runtime := &heldQueueRuntime{}
	service.Runtime = runtime
	closed, err := service.CloseWorker(ctx, details.Worker.WorkerRef)
	if err != nil || closed.Worker.Status != core.WorkerClosed || closed.QueuedMessages[0].State != "canceled" {
		t.Fatalf("closed=%#v err=%v", closed, err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.count("dispatch") != 0 {
		t.Fatal("canceled prepared queue was dispatched")
	}
	if _, _, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", closed.Worker.ID, closed.CurrentAttempt().ID); !errors.Is(err, core.ErrInvalidTransition) {
		t.Fatalf("canceled intent remained claimable: %v", err)
	}
}
