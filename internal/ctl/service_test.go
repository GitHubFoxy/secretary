package ctl

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

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
	task, err := store.CreateTask(ctx, conversation.ID, "inspect repository")
	if err != nil { t.Fatal(err) }
	if _, _, _, err := store.AcceptDispatch(ctx, task.ID, task.ID, "local", "session-1", ""); err != nil {
		t.Fatal(err)
	}
	listed, err := service.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != task.ID {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	details, err := service.Show(ctx, task.ID)
	if err != nil || details.Binding == nil || len(details.Attempts) != 1 {
		t.Fatalf("details=%#v err=%v", details, err)
	}
	if _, err := service.Close(ctx, task.ID); !errors.Is(err, core.ErrLegacyTaskReadOnly) {
		t.Fatalf("legacy Task close unexpectedly mutates Node execution: %v", err)
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

func TestServiceLegacyMutationsRejectedBeforeStoreOnBothMigrationStates(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		name := "migration-incomplete"
		if migrated { name = "migration-complete" }
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "ctl.db")
			store, err := core.Open(ctx, path)
			if err != nil { t.Fatal(err) }
			defer store.Close()
			owner, conversation, err := store.CreatePersonWithConversation(ctx)
			if err != nil { t.Fatal(err) }
			capability, err := store.RotateSecretaryCapability(ctx, owner.ID)
			if err != nil { t.Fatal(err) }
			service := Service{Store: store, PersonID: owner.ID, Capability: capability}
			retryTask, err := store.CreateTask(ctx, conversation.ID, "failed dispatch")
			if err != nil { t.Fatal(err) }
			if _, err := store.MarkDispatchFailed(ctx, retryTask.ID); err != nil { t.Fatal(err) }
			openTask, err := store.CreateTask(ctx, conversation.ID, "active execution")
			if err != nil { t.Fatal(err) }
			if _, _, _, err := store.AcceptDispatch(ctx, openTask.ID, "legacy-bound", "local", "native-session", t.TempDir()); err != nil { t.Fatal(err) }
			if migrated {
				db, err := sql.Open("sqlite", path)
				if err != nil { t.Fatal(err) }
				_, err = db.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES('phase4.migration_complete', '1') ON CONFLICT(key) DO UPDATE SET value = excluded.value")
				closeErr := db.Close()
				if err != nil { t.Fatal(err) }
				if closeErr != nil { t.Fatal(closeErr) }
			}
			if got := store.LegacyTasksReadOnly(ctx); got != migrated { t.Fatalf("expected migration_complete=%v, got readOnly=%v", migrated, got) }
			before, err := service.List(ctx)
			if err != nil || len(before) != 2 { t.Fatalf("tasks before=%#v err=%v", before, err) }
			sequenceBefore, err := store.MaxEventSeq(ctx)
			if err != nil { t.Fatal(err) }
			if _, err := service.Create(ctx, "unrouted new Task"); !errors.Is(err, core.ErrLegacyTaskReadOnly) { t.Fatalf("legacy create: %v", err) }
			if _, err := service.Retry(ctx, retryTask.ID); !errors.Is(err, core.ErrLegacyTaskReadOnly) { t.Fatalf("legacy retry: %v", err) }
			if _, err := service.Close(ctx, openTask.ID); !errors.Is(err, core.ErrLegacyTaskReadOnly) { t.Fatalf("legacy close: %v", err) }
			after, err := service.List(ctx)
			if err != nil || len(after) != len(before) { t.Fatalf("tasks after=%#v err=%v", after, err) }
			sequenceAfter, err := store.MaxEventSeq(ctx)
			if err != nil || sequenceAfter != sequenceBefore {
				t.Fatalf("legacy command created an event: before=%d after=%d err=%v", sequenceBefore, sequenceAfter, err)
			}
			for _, original := range before {
				current, err := store.Task(ctx, original.ID)
				if err != nil || current.State != original.State || !current.UpdatedAt.Equal(original.UpdatedAt) {
					t.Fatalf("Task mutated: before=%#v after=%#v err=%v", original, current, err)
				}
			}
			details, err := service.Show(ctx, openTask.ID)
			if err != nil || details.Binding == nil || details.Binding.WorkerRef != "legacy-bound" ||
				len(details.Attempts) != 1 || details.Attempts[0].State != core.AttemptStarting {
				t.Fatalf("legacy Worker binding or native Attempt changed: %#v err=%v", details, err)
			}
		})
	}
}
