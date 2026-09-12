package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestSecretaryToolsMatchWorkerLifecycleContract(t *testing.T) {
	handler := Secretary{}
	tools := handler.Tools()
	got := make([]string, len(tools))
	for i, tool := range tools {
		got[i] = tool.Name
	}
	want := []string{"list_nodes", "list_projects", "list_workers", "get_worker", "spawn_worker", "message_worker", "cancel_worker", "close_worker"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools=%#v, want %#v", got, want)
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
	handler := Secretary{Workers: ctl.WorkerService{Store: store, PersonID: person.ID, Capability: "wrong"}}
	if _, err := handler.Call(ctx, "list_workers", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected authorization error")
	}
}
