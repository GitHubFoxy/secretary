package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestSecretaryToolCallUsesCapabilityAndCreatesEvents(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	handler := AuditedHandler{
		Handler: Secretary{Service: ctl.Service{Store: store, PersonID: person.ID, Capability: capability}},
		Store:   store,
		Role:    "secretary",
	}
	value, err := handler.Call(ctx, "delegate_task", json.RawMessage(`{"text":"inspect"}`))
	if err != nil {
		t.Fatal(err)
	}
	task, ok := value.(core.Task)
	if !ok || task.ConversationID != conversation.ID || task.State != core.TaskDispatching {
		t.Fatalf("value=%#v", value)
	}
	events, err := store.EventsAfter(ctx, timeZero(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Kind != "mcp.tool_call" || events[1].Kind != "mcp.tool_result" {
		t.Fatalf("events=%#v", events)
	}
}

func TestSecretaryToolCallRejectsWrongCapability(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	handler := Secretary{Service: ctl.Service{Store: store, PersonID: person.ID, Capability: "wrong"}}
	if _, err := handler.Call(ctx, "list_tasks", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected authorization error")
	}
}

func timeZero() time.Time { return time.Time{} }
