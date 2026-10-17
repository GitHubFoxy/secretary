package webapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/mcp"
)

func TestMemoryMCPRemoteRequiresExplicitActiveOwnerRequest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := core.Open(ctx, filepath.Join(dir, "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachUserDocument(filepath.Join(dir, "user.md"))
	api.AttachSecretaryWorkerTools(fakeSecretaryWorkerTools{})
	capability, err := store.RotateSecretaryCapability(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, api.OwnerID(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "v1", Harness: "fx", Model: "m", Reasoning: "high",
		ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy",
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	remote := mcp.RemoteSecretary{BaseURL: server.URL, Capability: capability}
	call := func(name string, args any) (any, error) {
		t.Helper()
		encoded, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		return remote.Call(ctx, name, encoded)
	}
	initial, err := call("read_memory", map[string]any{"kind": "preferences"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(initial)
	var doc core.UserDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ordinary, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Tell me about preferences")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, ordinary.ID); err != nil {
		t.Fatal(err)
	}
	memoryArgs := func(turn core.SecretaryTurn) map[string]any {
		return map[string]any{
			"kind": "preferences", "entry": "Always answer concisely",
			"expected_revision": doc.Revision,
			"secretary_turn_id": turn.ID, "input_id": turn.InputID,
		}
	}
	if _, err := call("remember", memoryArgs(ordinary)); err == nil {
		t.Fatal("normal user question authorized memory write")
	}
	if _, err := store.FinishSecretaryTurn(ctx, ordinary.ID, core.SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	explicit, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Remember that I prefer short answers")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, explicit.ID); err != nil {
		t.Fatal(err)
	}
	args := memoryArgs(explicit)
	args["input_id"] = "forged-input"
	if _, err := call("remember", args); err == nil {
		t.Fatal("forged input authorized memory write")
	}
	args = memoryArgs(explicit)
	if _, err := call("remember", args); err != nil {
		t.Fatalf("explicit owner write failed: %v", err)
	}
	after, err := call("read_memory", map[string]any{"kind": "preferences"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(after)
	var updated core.UserDocument
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Revision != doc.Revision+1 || updated.Content != "- Always answer concisely\n" {
		t.Fatalf("remote memory mismatch: %+v", updated)
	}
	if _, err := call("remember", args); err == nil {
		t.Fatal("replayed write silently appended a duplicate")
	}
}
