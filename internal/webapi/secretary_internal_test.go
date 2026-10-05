package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

type fakeSecretaryWorkerTools struct{}

type originSecretaryWorkerTools struct {
	fakeSecretaryWorkerTools
	spawnResults   []core.WorkerDetails
	messageResults []core.WorkerDetails
	spawnError     error
}

func (f *originSecretaryWorkerTools) SpawnWorker(context.Context, ctl.SpawnWorkerRequest) (core.WorkerDetails, error) {
	if f.spawnError != nil {
		return core.WorkerDetails{}, f.spawnError
	}
	if len(f.spawnResults) == 0 {
		return core.WorkerDetails{}, nil
	}
	details := f.spawnResults[0]
	f.spawnResults = f.spawnResults[1:]
	return details, nil
}

func (f *originSecretaryWorkerTools) MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	if len(f.messageResults) == 0 {
		return core.WorkerDetails{}, nil
	}
	details := f.messageResults[0]
	f.messageResults = f.messageResults[1:]
	return details, nil
}

func (fakeSecretaryWorkerTools) ListNodes(context.Context) ([]core.NodeRecord, error) {
	return []core.NodeRecord{{Node: "node-a"}}, nil
}
func (fakeSecretaryWorkerTools) ListProjects(context.Context) ([]core.Project, error) {
	return []core.Project{{ID: "project-a", Name: "Project A"}}, nil
}
func (fakeSecretaryWorkerTools) ListWorkers(context.Context) ([]core.Worker, error) {
	return nil, nil
}
func (fakeSecretaryWorkerTools) GetWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) SpawnWorker(context.Context, ctl.SpawnWorkerRequest) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) CancelWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) CloseWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}

