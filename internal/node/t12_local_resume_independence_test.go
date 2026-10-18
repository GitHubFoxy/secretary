package node

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Resume A holds the provider call open indefinitely; independent Worker B
// must still be dispatchable, queryable, and cancellable.
type t12BlockingResumeRuntime struct {
	t12AttemptBoundRuntime
	entered chan struct{}
	release chan struct{}
	resumeCalls atomic.Int32
}

func (r *t12BlockingResumeRuntime) Resume(ctx context.Context, request StartRequest, id string) (Session, error) {
	r.resumeCalls.Add(1)
	select {
	case r.entered <- struct{}{}:
	default:
	}
	select {
	case <-r.release:
		return r.t12AttemptBoundRuntime.Resume(ctx, request, id)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestT12LocalResumeBlockedNativeDoesNotBlockOtherWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runtime := &t12BlockingResumeRuntime{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	defer func() {
		select { case <-runtime.release: default: close(runtime.release) }
	}()
	local := NewLocal(runtime)

	workerA := StartRequest{
		WorkerRef: "worker-A", AttemptID: "attempt-A",
		Workspace: t.TempDir(),
	}
	aDone := make(chan error, 1)
	go func() {
		_, err := local.Resume(ctx, workerA, "existing-native-A")
		aDone <- err
	}()
	select {
	case <-runtime.entered:
	case <-ctx.Done():
		t.Fatal("Worker A never entered native Resume")
	}

	// A blocked native provider must not hold LocalNode.mu or the gate for B.
	bCtx, stopB := context.WithTimeout(ctx, 3*time.Second)
	defer stopB()
	workerB := StartRequest{
		WorkerRef: "worker-B", AttemptID: "attempt-B",
		Workspace: t.TempDir(),
	}
	bDone := make(chan error, 1)
	go func() {
		_, err := local.Dispatch(bCtx, workerB)
		bDone <- err
	}()
	select {
	case err := <-bDone:
		if err != nil {
			t.Fatalf("independent B Dispatch failed: %v", err)
		}
	case <-bCtx.Done():
		t.Fatal("Worker B Dispatch blocked by A native Resume")
	}

	lookup := make(chan bool, 1)
	go func() {
		_, ok := local.Session(workerB.WorkerRef)
		lookup <- ok
	}()
	select {
	case ok := <-lookup:
		if !ok {
			t.Fatal("Worker B Session missing while A Resume blocked")
		}
	case <-bCtx.Done():
		t.Fatal("Worker B Session lookup blocked by A native Resume")
	}

	bCanceled := make(chan struct {
		handled bool
		err error
	}, 1)
	go func() {
		ok, err := local.CancelLocalAttempt(bCtx, workerB.WorkerRef, workerB.AttemptID)
		bCanceled <- struct {
			handled bool
			err error
		}{ok, err}
	}()
	select {
	case result := <-bCanceled:
		if result.err != nil || !result.handled || runtime.cancels.Load() != 1 {
			t.Fatalf("Worker B Cancel not delivered: handled=%t err=%v cancels=%d", result.handled, result.err, runtime.cancels.Load())
		}
	case <-bCtx.Done():
		t.Fatal("Worker B Cancel blocked by A native Resume")
	}

	// Per-Worker serialization remains intact: a second A Resume cannot
	// reach the provider while the first A Resume holds its gate.
	secondCtx, stopSecond := context.WithTimeout(ctx, 120*time.Millisecond)
	defer stopSecond()
	_, err := local.Resume(secondCtx, workerA, "another-native-A")
	if !errors.Is(err, context.DeadlineExceeded) || runtime.resumeCalls.Load() != 1 {
		t.Fatalf("same-Worker Resume was not serialized: err=%v calls=%d", err, runtime.resumeCalls.Load())
	}
	select {
	case err := <-aDone:
		t.Fatalf("Worker A Resume unexpectedly finished before release: %v", err)
	default:
	}

	close(runtime.release)
	select {
	case err := <-aDone:
		if err != nil {
			t.Fatalf("Worker A Resume failed on release: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Worker A did not resume after provider was released")
	}
}
