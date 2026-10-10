package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

func TestSecretaryToolsMatchWorkerLifecycleContract(t *testing.T) {
	handler := Secretary{}
	tools := handler.Tools()
	got := make([]string, len(tools))
	for i, tool := range tools {
		got[i] = tool.Name
	}
	want := []string{"list_nodes", "list_projects", "list_workers", "get_worker", "spawn_worker", "message_worker", "cancel_worker", "close_worker", "acknowledge_user"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools=%#v, want %#v", got, want)
	}
}

func TestAddressedReplyContractAddsOnlyVersionedMCPToolsAndOriginFields(t *testing.T) {
	legacy := Secretary{}.Tools()
	for _, tool := range legacy {
		if tool.Name == "reply_to_user" {
			t.Fatal("legacy tool surface exposes reply_to_user")
		}
		if tool.Name == "spawn_worker" || tool.Name == "message_worker" {
			properties := tool.InputSchema["properties"].(map[string]any)
			if properties["secretary_turn_id"] != nil || properties["input_id"] != nil {
				t.Fatalf("legacy %s schema changed: %#v", tool.Name, properties)
			}
		}
	}
	versioned := (Secretary{ReplyContractVersion: core.SecretaryReplyContractAddressedV1}).Tools()
	seen := map[string]bool{}
	for _, tool := range versioned {
		seen[tool.Name] = true
		if tool.Name == "spawn_worker" || tool.Name == "message_worker" {
			properties := tool.InputSchema["properties"].(map[string]any)
			required := tool.InputSchema["required"].([]string)
			if properties["secretary_turn_id"] == nil || properties["input_id"] == nil || !reflect.DeepEqual(required[len(required)-2:], []string{"secretary_turn_id", "input_id"}) {
				t.Fatalf("versioned %s schema lacks required exact origin IDs: %#v", tool.Name, tool.InputSchema)
			}
		}
		if tool.Name == "reply_to_user" {
			required := tool.InputSchema["required"].([]string)
			if !reflect.DeepEqual(required, []string{"secretary_turn_id", "input_id", "text"}) {
				t.Fatalf("reply_to_user schema=%#v", tool.InputSchema)
			}
		}
	}
	if !seen["reply_to_user"] {
		t.Fatal("addressed-reply-v1 surface omitted reply_to_user")
	}
}

func TestSecretaryAddressedReplyRequiresCurrentServerOriginAndIsIdempotent(t *testing.T) {
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
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test", Harness: "fx", Model: "m", Reasoning: "high", ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy"}); err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "answer this")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	handler := Secretary{Workers: ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability}, ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
	toolNames := map[string]bool{}
	for _, tool := range handler.Tools() {
		toolNames[tool.Name] = true
	}
	if !toolNames["reply_to_user"] {
		t.Fatal("opt-in MCP surface omitted reply_to_user")
	}
	args, _ := json.Marshal(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "The answer is 42."})
	first, err := handler.Call(ctx, "reply_to_user", args)
	if err != nil {
		t.Fatalf("valid addressed reply rejected: %v", err)
	}
	second, err := handler.Call(ctx, "reply_to_user", args)
	if err != nil {
		t.Fatalf("exact reply replay rejected: %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) == string(secondJSON) {
		t.Fatal("exact replay was not identified as duplicate")
	}
	for _, invalid := range []map[string]string{
		{"secretary_turn_id": "stale-turn", "input_id": turn.InputID, "text": "stale"},
		{"secretary_turn_id": turn.ID, "input_id": "stale-input", "text": "stale"},
	} {
		encoded, _ := json.Marshal(invalid)
		if _, err := handler.Call(ctx, "reply_to_user", encoded); err == nil {
			t.Fatalf("invalid origin accepted: %#v", invalid)
		}
	}
	legacy := Secretary{Workers: handler.Workers}
	for _, tool := range legacy.Tools() {
		if tool.Name == "reply_to_user" {
			t.Fatal("legacy MCP surface exposed opt-in reply tool")
		}
	}
	if _, err := legacy.Call(ctx, "reply_to_user", args); err == nil {
		t.Fatal("legacy MCP accepted opt-in reply operation")
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secretaryEntries := 0
	for _, entry := range entries {
		if entry.Kind == core.EntrySecretary {
			secretaryEntries++
		}
	}
	if secretaryEntries != 1 {
		t.Fatalf("reply/replay produced %d Conversation entries, want 1", secretaryEntries)
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("reply operation created another turn: turns=%#v err=%v", turns, err)
	}
}

func TestMCPSpawnSuppressesExactResultBeforeDeliveryReceiptCommit(t *testing.T) {
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
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test", Harness: "fx", Model: "m", Reasoning: "high", ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy"}); err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "create a Worker and report separately")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "repo", Name: "Repo", Mappings: []core.ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	inventory := core.HarnessInventorySnapshot{Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{{ID: "node/fx", Node: "node", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit}}}}}
	if err := store.UpdateNodeHeartbeat(ctx, "node", inventory, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	runtime := &mcpReceiptGapRuntime{store: store}
	handler := Secretary{
		Workers: ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: runtime, WorkerProfileSource: func() (node.ManagedProfile, error) {
			return node.ManagedProfile{Version: "synthetic-worker-template-v1", Name: "worker", Content: "Synthetic fixture.", AllowTools: []string{"read"}, Hash: "synthetic-worker-template-source", Runtime: "fx"}, nil
		}},
		ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
	}
	args, err := json.Marshal(map[string]any{
		"intent": "synthetic work", "preferences": map[string]any{"project_id": project.ID, "node_id": "node", "harness_instance_id": "node/fx", "harness_kind": "fx"},
		"idempotency_key": "mcp-receipt-gap", "secretary_turn_id": turn.ID, "input_id": turn.InputID,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := handler.Call(ctx, "spawn_worker", args)
	if err != nil {
		t.Fatalf("addressed spawn failed: %v", err)
	}
	if _, ok := result.(core.WorkerDetails); !ok || !runtime.resultRecorded {
		t.Fatalf("MCP call did not traverse the receipt-gap path: result=%T runtime=%#v", result, runtime)
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, turn.ID, turn.InputID, core.SecretaryTurnSucceeded, "", "duplicate terminal echo", nil); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Kind == core.EntrySecretary && entry.Body == "duplicate terminal echo" {
			t.Fatalf("unaddressed terminal echo escaped before receipt commit: %#v", entries)
		}
	}
	if related, err := store.SecretaryOriginHasResult(ctx, turn.ID, turn.InputID); err != nil || !related {
		t.Fatalf("MCP spawn did not correlate exact Worker Result: related=%v err=%v", related, err)
	}
}