func TestSecretaryToolCallRequiresCapabilityAndUsesAttachedRuntimeService(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(context.Background(), api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	api.AttachSecretaryWorkerTools(fakeSecretaryWorkerTools{})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	unauthorized, err := http.Post(server.URL+"/v1/internal/secretary/tools/call", "application/json", stringsReader(`{"name":"list_projects","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.StatusCode)
	}
	unauthorized.Body.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/internal/secretary/tools/call", stringsReader(`{"name":"list_projects","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized status=%d", response.StatusCode)
	}
	var result struct {
		Value []core.Project `json:"value"`
		Error string         `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Error != "" || len(result.Value) != 1 || result.Value[0].ID != "project-a" {
		t.Fatalf("result=%+v", result)
	}
}

func TestInternalMCPAddressedReplyRequiresOptInAndExactOrigin(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
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
	api.AttachSecretaryWorkerTools(fakeSecretaryWorkerTools{})
	contract := core.SecretaryReplyContractAddressedV1
	api.AttachSecretaryReplyContract(func() string { return contract })
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	call := func(args map[string]string) secretaryToolCallResponse {
		t.Helper()
		encoded, err := json.Marshal(secretaryToolCallRequest{Name: "reply_to_user", Arguments: encodeOriginArgs(t, args)})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/internal/secretary/tools/call", strings.NewReader(string(encoded)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+capability)
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result secretaryToolCallResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	args := map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "Independent answer."}
	first := call(args)
	if first.Error != "" || first.Value == nil {
		t.Fatalf("valid MCP reply failed: %#v", first)
	}
	second := call(args)
	firstValue, firstOK := first.Value.(map[string]any)
	secondValue, secondOK := second.Value.(map[string]any)
	if second.Error != "" || !firstOK || !secondOK || firstValue["entry_id"] != secondValue["entry_id"] || firstValue["duplicate"] != false || secondValue["duplicate"] != true {
		t.Fatalf("reply replay was not idempotently acknowledged: first=%#v second=%#v err=%s", first.Value, second.Value, second.Error)
	}
	conflict := call(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "different body"})
	if conflict.Error == "" {
		t.Fatal("same origin accepted a conflicting reply body")
	}
	stale := call(map[string]string{"secretary_turn_id": turn.ID, "input_id": "stale-input", "text": "stale"})
	if stale.Error == "" {
		t.Fatal("stale input id was accepted by the internal MCP route")
	}
	contract = ""
	disabled := call(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "legacy"})
	if disabled.Error == "" {
		t.Fatal("legacy config accepted the opt-in reply operation")
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
		t.Fatalf("HTTP reply replay created %d Secretary entries", secretaryEntries)
	}
}

func encodeOriginArgs(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestInternalMCPLinksOnlyAcceptedWorkerTurnsToExactSecretaryOrigin(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	personID := api.OwnerID()
	conversation, err := store.ConversationForPerson(ctx, personID)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, personID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, personID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test", Harness: "fx", Model: "m", Reasoning: "high", ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy"}); err != nil {
		t.Fatal(err)
	}
	makeOrigin := func(input string) core.SecretaryTurn {
		t.Helper()
		turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	makeWorker := func(ref string) (core.WorkerDetails, core.Phase4Attempt) {
		t.Helper()
		worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: ref, Intent: "synthetic", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic"}, core.TurnSpec{Input: "synthetic"})
		if err != nil {
			t.Fatal(err)
		}
		if turn.ID != worker.CurrentTurnID {
			t.Fatalf("worker did not return its exact current turn: worker=%#v turn=%#v", worker, turn)
		}
		return core.WorkerDetails{Worker: worker, ActionTurnID: turn.ID}, attempt
	}
	fast, fastAttempt := makeWorker("server-owned-fast")
	slow, slowAttempt := makeWorker("server-owned-slow")
	_, failedAttempt := makeWorker("server-owned-failed")
	tools := &originSecretaryWorkerTools{spawnResults: []core.WorkerDetails{fast}, messageResults: []core.WorkerDetails{slow}}
	api.AttachSecretaryWorkerTools(tools)
	api.AttachSecretaryReplyContract(func() string { return core.SecretaryReplyContractAddressedV1 })
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	call := func(name string, args map[string]any) secretaryToolCallResponse {
		t.Helper()
		arguments, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(secretaryToolCallRequest{Name: name, Arguments: arguments})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/internal/secretary/tools/call", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+capability)
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result secretaryToolCallResponse
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	origin := makeOrigin("spawn and steer two tasks")
	spawnArgs := map[string]any{"secretary_turn_id": origin.ID, "input_id": origin.InputID, "intent": "fast task", "preferences": map[string]any{}}
	if result := call("spawn_worker", spawnArgs); result.Error != "" {
		t.Fatalf("accepted spawn failed: %#v", result)
	}
	messageArgs := map[string]any{"secretary_turn_id": origin.ID, "input_id": origin.InputID, "worker_ref": slow.Worker.WorkerRef, "text": "continue"}
	if result := call("message_worker", messageArgs); result.Error != "" {
		t.Fatalf("accepted message failed: %#v", result)
	}
	for _, attempt := range []core.Phase4Attempt{fastAttempt, slowAttempt} {
		if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
			t.Fatalf("activate accepted Worker attempt %s: %v", attempt.ID, err)
		}
		if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "synthetic result"}); err != nil || result == nil {
			t.Fatalf("record linked result for %s: result=%#v err=%v", attempt.ID, result, err)
		}
	}
	if related, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID); err != nil || !related {
		t.Fatalf("accepted MCP results did not link to originating pair: related=%v err=%v", related, err)
	}
	if _, _, err := store.FinishSecretaryTurnWithResponse(ctx, origin.ID, core.SecretaryTurnSucceeded, "", ""); err != nil {
		t.Fatal(err)
	}
	unrelated := makeOrigin("unrelated user input")
	if _, err := store.SetPhase4AttemptActive(ctx, failedAttempt.ID); err != nil {
		t.Fatalf("activate unlinked failed Worker attempt %s: %v", failedAttempt.ID, err)
	}
	tools.spawnError = errors.New("synthetic dispatch failure")
	failedArgs := map[string]any{"secretary_turn_id": unrelated.ID, "input_id": unrelated.InputID, "intent": "must fail", "preferences": map[string]any{}}
	failedCall := call("spawn_worker", failedArgs)
	if failedCall.Error == "" {
		t.Fatal("failed dispatch unexpectedly returned success")
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, failedAttempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeFailed, Classification: core.OutcomeFinal, Summary: "failed Worker result"}); err != nil || result == nil {
		t.Fatalf("record failed Worker result: result=%#v err=%v", result, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, unrelated.ID, unrelated.InputID); err != nil || related {
		t.Fatalf("failed MCP dispatch created a Worker-origin link: related=%v err=%v", related, err)
	}
}

func stringsReader(value string) *strings.Reader { return strings.NewReader(value) }
