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

func TestTicket11SafeConfigAndProfileFieldsRemainVisibleWhileOpaqueValuesStayRedacted(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	api.AttachControl(ControlOptions{
		ConfigContent: func() (string, error) {
			return "skills = [\"safe-skill\"]\nreasoning = high\nmodel = safe-model\nruntime = fx\nname = safe-name\ncontent = ordinary product content\nnotes = \"api_key=embedded-secret\"\napi_key = config-secret\nauth = auth-secret\nopaque_value = opaque-secret\ncot_payload = cot-secret\ncallback = callback-secret\nnative_session_id = native-secret\ntask_id = task-secret\n", nil
		},
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{{Name: "worker", Content: "name: safe-worker\nmodel: safe-model\nreasoning: high\nruntime: fx\ncontent: ordinary profile content\ncredential: profile-secret\npassword: password-secret\n", Hash: "profile-rev"}}, nil
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d body=%#v", response.StatusCode, config)
	}
	configText, _ := config["content"].(string)
	for _, safe := range []string{"skills = [\"safe-skill\"]", "reasoning = high", "model = safe-model", "runtime = fx", "name = safe-name", "content = ordinary product content"} {
		if !strings.Contains(configText, safe) {
			t.Fatalf("config removed safe field %q: %q", safe, configText)
		}
	}
	for _, secret := range []string{"config-secret", "embedded-secret", "auth-secret", "opaque-secret", "cot-secret", "callback-secret", "native-secret", "task-secret"} {
		if strings.Contains(configText, secret) {
			t.Fatalf("config leaked %q: %q", secret, configText)
		}
	}

	response, err = client.Get(server.URL + "/v1/control/profiles")
	if err != nil {
		t.Fatal(err)
	}
	var profiles []ProfileFile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(profiles) != 1 {
		t.Fatalf("profiles status=%d body=%#v", response.StatusCode, profiles)
	}
	profileText := profiles[0].Content
	for _, safe := range []string{"name: safe-worker", "model: safe-model", "reasoning: high", "runtime: fx", "content: ordinary profile content"} {
		if !strings.Contains(profileText, safe) {
			t.Fatalf("profile removed safe field %q: %q", safe, profileText)
		}
	}
	for _, secret := range []string{"profile-secret", "password-secret"} {
		if strings.Contains(profileText, secret) {
			t.Fatalf("profile leaked %q: %q", secret, profileText)
		}
	}
}

func TestTicket11ProfileMetadataIsRedactedAcrossFreshReplayAndExport(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	hostileRuntime := "sk-runtime-secret ghp_runtime xoxb_runtime"
	hostileModel := "token: model-token"
	hostileReasoning := "native-session callback task <think>COT-secret</think>"
	api.AttachControl(ControlOptions{
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{
				{Name: "worker", Path: "/profiles/worker.md", Content: "ordinary profile", Hash: "profile-rev", Runtime: hostileRuntime, Model: hostileModel, Reasoning: hostileReasoning},
				{Name: "secretary", Path: "/profiles/secretary.md", Content: "ordinary secretary", Hash: "secretary-rev", Runtime: "codex", Model: "smart-model", Reasoning: "high"},
			}, nil
		},
		ConfigSnapshot: func() any {
			return map[string]any{"profiles": map[string]any{
				"worker":    map[string]string{"runtime": hostileRuntime, "model": hostileModel, "reasoning": hostileReasoning},
				"secretary": map[string]string{"runtime": "codex", "model": "smart-model", "reasoning": "high"},
			}}
		},
	})

	for _, endpoint := range []string{"/v1/control/profiles", "/v1/control/profiles"} {
		response, err := client.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		var profiles []ProfileFile
		if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
			response.Body.Close()
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || len(profiles) != 2 {
			t.Fatalf("%s status=%d profiles=%#v", endpoint, response.StatusCode, profiles)
		}
		if profiles[0].Runtime != "[redacted]" || profiles[0].Model != "[redacted]" || profiles[0].Reasoning != "[redacted]" {
			t.Fatalf("hostile profile metadata was not redacted: %#v", profiles[0])
		}
		if profiles[0].Content != "ordinary profile" || profiles[1].Runtime != "codex" || profiles[1].Model != "smart-model" || profiles[1].Reasoning != "high" {
			t.Fatalf("ordinary profile data changed: %#v", profiles)
		}
	}

	response, err := client.Get(server.URL + "/v1/control/export")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.StatusCode, body)
	}
	text := string(body)
	for _, secret := range []string{hostileRuntime, hostileModel, hostileReasoning, "sk-runtime-secret", "ghp_runtime", "xoxb_runtime", "model-token", "COT-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("export leaked hostile profile metadata %q: %s", secret, text)
		}
	}
	for _, safe := range []string{"codex", "smart-model", "high"} {
		if !strings.Contains(text, safe) {
			t.Fatalf("export removed ordinary profile metadata %q: %s", safe, text)
		}
	}
}

