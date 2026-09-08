package ctl

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

type fakeDispatcher struct{ store *core.Store }

func (d fakeDispatcher) Dispatch(ctx context.Context, task core.Task, workerRef string) (core.WorkerBinding, core.Attempt, error) {
	_, binding, attempt, err := d.store.AcceptDispatch(ctx, task.ID, workerRef, "local", "session-1", "")
	return binding, attempt, err
}

func newServiceTest(t *testing.T) (*core.Store, Service, core.Person, core.Conversation) {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "ctl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	return store, Service{Store: store, PersonID: person.ID, Capability: capability}, person, conversation
}

func TestServiceAuthorizesTypedLifecycleAndOwnerScope(t *testing.T) {
	ctx := context.Background()
	store, service, person, conversation := newServiceTest(t)
	service.Dispatcher = fakeDispatcher{store: store}
	task, err := service.Create(ctx, "inspect repository")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != core.TaskOpen {
		t.Fatalf("created task=%#v", task)
	}
	listed, err := service.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != task.ID {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	details, err := service.Show(ctx, task.ID)
	if err != nil || details.Binding == nil || len(details.Attempts) != 1 {
		t.Fatalf("details=%#v err=%v", details, err)
	}
	closed, err := service.Close(ctx, task.ID)
	if err != nil || closed.Task.State != core.TaskClosing || closed.CancelAttempt == nil {
		t.Fatalf("close=%#v err=%v", closed, err)
	}

	_, otherConversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	otherTask, err := store.CreateTask(ctx, otherConversation.ID, "private")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Show(ctx, otherTask.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("cross-owner show err=%v", err)
	}
	_ = person
	_ = conversation
}

func TestServiceRejectsRevokedCapability(t *testing.T) {
	ctx := context.Background()
	store, service, person, _ := newServiceTest(t)
	if _, err := store.RotateSecretaryCapability(ctx, person.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("list with revoked capability err=%v", err)
	}
}

func TestServiceRetryDispatchFailure(t *testing.T) {
	ctx := context.Background()
	store, service, _, conversation := newServiceTest(t)
	task, err := store.CreateTask(ctx, conversation.ID, "retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.MarkDispatchFailed(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := service.Retry(ctx, task.ID)
	if err != nil || retried.State != core.TaskDispatching {
		t.Fatalf("retry=%#v err=%v", retried, err)
	}
}
