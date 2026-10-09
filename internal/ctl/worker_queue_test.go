package ctl

import (
	"context"
	"errors"
	"github.com/beruseruko/secretary/internal/core"
	"path/filepath"
	"testing"
)

func TestWorkerQueueWaitsForTerminalAndPreservesFIFO(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	runtime := service.Runtime.(*lifecycleRuntime)
	first := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q second", IdempotencyKey: "queued-second"}
	queued, err := service.MessageWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued.QueuedMessages) != 1 || queued.ActionMode != "queued" || queued.QueuedMessages[0].Text != "second" || runtime.count("steer") != 0 || len(queued.Attempts) != 1 {
		t.Fatalf("queued=%#v", queued)
	}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: request.WorkerRef, Text: "/q third", IdempotencyKey: "queued-third"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.count("dispatch") != 1 {
		t.Fatal("queue delivered while active")
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, first.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := service.GetWorker(ctx, request.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Attempts) != 2 || next.CurrentTurn().Input != "second" || next.QueuedMessages[0].State != "delivered" || next.QueuedMessages[1].State != "pending" {
		t.Fatalf("next=%#v", next)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.count("dispatch") != 2 {
		t.Fatal("duplicate queue delivery")
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: request.WorkerRef, Text: "/q"}); err == nil {
		t.Fatal("empty queue accepted")
	}
}

func TestWorkerQueueSurvivesServerRestartAndClosure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secretary.db")
	ctx, store, service, project := newWorkerServiceAt(t, path)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q remembered", IdempotencyKey: "restart-message"}
	accepted, err := service.MessageWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	service.Store = reopened
	replay, err := service.MessageWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.QueuedMessages) != 1 || replay.QueuedMessages[0].ID != accepted.QueuedMessages[0].ID {
		t.Fatal("restart duplicated queue")
	}
	if _, _, _, err := reopened.InterruptPhase4Attempt(ctx, details.Attempts[0].ID, "restart", "server restarted"); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorker(ctx, request.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if service.Runtime.(*lifecycleRuntime).count("resume") != 1 || current.CurrentTurn().Input != "remembered" {
		t.Fatal("interrupted binding was not resumed")
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: request.WorkerRef, Text: "/q do not run", IdempotencyKey: "close-message"}); err != nil {
		t.Fatal(err)
	}
	closed, err := service.CloseWorker(ctx, request.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if closed.QueuedMessages[1].State != "canceled" {
		t.Fatalf("closed=%#v", closed)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	if service.Runtime.(*lifecycleRuntime).count("dispatch") != 1 {
		t.Fatal("closed queue ran")
	}
}

type failedQueueRuntime struct{ lifecycleRuntime }

func (r *failedQueueRuntime) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error {
	r.add("dispatch")
	return errors.New("runtime_session_unavailable")
}
func TestWorkerQueueExposesFailureWithoutAutomaticRetry(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	failed := &failedQueueRuntime{}
	service.Runtime = failed
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q unavailable", IdempotencyKey: "blocked"}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err == nil {
		t.Fatal("delivery failure hidden")
	}
	current, err := service.GetWorker(ctx, request.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.QueuedMessages[0].State != "blocked" || current.QueuedMessages[0].LastError == "" {
		t.Fatalf("queue=%#v", current.QueuedMessages)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	if failed.count("dispatch") != 1 {
		t.Fatal("failed delivery automatically retried")
	}
}

func TestWorkerQueueRecoversPreparedDeliveryAndDoesNotAnswerInputRequest(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	queued, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q next", RequestID: "old-input", IdempotencyKey: "prepared"})
	if err != nil {
		t.Fatal(err)
	}
	if service.Runtime.(*lifecycleRuntime).count("respond") != 0 {
		t.Fatal("queue answered pending input")
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PromoteQueuedWorkerMessage(ctx, queued.QueuedMessages[0], "dispatch"); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Attempts) != 2 || current.QueuedMessages[0].State != "delivered" || service.Runtime.(*lifecycleRuntime).count("dispatch") != 2 {
		t.Fatalf("current=%#v", current)
	}
}

func TestWorkerQueueKeepsDistinctInputsWithSameText(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"one", "two"} {
		if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q identical", IdempotencyKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.QueuedMessages) != 2 || current.QueuedMessages[0].ID == current.QueuedMessages[1].ID {
		t.Fatalf("messages=%#v", current.QueuedMessages)
	}
}

func TestWorkerQueuePreservesSecretaryOriginAfterSecretaryTurnFinishes(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	identity, err := store.GetSecretaryIdentity(ctx, service.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "continue Worker later")
	if err != nil {
		t.Fatal(err)
	}
	origin, err = store.StartSecretaryTurn(ctx, origin.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q attributed follow-up", SecretaryTurnID: origin.ID, InputID: origin.InputID, IdempotencyKey: "origin-queue"}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FinishSecretaryTurnWithResponse(ctx, origin.ID, core.SecretaryTurnSucceeded, "", "Queued"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorker(ctx, request.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, current.CurrentAttempt().ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "attributed"}); err != nil {
		t.Fatal(err)
	}
	hasResult, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID)
	if err != nil || !hasResult {
		t.Fatalf("queued Result lost origin: hasResult=%v err=%v", hasResult, err)
	}
}

func TestWorkerQueueDoesNotReplayInterruptedPromotedAttempt(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	queued, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "/q before crash", IdempotencyKey: "interrupted-queue"})
	if err != nil {
		t.Fatal(err)
	}
	_, attempt, err := store.PromoteQueuedWorkerMessage(ctx, queued.QueuedMessages[0], "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.InterruptPhase4Attempt(ctx, attempt.ID, "restart", "lost execution"); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.QueuedMessages[0].State != "blocked" || service.Runtime.(*lifecycleRuntime).count("dispatch") != 1 {
		t.Fatal("interrupted promoted Attempt automatically replayed or lost visible failure")
	}
}
