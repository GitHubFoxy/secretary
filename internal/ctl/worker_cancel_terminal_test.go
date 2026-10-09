package ctl

import (
	"context"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type ccCancelReproRuntime struct {
	lifecycleRuntime
	session node.Session
}

func (r *ccCancelReproRuntime) Cancel(ctx context.Context, _ string, _ core.Worker, _ core.Phase4Attempt) error {
	return r.session.Cancel(ctx)
}
func TestCCCancelDoesNotDeliverQueueBeforeNativeTerminal(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	script := filepath.Join(t.TempDir(), "claude")
	if e := os.WriteFile(script, []byte("#!/bin/sh\nread prompt\nprintf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"working\"}]}}'\nread interrupt\nread held_terminal\n"), 0700); e != nil {
		t.Fatal(e)
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	session, e := (node.ClaudeCodeRuntime{Command: script}).Start(cctx, node.StartRequest{Workspace: t.TempDir(), Task: "work"})
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	select {
	case <-session.Activity():
	case <-cctx.Done():
		t.Fatal(cctx.Err())
	}
	r := &ccCancelReproRuntime{session: session}
	service.Runtime = r
	d := spawnLifecycleWorker(t, ctx, service, project)
	if _, e = store.SetPhase4AttemptActive(ctx, d.Attempts[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: d.Worker.WorkerRef, Text: "/q next", IdempotencyKey: "next"}); e != nil {
		t.Fatal(e)
	}
	if _, e = service.CancelWorker(ctx, d.Worker.WorkerRef); e != nil {
		t.Fatal(e)
	}
	if e = session.Prompt(ctx, "too soon"); e == nil {
		t.Fatal("fixture native Attempt unexpectedly ended")
	}
	if e = service.ProcessQueuedWorkerMessages(ctx); e != nil {
		t.Fatal(e)
	}
	if r.count("dispatch") != 1 {
		t.Fatalf("queue dispatched before old native terminal: dispatches=%d", r.count("dispatch"))
	}
	current, err := service.GetWorker(ctx, d.Worker.WorkerRef)
	if err != nil || current.CurrentAttempt().State != core.AttemptActive || len(current.Results) != 0 {
		t.Fatalf("Cancel receipt fabricated terminal: %#v %v", current, err)
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer closeCancel()
	if _, err := service.CloseWorker(closeCtx, d.Worker.WorkerRef); err != context.DeadlineExceeded {
		t.Fatalf("Close did not wait for native terminal: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "canceled" {
			t.Fatalf("native terminal=%#v", result)
		}
		if _, _, _, err := store.RecordAttemptOutcome(ctx, d.CurrentAttempt().ID, core.AttemptOutcomeInput{Status: core.OutcomeCanceled, Classification: core.OutcomeFinal, Summary: result.Summary}); err != nil {
			t.Fatal(err)
		}
	case <-cctx.Done():
		t.Fatal(cctx.Err())
	}
	closed, err := service.CloseWorker(ctx, d.Worker.WorkerRef)
	if err != nil || closed.Worker.Status != core.WorkerClosed || len(closed.Results) != 1 || closed.QueuedMessages[0].State != "canceled" {
		t.Fatalf("native completion did not unblock closure: %#v %v", closed, err)
	}
}
