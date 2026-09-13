package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "ticket11.test", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: map[string]any{"task": "task-do-not-return", "task_id": "task-do-not-return", "nested": map[string]any{"callback": "callback-do-not-return", "token": "token-do-not-return", "safe": "kept"}}}); err != nil {
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
	for _, secret := range []string{"do-not-return", `"task"`, "task_id", "runtime_session_id", "callback", "token-do-not-return", "credential_hash", "credential_secret"} {
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

func TestTicket11ControlRoomRendersInventoryFieldsAndExportAction(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "web", "control-room", "src", "App.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, marker := range []string{"instance.capabilities", "instance.reasoning_levels", "instance.authentication", "worker.turns", "/v1/control/export", "downloadExport"} {
		if !strings.Contains(source, marker) {
			t.Fatalf("Control Room omitted %q", marker)
		}
	}
}

func TestTicket11RedactedConfigAndProfileWritesFailClosedAndUseRevision(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	profileRevision := "profile-rev-1"
	configApplyCalls := 0
	profileApplyCalls := 0
	api.AttachControl(ControlOptions{
		ConfigContent: func() (string, error) { return "api_key = super-secret\\nname = safe\\n", nil },
		ApplyConfig: func(content []byte) (any, error) {
			configApplyCalls++
			return map[string]string{"status": "applied"}, nil
		},
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{{Name: "worker", Content: "token = opaque-secret\\nname = safe\\n", Hash: profileRevision}}, nil
		},
		ApplyProfile: func(string, []byte) error {
			profileApplyCalls++
			return nil
		},
	})

	response, err := client.Get(server.URL + "/v1/control/config")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if revision, ok := config["revision"].(string); !ok || revision == "" || config["editable"] != false {
		t.Fatalf("config write contract missing revision/editable: %#v", config)
	}
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/v1/control/config", strings.NewReader(`{"content":"[redacted]\\nname = changed","expected_revision":"config-rev-1"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || configApplyCalls != 0 {
		t.Fatalf("redacted config was accepted status=%d calls=%d", response.StatusCode, configApplyCalls)
	}

	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/control/profiles/worker", strings.NewReader(`{"content":"[redacted]\\nname = changed","expected_revision":"profile-rev-1"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || profileApplyCalls != 0 {
		t.Fatalf("redacted profile was accepted status=%d calls=%d", response.StatusCode, profileApplyCalls)
	}

	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/control/profiles/worker", strings.NewReader(`{"content":"name = changed","expected_revision":"stale"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict || profileApplyCalls != 0 {
		t.Fatalf("stale profile revision was accepted status=%d calls=%d", response.StatusCode, profileApplyCalls)
	}
}

func TestTicket11SanitizerRedactsArbitraryNestedDiagnosticPayload(t *testing.T) {
	value := sanitizeControlAny(map[string]any{
		"safe":    "kept",
		"content": map[string]any{"safe": "kept", "secret_value": "secret-do-not-return"},
		"skills":  []any{map[string]any{"name": "skill", "callback": "callback-do-not-return", "native_id": "native-do-not-return"}},
		"task":    map[string]any{"id": "task-do-not-return", "text": "task text"},
		"taskId":  "task-id-do-not-return",
		"nested":  []any{map[string]any{"opaque": "api_key=secret-do-not-return", "nativeSessionId": "native-session-do-not-return"}},
	})
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"secret-do-not-return", "callback-do-not-return", "native-do-not-return", "task-do-not-return", "task-id-do-not-return", "native-session-do-not-return", `"task"`, `"taskId"`, `"native_id"`} {
		if strings.Contains(text, secret) {
			t.Fatalf("sanitizer leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "safe") {
		t.Fatalf("sanitizer removed safe diagnostic content: %s", text)
	}
	detail, ok := parseHarnessDiagnostic([]byte(`{"jsonrpc":"2.0","params":{"update":{"title":"native-tool-do-not-return","status":"callback-do-not-return","sessionUpdate":"tool_call","content":{"token":"secret-do-not-return"}}}}`), 1)
	if !ok {
		t.Fatal("raw harness diagnostic was discarded")
	}
	detailText, _ := json.Marshal(detail)
	for _, secret := range []string{"native-tool-do-not-return", "callback-do-not-return", "secret-do-not-return"} {
		if strings.Contains(string(detailText), secret) {
			t.Fatalf("raw diagnostic leaked %q: %s", secret, detailText)
		}
	}
}

func TestTicket11ExportIsAuthorizedRedactedDownload(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	response, err := client.Get(server.URL + "/v1/control/export")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized export status=%d", response.StatusCode)
	}
	controlRoomLogin(t, client, server.URL)
	response, err = client.Get(server.URL + "/v1/control/export")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(response.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export download headers status=%d disposition=%q", response.StatusCode, response.Header.Get("Content-Disposition"))
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

func TestTicket11RawCoTMarkersInAllowedValuesRedactedAcrossControlSurfaces(t *testing.T) {
	store, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	ctx := context.Background()
	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "worker-cot", Title: "safe worker", Intent: "safe intent", ProjectID: "project", NodeID: "node", HarnessInstanceID: "harness", PolicySnapshot: "safe policy"}, core.TurnSpec{Input: "safe input"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(ctx, attempt.ID, core.FinishAttemptInput{AttemptOutcomeInput: core.AttemptOutcomeInput{
		Status:         core.OutcomeFailed,
		Classification: core.OutcomeFinal,
		ErrorMessage:   "reasoning: COT-REASONING",
		Diagnostics:    `{"message":"thought: COT-THOUGHT","summary":"safe summary"}`,
		Summary:        "safe summary",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "ticket11.cot", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: map[string]any{
		"message": "analysis: COT-ANALYSIS",
		"summary": "safe summary",
		"markup":  "<think>COT-THINK</think>",
		"trace":   "chain-of-thought: COT-CHAIN",
	}}); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	log := `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"safe tool","status":"thought: COT-RAW-THOUGHT","details":"<think>COT-RAW-THINK</think>"}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "worker-cot.jsonl"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	api.AttachDiagnosticLogDir(logDir)

	for _, endpoint := range []string{
		"/v1/control/overview",
		"/v1/control/events",
		"/v1/control/diagnostics/worker-cot",
		"/v1/control/export",
	} {
		response, requestErr := client.Get(server.URL + endpoint)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, response.StatusCode, body)
		}
		text := string(body)
		for _, marker := range []string{"COT-ANALYSIS", "COT-REASONING", "COT-THOUGHT", "COT-THINK", "COT-CHAIN", "COT-RAW-THOUGHT", "COT-RAW-THINK"} {
			if strings.Contains(text, marker) {
				t.Fatalf("%s leaked %q: %s", endpoint, marker, text)
			}
		}
		if !strings.Contains(text, "safe summary") {
			t.Fatalf("%s removed ordinary safe summary: %s", endpoint, text)
		}
	}
}

func nowForTest() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
