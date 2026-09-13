package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func controlRoomTestAPI(t *testing.T) (*core.Store, *Server, *httptest.Server, *http.Client) {
	t.Helper()
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/", api.Handler())
	mux.Handle("/v1/control/", api.ControlHandler())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return store, api, server, &http.Client{}
}

func controlRoomLogin(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	response, err := client.Post(baseURL+"/v1/web/session", "application/json", bytes.NewBufferString(`{"bootstrap_token":"bootstrap"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login status=%d", response.StatusCode)
	}
}

func TestTicket11ControlRoomIsDebugGatedAndUsesOwnerSession(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	response, err := client.Get(server.URL + "/v1/control/overview")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("without --debug status=%d", response.StatusCode)
	}

	api.SetDebug(true)
	response, err = client.Get(server.URL + "/v1/control/overview")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without owner session status=%d", response.StatusCode)
	}

	controlRoomLogin(t, client, server.URL)
	response, err = client.Get(server.URL + "/v1/control/overview")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized debug status=%d", response.StatusCode)
	}
}

func TestTicket11OverviewAndDiagnosticsUseSafeDTOs(t *testing.T) {
	store, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	ctx := context.Background()

	inventory := core.HarnessInventorySnapshot{Node: "node-a", ObservedAt: nowForTest(), Instances: []core.HarnessInstance{{ID: "node-a/codex", Node: "node-a", Kind: core.HarnessCodex, Version: "1.2.3", Authentication: core.HarnessAuthentication{Authenticated: true, Method: "credential"}, Status: core.HarnessReady, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}, Activity: []core.ActivityCapability{core.ActivityStatus}}, ModelIDs: []core.ObservedModelID{"model-a"}, ReasoningLevels: []core.ObservedReasoningLevel{"high"}}}}
	if _, err := store.EnrollNode(ctx, "node-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, "node-a", inventory); err != nil {
		t.Fatal(err)
	}
	secondInventory := inventory
	secondInventory.Node = "node-b"
	secondInventory.Instances = []core.HarnessInstance{{ID: "node-b/fx", Node: "node-b", Kind: core.HarnessFX, Version: "2.0.0", Authentication: core.HarnessAuthentication{Authenticated: true, Method: "local"}, Status: core.HarnessReady, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}}, ModelIDs: []core.ObservedModelID{"model-b"}, ReasoningLevels: []core.ObservedReasoningLevel{"default"}}}
	if _, err := store.EnrollNode(ctx, "node-b"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, "node-b", secondInventory); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetNodeDraining(ctx, "node-a", true); err != nil {
		t.Fatal(err)
	}
	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "worker-a", Title: "safe title", Intent: "safe intent", ProjectID: "project-a", NodeID: "node-a", HarnessInstanceID: "node-a/codex", PolicySnapshot: `{"credential":"do-not-return"}`}, core.TurnSpec{Input: "user input token=do-not-return"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(ctx, attempt.ID, core.FinishAttemptInput{AttemptOutcomeInput: core.AttemptOutcomeInput{Status: core.OutcomeFailed, Classification: core.OutcomeFinal, ErrorCode: "credential=do-not-return", ErrorMessage: "callback do-not-return", Diagnostics: `{"nested":{"runtime_session_id":"native-session-do-not-return","api_key":"do-not-return"}}`, Summary: "failed safely"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "ticket11.test", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: map[string]any{"task_id": "task-do-not-return", "nested": map[string]any{"callback": "callback-do-not-return", "token": "token-do-not-return", "safe": "kept"}}}); err != nil {
		t.Fatal(err)
	}
	_ = turn
	logDir := t.TempDir()
	log := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"native-session-do-not-return","update":{"sessionUpdate":"tool_call","title":"shell","rawInput":{"command":"printf safe","token":"token-do-not-return"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "worker-a.jsonl"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	api.AttachDiagnosticLogDir(logDir)

	response, err := client.Get(server.URL + "/v1/control/overview")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("overview status=%d body=%#v", response.StatusCode, body)
	}
	encoded, _ := json.Marshal(body)
	text := string(encoded)
	for _, secret := range []string{"do-not-return", "task_id", "runtime_session_id", "callback", "token-do-not-return", "credential_hash", "credential_secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("overview leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "node-a/codex") || !strings.Contains(text, "node-b/fx") || !strings.Contains(text, "worker-a") || !strings.Contains(text, "failed safely") {
		t.Fatalf("overview omitted diagnostics state: %s", text)
	}

	response, err = client.Get(server.URL + "/v1/control/diagnostics/worker-a")
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics map[string]any
	if err := json.NewDecoder(response.Body).Decode(&diagnostics); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	encoded, _ = json.Marshal(diagnostics)
	text = string(encoded)
	for _, secret := range []string{"do-not-return", "native-session-do-not-return", "task_id", "runtime_session_id", "callback", "token-do-not-return", "credential_hash", "credential_secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostics leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "attempt_outcomes") {
		t.Fatalf("diagnostics missing outcomes: %s", text)
	}
}

func TestTicket11ControlRoomRevokeRoutesAreAuditedAndPublicRoutesStaySeparate(t *testing.T) {
	store, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	ctx := context.Background()
	if err := store.ConfigureNodePairingToken(ctx, "pairing-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNodeWithPairing(ctx, "pairing-token", "node-a", bytes.Repeat([]byte{'n'}, 32)); err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, api.OwnerID(), "device-a", "Device A", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApproveClient(ctx, pairing.ID); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/v1/control/clients/" + pairing.ID + "/revoke", "/v1/control/nodes/node-a/revoke"} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+path, nil)
		request.Header.Set("Idempotency-Key", "ticket11-"+path)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("revoke %s status=%d", path, response.StatusCode)
		}
	}

	events, err := store.EventsRecent(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	var clientAudit, nodeAudit bool
	for _, event := range events {
		clientAudit = clientAudit || event.Kind == "client.revoked"
		nodeAudit = nodeAudit || event.Kind == "control.node_revoked"
	}
	if !clientAudit || !nodeAudit {
		t.Fatalf("revoke audit missing client=%v node=%v events=%#v", clientAudit, nodeAudit, events)
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "/v1/control/overview", nil)
	publicRecorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(publicRecorder, publicRequest)
	if publicRecorder.Code != http.StatusNotFound {
		t.Fatalf("public API unexpectedly served Control Room status=%d", publicRecorder.Code)
	}
}

func TestTicket11InvalidConfigKeepsActiveSnapshot(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	active := map[string]any{"version": "active-1", "model": "safe"}
	api.AttachControl(ControlOptions{
		ConfigContent:  func() (string, error) { return "active", nil },
		ConfigSnapshot: func() any { return active },
		ApplyConfig: func(content []byte) (any, error) {
			if string(content) == "invalid" {
				return nil, errors.New("invalid config")
			}
			active = map[string]any{"version": "active-2"}
			return active, nil
		},
	})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/v1/control/config", strings.NewReader(`{"content":"invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid config status=%d", response.StatusCode)
	}
	if active["version"] != "active-1" {
		t.Fatalf("active snapshot changed after invalid input: %#v", active)
	}
}

func nowForTest() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