type mcpReceiptGapRuntime struct {
	store          *core.Store
	resultRecorded bool
}

func (r *mcpReceiptGapRuntime) Dispatch(ctx context.Context, _ string, _ core.Worker, _ core.Turn, attempt core.Phase4Attempt, _ core.DispatchResolution) error {
	if _, err := r.store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		return err
	}
	if _, result, _, err := r.store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "exact Worker Result before receipt"}); err != nil || result == nil {
		if err == nil {
			err = errors.New("Worker Result was not recorded")
		}
		return err
	}
	r.resultRecorded = true
	return nil
}

func (*mcpReceiptGapRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return errors.New("unexpected steering command")
}
func (*mcpReceiptGapRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	return errors.New("unexpected response command")
}
func (*mcpReceiptGapRuntime) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error {
	return errors.New("unexpected resume command")
}
func (*mcpReceiptGapRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return errors.New("unexpected cancel command")
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

func TestSecretaryAcknowledgementPrecedesWorkAndDoesNotHideFinalErrors(t *testing.T) {
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
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test", Harness: "fx", Model: "m", Reasoning: "high", ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy"}); err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "answer this")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	handler := Secretary{Workers: ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability}}
	args, _ := json.Marshal(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "Сейчас проверю"})
	if _, err := handler.Call(ctx, "acknowledge_user", args); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil || len(entries) != 1 || entries[0].Body != "Сейчас проверю" {
		t.Fatalf("ack not canonical before work: %#v %v", entries, err)
	}
	if _, err := store.RecordSecretaryToolCall(ctx, turn.ID, "spawn_worker", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSecretaryTextDelta(ctx, turn.ID, "Сейчас проверю"); err != nil {
		t.Fatal(err)
	}
	finished, entry, err := store.FinishSecretaryTurnWithResponse(ctx, turn.ID, core.SecretaryTurnSucceeded, "", "Не удалось запустить задачу: узел недоступен.")
	if err != nil || finished.State != core.SecretaryTurnSucceeded || entry.Body != "Не удалось запустить задачу: узел недоступен." {
		t.Fatalf("dispatch error hidden after ack: %#v %#v %v", finished, entry, err)
	}
	if _, err := handler.Call(ctx, "acknowledge_user", args); err != nil {
		t.Fatalf("exact replay rejected: %v", err)
	}
	entries, _ = store.EntriesAfter(ctx, conversation.ID, 0)
	if len(entries) != 2 {
		t.Fatalf("ack replay duplicated history: %#v", entries)
	}
	for _, tc := range []struct {
		status   core.SecretaryTurnState
		response string
	}{
		{core.SecretaryTurnSucceeded, "Сейчас проверю"},
		{core.SecretaryTurnFailed, ""},
	} {
		next, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "next")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.StartSecretaryTurn(ctx, next.ID); err != nil {
			t.Fatal(err)
		}
		ackArgs, _ := json.Marshal(map[string]string{"secretary_turn_id": next.ID, "input_id": next.InputID, "text": "Сейчас проверю"})
		if _, err := handler.Call(ctx, "acknowledge_user", ackArgs); err != nil {
			t.Fatal(err)
		}
		terminalError := ""
		if tc.status == core.SecretaryTurnFailed {
			terminalError = "native failure"
		}
		finished, entry, err := store.FinishSecretaryTurnWithResponse(ctx, next.ID, tc.status, terminalError, tc.response)
		if err != nil || finished.State != tc.status || entry.ID != "" {
			t.Fatalf("ack hid failure or duplicated echo: %#v %#v %v", finished, entry, err)
		}
	}
}
