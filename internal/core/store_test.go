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

func TestEntriesAfterReturnsEmptySliceForEmptyConversation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("entries = %#v", entries)
	}
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
	acceptedTask, _, attempt, err := store.AcceptDispatch(ctx, task.ID, "phone-42", "local", "session-42", t.TempDir())
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

func TestRecordConfigChangeStoresVersionAndEvent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.RecordConfigVersion(ctx, "cfg-1", "/config", `{}`); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordConfigChange(ctx, "cfg-1", "cfg-2", "/config", `{"version":"cfg-2"}`, `{"models":{"smart":"new"}}`); err != nil {
		t.Fatal(err)
	}
	var previous, next, diff string
	if err := store.db.QueryRowContext(ctx, `SELECT previous_version, next_version, diff_json FROM config_events`).Scan(&previous, &next, &diff); err != nil {
		t.Fatal(err)
	}
	if previous != "cfg-1" || next != "cfg-2" || diff != `{"models":{"smart":"new"}}` {
		t.Fatalf("event = %q %q %q", previous, next, diff)
	}
}

func TestSettingsAreDurableAndUpdatable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	value, found, err := store.GetSetting(ctx, "secretary.model")
	if err != nil || found || value != "" {
		t.Fatalf("initial setting value=%q found=%v err=%v", value, found, err)
	}
	if err := store.SetSetting(ctx, "secretary.model", "smart"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(ctx, "secretary.model", "fast"); err != nil {
		t.Fatal(err)
	}
	value, found, err = store.GetSetting(ctx, "secretary.model")
	if err != nil || !found || value != "fast" {
		t.Fatalf("updated setting value=%q found=%v err=%v", value, found, err)
	}
}

func TestRecordConfigVersionIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.RecordConfigVersion(ctx, "cfg-1", "/tmp/config.toml", `{"version":"cfg-1"}`); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordConfigVersion(ctx, "cfg-1", "/changed", `{"changed":true}`); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := store.db.QueryRowContext(ctx, `SELECT source_path FROM config_versions WHERE version = ?`, "cfg-1").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/config.toml" {
		t.Fatalf("source path = %q", path)
	}
}

func TestWorkerBindingKeepsImmutableProfile(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "profile task")
	if err != nil {
		t.Fatal(err)
	}
	profile := BindingProfile{Version: "cfg-1", Name: "worker", Hash: "sha256", Runtime: "opencode", Model: "smart", Reasoning: "high", Tools: "bash,read"}
	_, binding, _, err := store.AcceptDispatchWithProfile(ctx, task.ID, "worker-1", "local", "session-1", t.TempDir(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Profile != profile {
		t.Fatalf("binding profile = %#v", binding.Profile)
	}
	details, err := store.TaskDetails(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if details.Binding == nil || details.Binding.Profile != profile {
		t.Fatalf("stored profile = %#v", details.Binding)
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
	_, _, attempt, err := store.AcceptDispatch(ctx, task.ID, "long-42", "local", "session-42", t.TempDir())
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
