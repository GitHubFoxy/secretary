package core

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRecoverInterruptedMarksActiveAttemptWithoutRetry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "recovery.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "resume")
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.AcceptDispatch(ctx, task.ID, "worker", "local", "session", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetAttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	phase4Spec := phase4WorkerSpec()
	phase4Spec.WorkerRef = "recovery-phase4-worker"
	_, _, phase4Attempt, err := store.CreateWorker(ctx, conversation.ID, phase4Spec, TurnSpec{Input: "phase4 remains running"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, phase4Attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	details, err := reopened.TaskDetails(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Attempts[0].State != AttemptActive {
		t.Fatalf("Open changed active Attempt: %#v", details)
	}
	if err := reopened.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	details, err = reopened.TaskDetails(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Task.State != TaskOpen || details.Attempts[0].State != AttemptInterrupted {
		t.Fatalf("recovered details=%#v", details)
	}
	phase4AfterLegacyRecovery, err := reopened.Phase4Attempt(ctx, phase4Attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if phase4AfterLegacyRecovery.State != AttemptActive {
		t.Fatalf("legacy recovery changed Phase 4 Attempt: %#v", phase4AfterLegacyRecovery)
	}
}