func TestTicket11SafeProductConfigIsEditableAndCASApplied(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	content := "skills = []\nreasoning = high\nmodel = safe-model\nruntime = fx\nname = safe-name\ncontent = ordinary product content\n"
	var applied string
	api.AttachControl(ControlOptions{
		RequireExpectedRevision: true,
		ConfigContent:           func() (string, error) { return content, nil },
		ApplyConfig: func(next []byte) (any, error) {
			applied = string(next)
			content = applied
			return map[string]string{"status": "applied"}, nil
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
	if config["editable"] != true {
		t.Fatalf("safe product config was made readonly: %#v", config)
	}
	revision, _ := config["revision"].(string)
	payload, _ := json.Marshal(map[string]string{"content": "skills = []\nreasoning = low\nmodel = safe-model\nruntime = fx\nname = changed\ncontent = ordinary product content", "expected_revision": revision})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/v1/control/config", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(applied, "name = changed") || !strings.Contains(applied, "reasoning = low") {
		t.Fatalf("safe config was not applied with CAS status=%d content=%q", response.StatusCode, applied)
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
		Diagnostics:    `{"message":"thought: COT-THOUGHT","safe_value":"xoxb_ABC","summary":"safe summary"}`,
		Summary:        "safe summary",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "ticket11.cot", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: map[string]any{
		"message":    "analysis: COT-ANALYSIS",
		"summary":    "safe summary",
		"safe_value": "xoxb_ABC",
		"markup":     "<think>COT-THINK</think>",
		"trace":      "chain-of-thought: COT-CHAIN",
	}}); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	log := `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","title":"safe tool","status":"xoxb_ABC","details":"<think>COT-RAW-THINK</think>"}}}` + "\n"
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
		for _, marker := range []string{"COT-ANALYSIS", "COT-REASONING", "COT-THOUGHT", "COT-THINK", "COT-CHAIN", "COT-RAW-THOUGHT", "COT-RAW-THINK", "xoxb_ABC"} {
			if strings.Contains(text, marker) {
				t.Fatalf("%s leaked %q: %s", endpoint, marker, text)
			}
		}
		if !strings.Contains(text, "safe summary") {
			t.Fatalf("%s removed ordinary safe summary: %s", endpoint, text)
		}
	}
}

