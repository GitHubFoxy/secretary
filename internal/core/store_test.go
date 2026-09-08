package core

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestConversationDeduplicatesInboundAndOrdersResult(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	first, duplicate, err := store.AppendInbound(ctx, conversation.ID, "web", "message-1", "find a phone")
	if err != nil || duplicate || first.Seq != 1 {
		t.Fatalf("first inbound = %#v, duplicate=%v, err=%v", first, duplicate, err)
	}
	again, duplicate, err := store.AppendInbound(ctx, conversation.ID, "web", "message-1", "find a phone")
	if err != nil || !duplicate || again.ID != first.ID {
		t.Fatalf("duplicate inbound = %#v, duplicate=%v, err=%v", again, duplicate, err)
	}

	task, err := store.CreateTask(ctx, conversation.ID, "research phones")
	if err != nil || task.State != TaskDispatching {
		t.Fatalf("task = %#v, err=%v", task, err)
	}
	acceptedTask, _, attempt, err := store.AcceptDispatch(ctx, task.ID, "phone-42", "local", "session-42")
	if err != nil || acceptedTask.State != TaskOpen || attempt.State != AttemptStarting {
		t.Fatalf("accepted dispatch: task=%#v attempt=%#v err=%v", acceptedTask, attempt, err)
	}
	if _, err := store.SetAttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	result, duplicate, err := store.CompleteAttempt(ctx, attempt.ID, ResultSucceeded, "three phones found")
	if err != nil || duplicate || result.Status != ResultSucceeded {
		t.Fatalf("result=%#v duplicate=%v err=%v", result, duplicate, err)
	}
	_, duplicate, err = store.CompleteAttempt(ctx, attempt.ID, ResultSucceeded, "three phones found")
	if err != nil || !duplicate {
		t.Fatalf("duplicate result: duplicate=%v err=%v", duplicate, err)
	}

	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Seq != 1 || entries[1].Seq != 2 || entries[1].Kind != EntryWorkerResult {
		t.Fatalf("entries = %#v", entries)
	}

	closed, err := store.CloseTask(ctx, task.ID)
	if err != nil || closed.Task.State != TaskClosed || closed.CancelAttempt != nil {
		t.Fatalf("close completed task = %#v, err=%v", closed, err)
	}
}

func TestDispatchFailureCanRetryWithoutNewTask(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "retry me")
	if err != nil {
		t.Fatal(err)
	}
	failed, err := store.MarkDispatchFailed(ctx, task.ID)
	if err != nil || failed.State != TaskDispatchFailed {
		t.Fatalf("failed dispatch = %#v, err=%v", failed, err)
	}
	retried, err := store.RetryDispatch(ctx, task.ID)
	if err != nil || retried.ID != task.ID || retried.State != TaskDispatching {
		t.Fatalf("retried dispatch = %#v, err=%v", retried, err)
	}
}

func TestClosingTaskCancelsActiveAttemptBeforeArchive(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := store.CreateTask(ctx, conversation.ID, "long task")
	_, _, attempt, err := store.AcceptDispatch(ctx, task.ID, "long-42", "local", "session-42")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetAttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	closing, err := store.CloseTask(ctx, task.ID)
	if err != nil || closing.Task.State != TaskClosing || closing.CancelAttempt == nil || closing.CancelAttempt.ID != attempt.ID {
		t.Fatalf("closing = %#v, err=%v", closing, err)
	}
	if _, _, err := store.CompleteAttempt(ctx, attempt.ID, ResultCanceled, "stopped"); err != nil {
		t.Fatal(err)
	}
	closed, err := store.FinishClosingTask(ctx, task.ID)
	if err != nil || closed.State != TaskClosed {
		t.Fatalf("finished close = %#v, err=%v", closed, err)
	}
}

func TestSecretaryCapabilityRotationRevokesOldToken(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := store.AuthorizeSecretaryCapability(ctx, person.ID, first)
	if err != nil || !allowed {
		t.Fatalf("first capability allowed=%v err=%v", allowed, err)
	}
	second, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil || second == first {
		t.Fatalf("second capability err=%v", err)
	}
	allowed, err = store.AuthorizeSecretaryCapability(ctx, person.ID, first)
	if err != nil || allowed {
		t.Fatalf("old capability allowed=%v err=%v", allowed, err)
	}
	allowed, err = store.AuthorizeSecretaryCapability(ctx, person.ID, second)
	if err != nil || !allowed {
		t.Fatalf("new capability allowed=%v err=%v", allowed, err)
	}
}