func TestTicket11CredentialValuesRedactedUnderSafeKeysAndMarkdown(t *testing.T) {
	_, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)

	configContent := `skills = "sk-config"
reasoning = "ghp_config"
content = "xoxb-config"
ordinary_underscore = "xoxb_ABC"
notes = "token: config-token"
description = "secret: config-secret"
summary = "credential: config-credential"
details = "token=config-equals"
ordinary = "keep-config"
`
	profileContent := `# Worker profile
skills: ordinary-skill
reasoning: high
content: ordinary profile content

This Markdown contains xoxb_ABC.
Use token: profile-token in this Markdown.
Also secret: profile-secret and credential: profile-credential.
`
	var appliedConfig, appliedProfile string
	configApplyCalls, profileApplyCalls := 0, 0
	api.AttachControl(ControlOptions{
		RequireExpectedRevision: true,
		ConfigContent:           func() (string, error) { return configContent, nil },
		ApplyConfig: func(content []byte) (any, error) {
			configApplyCalls++
			appliedConfig = string(content)
			return map[string]string{"status": "applied"}, nil
		},
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{{Name: "worker", Content: profileContent, Hash: "profile-redaction-rev"}}, nil
		},
		ApplyProfile: func(_ string, content []byte) error {
			profileApplyCalls++
			appliedProfile = string(content)
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d body=%#v", response.StatusCode, config)
	}
	configText, _ := config["content"].(string)
	for _, secret := range []string{"sk-config", "ghp_config", "xoxb-config", "xoxb_ABC", "config-token", "config-secret", "config-credential", "config-equals"} {
		if strings.Contains(configText, secret) {
			t.Fatalf("config leaked %q: %q", secret, configText)
		}
	}
	for _, safe := range []string{`ordinary = "keep-config"`} {
		if !strings.Contains(configText, safe) {
			t.Fatalf("config removed ordinary value %q: %q", safe, configText)
		}
	}
	if config["editable"] != false {
		t.Fatalf("redacted config remained editable: %#v", config)
	}
	configRevision, _ := config["revision"].(string)

	response, err = client.Get(server.URL + "/v1/control/profiles")
	if err != nil {
		t.Fatal(err)
	}
	var profiles []ProfileFile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(profiles) != 1 {
		t.Fatalf("profiles status=%d body=%#v", response.StatusCode, profiles)
	}
	profileText := profiles[0].Content
	if profiles[0].Editable {
		t.Fatalf("redacted profile remained editable: %#v", profiles[0])
	}
	for _, secret := range []string{"xoxb_ABC", "profile-token", "profile-secret", "profile-credential"} {
		if strings.Contains(profileText, secret) {
			t.Fatalf("profile leaked %q: %q", secret, profileText)
		}
	}
	for _, safe := range []string{"# Worker profile", "skills: ordinary-skill", "reasoning: high", "content: ordinary profile content"} {
		if !strings.Contains(profileText, safe) {
			t.Fatalf("profile removed ordinary Markdown %q: %q", safe, profileText)
		}
	}
	profileRevision := profiles[0].Revision

	configPayload, _ := json.Marshal(map[string]string{
		"content": `safe_key = "xoxb_write"
ordinary = "keep"
`,
		"expected_revision": configRevision,
	})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/v1/control/config", bytes.NewReader(configPayload))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || configApplyCalls != 0 {
		t.Fatalf("credential config write was accepted status=%d calls=%d", response.StatusCode, configApplyCalls)
	}

	profilePayload, _ := json.Marshal(map[string]string{
		"content":           "# profile\nThis Markdown contains xoxb_write\nordinary: kept\n",
		"expected_revision": profileRevision,
	})
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/control/profiles/worker", bytes.NewReader(profilePayload))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || profileApplyCalls != 0 {
		t.Fatalf("credential Markdown write was accepted status=%d calls=%d", response.StatusCode, profileApplyCalls)
	}

	ordinaryProfile := "# profile\nskills: coding\nreasoning: high\ncontent: ordinary editable Markdown\n"
	profilePayload, _ = json.Marshal(map[string]string{
		"content":           ordinaryProfile,
		"expected_revision": profileRevision,
	})
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/control/profiles/worker", bytes.NewReader(profilePayload))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || profileApplyCalls != 1 || appliedProfile != ordinaryProfile {
		t.Fatalf("ordinary Markdown was not editable status=%d calls=%d content=%q", response.StatusCode, profileApplyCalls, appliedProfile)
	}
	_ = appliedConfig
}

func TestTicket11ColonSensitiveValuesAreRedactedAcrossControlSurfaces(t *testing.T) {
	store, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	ctx := context.Background()

	configContent := `skills = "api_key: config-api-key-colon"
reasoning = "password: config-password-colon"
content = "callback: config-callback-colon"
notes = "auth: config-auth-colon"
metadata = "apikey: config-apikey-colon token: config-token-colon secret: config-secret-colon credential: config-credential-colon session_id: config-session-colon native_id: config-native-colon task_id: config-task-colon"
ordinary = "keep-config"
`
	profileContent := `# Worker profile
skills: ordinary-skill
reasoning: high
content: ordinary profile content

This Markdown contains api_key: profile-api-key-colon.
Also password: profile-password-colon and callback: profile-callback-colon.
More metadata: apikey: profile-apikey-colon token: profile-token-colon secret: profile-secret-colon credential: profile-credential-colon session_id: profile-session-colon native_id: profile-native-colon task_id: profile-task-colon.
`
	configMetadata := map[string]string{
		"runtime":   "api_key: metadata-api-key-colon",
		"model":     "password: metadata-password-colon",
		"reasoning": "callback: metadata-callback-colon",
	}
	var configApplyCalls, profileApplyCalls int
	api.AttachControl(ControlOptions{
		RequireExpectedRevision: true,
		ConfigContent:           func() (string, error) { return configContent, nil },
		ConfigSnapshot: func() any {
			return map[string]any{"profiles": map[string]any{"worker": configMetadata}}
		},
		ApplyConfig: func([]byte) (any, error) {
			configApplyCalls++
			return map[string]string{"status": "applied"}, nil
		},
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{{Name: "worker", Content: profileContent, Hash: "profile-colon-rev", Runtime: configMetadata["runtime"], Model: configMetadata["model"], Reasoning: configMetadata["reasoning"]}}, nil
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
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d body=%#v", response.StatusCode, config)
	}
	configText, _ := config["content"].(string)
	for _, secret := range []string{"config-api-key-colon", "config-password-colon", "config-callback-colon", "config-auth-colon", "config-apikey-colon", "config-token-colon", "config-secret-colon", "config-credential-colon", "config-session-colon", "config-native-colon", "config-task-colon"} {
		if strings.Contains(configText, secret) {
			t.Fatalf("config leaked %q: %q", secret, configText)
		}
	}
	if !strings.Contains(configText, `ordinary = "keep-config"`) || config["editable"] != false {
		t.Fatalf("config safe field/editability changed: %#v", config)
	}
	configRevision, _ := config["revision"].(string)

	response, err = client.Get(server.URL + "/v1/control/profiles")
	if err != nil {
		t.Fatal(err)
	}
	var profiles []controlProfile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(profiles) != 1 {
		t.Fatalf("profiles status=%d body=%#v", response.StatusCode, profiles)
	}
	profileText := profiles[0].Content
	for _, secret := range []string{"profile-api-key-colon", "profile-password-colon", "profile-callback-colon", "profile-apikey-colon", "profile-token-colon", "profile-secret-colon", "profile-credential-colon", "profile-session-colon", "profile-native-colon", "profile-task-colon", "metadata-api-key-colon", "metadata-password-colon", "metadata-callback-colon"} {
		if strings.Contains(profileText, secret) || strings.Contains(profiles[0].Runtime, secret) || strings.Contains(profiles[0].Model, secret) || strings.Contains(profiles[0].Reasoning, secret) {
			t.Fatalf("profile leaked %q: %#v", secret, profiles[0])
		}
	}
	if profiles[0].Editable || !strings.Contains(profileText, "ordinary profile content") || profiles[0].Runtime != "[redacted]" || profiles[0].Model != "[redacted]" || profiles[0].Reasoning != "[redacted]" {
		t.Fatalf("profile safe fields/editability changed: %#v", profiles[0])
	}
	profileRevision := profiles[0].Revision

	configPayload, _ := json.Marshal(map[string]string{"content": `skills = "api_key: config-write-api-key-colon token: config-write-token-colon task_id: config-write-task-colon"`, "expected_revision": configRevision})
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/v1/control/config", bytes.NewReader(configPayload))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || configApplyCalls != 0 {
		t.Fatalf("colon config write was accepted status=%d calls=%d", response.StatusCode, configApplyCalls)
	}

	profilePayload, _ := json.Marshal(map[string]string{"content": "# profile\\nNotes: password: profile-write-password-colon auth: profile-write-auth-colon native_id: profile-write-native-colon", "expected_revision": profileRevision})
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/control/profiles/worker", bytes.NewReader(profilePayload))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || profileApplyCalls != 0 {
		t.Fatalf("colon profile write was accepted status=%d calls=%d", response.StatusCode, profileApplyCalls)
	}

	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "worker-colon", Intent: "inspect", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(ctx, attempt.ID, core.FinishAttemptInput{AttemptOutcomeInput: core.AttemptOutcomeInput{Status: core.OutcomeFailed, Classification: core.OutcomeFinal, Diagnostics: `{"safe":"api_key: diagnostics-api-key-colon","password":"password: diagnostics-password-colon","callback":"callback: diagnostics-callback-colon","extra":"apikey: diagnostics-apikey-colon token: diagnostics-token-colon secret: diagnostics-secret-colon credential: diagnostics-credential-colon auth: diagnostics-auth-colon session_id: diagnostics-session-colon native_id: diagnostics-native-colon task_id: diagnostics-task-colon","ordinary":"kept","summary":"safe summary"}`, Summary: "safe summary"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "ticket11.colon", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: map[string]any{"safe": "api_key: event-api-key-colon", "ordinary": "kept"}}); err != nil {
		t.Fatal(err)
	}

	for _, endpoint := range []string{"/v1/control/overview", "/v1/control/events", "/v1/control/diagnostics/worker-colon", "/v1/control/export"} {
		response, err := client.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d err=%v body=%s", endpoint, response.StatusCode, err, body)
		}
		text := string(body)
		for _, secret := range []string{"metadata-api-key-colon", "metadata-password-colon", "metadata-callback-colon", "diagnostics-api-key-colon", "diagnostics-password-colon", "diagnostics-callback-colon", "diagnostics-apikey-colon", "diagnostics-token-colon", "diagnostics-secret-colon", "diagnostics-credential-colon", "diagnostics-auth-colon", "diagnostics-session-colon", "diagnostics-native-colon", "diagnostics-task-colon", "event-api-key-colon"} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s leaked %q: %s", endpoint, secret, text)
			}
		}
		if !strings.Contains(text, "safe summary") || !strings.Contains(text, "ordinary") {
			t.Fatalf("%s removed safe diagnostics content: %s", endpoint, text)
		}
	}
}

func TestTicket11BareSessionNativeTaskValuesFailClosedAcrossControlAndPublicSurfaces(t *testing.T) {
	store, api, server, client := controlRoomTestAPI(t)
	api.SetDebug(true)
	controlRoomLogin(t, client, server.URL)
	ctx := context.Background()

	configContent := `model = "safe-model"
description = "session: bare-config-session native: bare-config-native task: bare-config-task"
ordinary = "keep-config"
`
	profileContent := `# Worker profile
model: safe-model
ordinary: kept profile content

This Markdown contains session: bare-profile-session.
Another line contains native: bare-profile-native and task: bare-profile-task.
`
	configApplyCalls, profileApplyCalls := 0, 0
	api.AttachControl(ControlOptions{
		RequireExpectedRevision: true,
		ConfigContent:           func() (string, error) { return configContent, nil },
		ConfigSnapshot: func() any {
			return map[string]any{"safe": "session: bare-snapshot-session native: bare-snapshot-native task: bare-snapshot-task", "ordinary": "kept"}
		},
		ApplyConfig: func(content []byte) (any, error) {
			configApplyCalls++
			configContent = string(content)
			return map[string]string{"status": "applied"}, nil
		},
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{{Name: "worker", Content: profileContent, Hash: "bare-profile-rev"}}, nil
		},
		ApplyProfile: func(_ string, content []byte) error {
			profileApplyCalls++
			profileContent = string(content)
			return nil
		},
	})

	response, err := client.Get(server.URL + "/v1/control/config")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("config status=%d body=%#v", response.StatusCode, config)
	}
	configText, _ := config["content"].(string)
	configRevision, _ := config["revision"].(string)
	for _, secret := range []string{"bare-config-session", "bare-config-native", "bare-config-task"} {
		if strings.Contains(configText, secret) {
			t.Fatalf("config leaked %q: %q", secret, configText)
		}
	}
	if !strings.Contains(configText, `ordinary = "keep-config"`) || config["editable"] != false {
		t.Fatalf("config safe field/editability changed: %#v", config)
	}

	response, err = client.Get(server.URL + "/v1/control/profiles")
	if err != nil {
		t.Fatal(err)
	}
	var profiles []controlProfile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(profiles) != 1 {
		t.Fatalf("profiles status=%d body=%#v", response.StatusCode, profiles)
	}
	profileText := profiles[0].Content
	profileRevision := profiles[0].Revision
	for _, secret := range []string{"bare-profile-session", "bare-profile-native", "bare-profile-task"} {
		if strings.Contains(profileText, secret) {
			t.Fatalf("profile leaked %q: %q", secret, profileText)
		}
	}
	if !strings.Contains(profileText, "ordinary: kept profile content") || profiles[0].Editable {
		t.Fatalf("profile safe field/editability changed: %#v", profiles[0])
	}

	writeJSONRequest := func(method, path string, payload map[string]string) *http.Response {
		t.Helper()
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		request, requestErr := http.NewRequest(method, server.URL+path, bytes.NewReader(encoded))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		result, doErr := client.Do(request)
		if doErr != nil {
			t.Fatal(doErr)
		}
		return result
	}
	response = writeJSONRequest(http.MethodPut, "/v1/control/config", map[string]string{
		"content":           `ordinary = "session: bare-write-session"`,
		"expected_revision": configRevision,
	})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || configApplyCalls != 0 {
		t.Fatalf("bare config write was accepted status=%d calls=%d", response.StatusCode, configApplyCalls)
	}
	response = writeJSONRequest(http.MethodPut, "/v1/control/profiles/worker", map[string]string{
		"content":           "# profile\\nNotes: native: bare-write-native task: bare-write-task session: bare-write-session",
		"expected_revision": profileRevision,
	})
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || profileApplyCalls != 0 {
		t.Fatalf("bare profile write was accepted status=%d calls=%d", response.StatusCode, profileApplyCalls)
	}
	response = writeJSONRequest(http.MethodPut, "/v1/control/profiles/worker", map[string]string{
		"content":           "# profile\\nordinary: editable Markdown",
		"expected_revision": profileRevision,
	})
	response.Body.Close()
	if response.StatusCode != http.StatusOK || profileApplyCalls != 1 || profileContent != "# profile\\nordinary: editable Markdown" {
		t.Fatalf("ordinary profile write was not editable status=%d calls=%d content=%q", response.StatusCode, profileApplyCalls, profileContent)
	}

	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "worker-bare", Title: "safe worker", Intent: "session: bare-public-session native: bare-public-native task: bare-public-task",
		ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe policy",
	}, core.TurnSpec{Input: "session: bare-turn-session native: bare-turn-native task: bare-turn-task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishAttempt(ctx, attempt.ID, core.FinishAttemptInput{AttemptOutcomeInput: core.AttemptOutcomeInput{
		Status: core.OutcomeFailed, Classification: core.OutcomeFinal,
		Diagnostics: `{"safe":"session: bare-diagnostics-session native: bare-diagnostics-native task: bare-diagnostics-task","ordinary":"kept","summary":"safe summary"}`,
		Summary:     "safe summary",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "ticket11.bare", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: map[string]any{
		"safe": "session: bare-event-session native: bare-event-native task: bare-event-task", "ordinary": "kept",
	}}); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	log := `{"jsonrpc":"2.0","params":{"update":{"sessionUpdate":"tool_call","title":"safe tool","details":"session: bare-raw-session native: bare-raw-native task: bare-raw-task"}}}` + "\\n"
	if err := os.WriteFile(filepath.Join(logDir, "worker-bare.jsonl"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	api.AttachDiagnosticLogDir(logDir)

	endpoints := []string{
		"/v1/control/overview", "/v1/control/events", "/v1/control/diagnostics/worker-bare", "/v1/control/export",
		"/v1/workers/worker-bare", "/v1/workers/worker-bare/diagnostics",
	}
	for _, endpoint := range endpoints {
		response, err := client.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d err=%v body=%s", endpoint, response.StatusCode, err, body)
		}
		text := string(body)
		for _, secret := range []string{
			"bare-snapshot-session", "bare-snapshot-native", "bare-snapshot-task", "bare-public-session", "bare-public-native", "bare-public-task", "bare-turn-session", "bare-turn-native", "bare-turn-task",
			"bare-diagnostics-session", "bare-diagnostics-native", "bare-diagnostics-task", "bare-event-session", "bare-event-native", "bare-event-task", "bare-raw-session", "bare-raw-native", "bare-raw-task",
		} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s leaked %q: %s", endpoint, secret, text)
			}
		}
	}
}

func nowForTest() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
