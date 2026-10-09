package main

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

	"github.com/coder/websocket"

	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/webapi"
)

func TestAddressedReplyInstructionsAreOptInAndDoNotRewriteExternalProfile(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	manager, err := config.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	legacy := manager.Snapshot()
	externalPath := legacy.Profiles["secretary"].Path
	externalBefore, err := os.ReadFile(externalPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(externalBefore), "Addressed reply v1") || legacy.Profiles["secretary"].ReplyContractVersion != "" {
		t.Fatal("legacy external Secretary profile unexpectedly contains addressed reply instructions")
	}
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(configContent), "reasoning = \"default\"", "reasoning = \"default\"\nreply_contract = \"addressed-reply-v1\"", 1)
	if updated == string(configContent) {
		t.Fatal("could not configure explicit reply contract")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	optedIn, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := secretaryProfile(optedIn, store)
	if profile.ReplyContractVersion != core.SecretaryReplyContractAddressedV1 || !strings.Contains(profile.Content, "## Addressed reply v1") {
		t.Fatalf("opt-in managed Profile omitted instructions/version: %#v", profile)
	}
	files := profileFiles(optedIn)
	if len(files) == 0 || strings.Contains(files[0].Content, "Addressed reply v1") {
		t.Fatalf("API profile view rewrote external instructions: %#v", files)
	}
	externalAfter, err := os.ReadFile(externalPath)
	if err != nil || string(externalAfter) != string(externalBefore) {
		t.Fatalf("external Profile changed during opt-in: err=%v", err)
	}
}

func TestExistingInstallationPinsLegacySecretaryAndWorkerStores(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "secretary.db"), []byte("existing server state"), 0o600); err != nil {
		t.Fatal("existing installation fixture unavailable")
	}
	legacy := filepath.Join(t.TempDir(), "legacy-xdg")
	t.Setenv("XDG_DATA_HOME", legacy)
	selection, err := selectRuntimeNativeStoreSelection(root)
	if err != nil {
		t.Fatal(err)
	}
	secretaryStore, workerStore := selection.Secretary, selection.Worker
	if secretaryStore.Mode != node.OpenCodeNativeStoreModeLegacy || secretaryStore.DataHome != legacy {
		t.Fatal("existing Secretary sessions were not pinned to the legacy OpenCode store")
	}
	if workerStore.Mode != node.OpenCodeNativeStoreModeLegacy || workerStore.DataHome != legacy {
		t.Fatal("existing local Worker sessions were not pinned to the legacy OpenCode store")
	}
	snapshot := config.Snapshot{Config: config.Config{Secretary: config.SecretaryPolicy{Harness: "opencode"}}}
	secretaryRuntime, _ := configuredRuntimeWithStore(snapshot, root, secretaryStore)
	workerRuntime, _ := configuredRuntimeWithStore(snapshot, filepath.Join(root, "node", "data"), workerStore)
	secretaryOpenCode := secretaryRuntime.(node.RuntimeRouter).OpenCode.(node.OpenCodeRuntime)
	workerOpenCode := workerRuntime.(node.RuntimeRouter).OpenCode.(node.OpenCodeRuntime)
	if secretaryOpenCode.DataHome != legacy || !secretaryOpenCode.LegacyDataHome || workerOpenCode.DataHome != legacy || !workerOpenCode.LegacyDataHome {
		t.Fatal("Secretary or Worker runtime switched away from the selected legacy store")
	}
	if _, err := os.Stat(node.OpenCodeNativeDataHome(root)); !os.IsNotExist(err) {
		t.Fatal("legacy selection created an empty replacement Secretary store")
	}
	if _, err := os.Stat(node.OpenCodeNativeDataHome(filepath.Join(root, "node", "data"))); !os.IsNotExist(err) {
		t.Fatal("legacy selection created an empty replacement Worker store")
	}
}

func TestRuntimeNativeStoreSelectionRejectsCoLocatedConflictAndKeepsStandaloneIndependent(t *testing.T) {
	root := t.TempDir()
	nodeDataDir := filepath.Join(root, "node", "data")
	if err := os.MkdirAll(nodeDataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("harness = \"fx\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyHome := filepath.Join(t.TempDir(), "legacy")
	sharedHome := node.OpenCodeNativeDataHome(root)
	externalStandaloneDataDir := filepath.Join(t.TempDir(), "standalone Node data")
	standaloneHome := node.OpenCodeNativeDataHome(externalStandaloneDataDir)
	t.Setenv("XDG_DATA_HOME", legacyHome)
	deployment := node.DeploymentConfig{ServerURL: "http://127.0.0.1:8081", Node: core.NodeReference("local-node"), DataDir: nodeDataDir, IncludeOpenCode: true}
	encodedDeployment, err := json.Marshal(deployment)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "node", "config.json")
	if err := os.WriteFile(configPath, encodedDeployment, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSelection := func(path, mode, dataHome string) {
		t.Helper()
		encoded, err := json.Marshal(map[string]any{"version": 1, "mode": mode, "data_home": dataHome})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	secretarySelectionPath := filepath.Join(root, "opencode-native-selection.json")
	nodeSelectionPath := filepath.Join(nodeDataDir, "opencode-native-selection.json")
	writeSelection(secretarySelectionPath, string(node.OpenCodeNativeStoreModeLegacy), legacyHome)
	writeSelection(nodeSelectionPath, string(node.OpenCodeNativeStoreModeShared), sharedHome)
	if _, err := selectRuntimeNativeStoreSelection(root); err == nil {
		t.Fatal("co-located Node with a different shared store passed the Secretary pair check")
	}
	if err := node.SaveDeploymentConfig(configPath, node.DeploymentConfig{ServerURL: deployment.ServerURL, Node: deployment.Node, DataDir: externalStandaloneDataDir, IncludeOpenCode: true, Standalone: true}); err != nil {
		t.Fatal(err)
	}
	externalNodeSelectionPath := filepath.Join(externalStandaloneDataDir, "opencode-native-selection.json")
	writeSelection(externalNodeSelectionPath, string(node.OpenCodeNativeStoreModeIsolated), standaloneHome)
	selection, err := selectRuntimeNativeStoreSelection(root)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.NodeStandalone || selection.NodeDataDir != externalStandaloneDataDir || selection.Secretary.Mode != node.OpenCodeNativeStoreModeLegacy || selection.Worker.Mode != node.OpenCodeNativeStoreModeIsolated || selection.Worker.DataHome != standaloneHome {
		t.Fatalf("standalone Node did not retain its independent Go-selected store: %#v", selection)
	}
	if _, _, err := node.SelectSharedOpenCodeNativeStores(root, nodeDataDir); err == nil {
		t.Fatal("owner shared-store transition accepted standalone Node deployment")
	}
}

func TestRuntimeNativeStoreSelectionRejectsCoLocatedDataDirMismatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node", "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := node.SaveDeploymentConfig(filepath.Join(root, "node", "config.json"), node.DeploymentConfig{
		ServerURL: "http://127.0.0.1:8081", Node: core.NodeReference("local-node"), DataDir: filepath.Join(t.TempDir(), "external"), IncludeOpenCode: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := selectRuntimeNativeStoreSelection(root); err == nil || !strings.Contains(err.Error(), "data directory does not match") {
		t.Fatalf("co-located data_dir mismatch was not rejected: %v", err)
	}
}

func TestConfigOnlyExistingInstallationPinsLegacyStores(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("harness = \"fx\"\n"), 0o600); err != nil {
		t.Fatal("existing FX installation fixture unavailable")
	}
	legacy := filepath.Join(t.TempDir(), "legacy-xdg")
	t.Setenv("XDG_DATA_HOME", legacy)
	selection, err := selectRuntimeNativeStoreSelection(root)
	if err != nil {
		t.Fatal(err)
	}
	secretaryStore, workerStore := selection.Secretary, selection.Worker
	if secretaryStore.Mode != node.OpenCodeNativeStoreModeLegacy || workerStore.Mode != node.OpenCodeNativeStoreModeLegacy || secretaryStore.DataHome != legacy || workerStore.DataHome != legacy {
		t.Fatal("config-only existing installation was silently assigned a new native store")
	}
	if _, err := os.Stat(node.OpenCodeNativeDataHome(root)); !os.IsNotExist(err) {
		t.Fatal("config-only legacy setup created a replacement native database")
	}
}

func TestSecretaryAndLocalNodeRuntimeUseOneNativeStore(t *testing.T) {
	serverRoot := filepath.Join(t.TempDir(), "secretary")
	snapshot := config.Snapshot{Config: config.Config{Secretary: config.SecretaryPolicy{Harness: "opencode"}}}
	secretaryRuntime, workerRuntime, _, _ := configuredRuntimePair(snapshot, serverRoot)
	secretaryRouter := secretaryRuntime.(node.RuntimeRouter)
	workerRouter := workerRuntime.(node.RuntimeRouter)
	secretaryOpenCode := secretaryRouter.OpenCode.(node.OpenCodeRuntime)
	workerOpenCode := workerRouter.OpenCode.(node.OpenCodeRuntime)
	sharedStore := node.OpenCodeNativeDataHome(serverRoot)
	if secretaryOpenCode.DataHome != sharedStore || workerOpenCode.DataHome != sharedStore {
		t.Fatal("Secretary and local Node Worker do not use the common OpenCode store")
	}
}

func TestProductionStartupRecoversUnknownPhase4Attempt(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "production-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "startup-recovery-worker", ProjectID: "project", NodeID: "missing-node", HarnessInstanceID: "missing-node/fx", Intent: "recover", PolicySnapshot: "{}"}, core.TurnSpec{Input: "recover"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := recoverProductionPhase4Attempts(ctx, store, nil, nil); err != nil {
		t.Fatal(err)
	}
	recoveredAttempt, err := store.Phase4Attempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredAttempt.State != core.AttemptInterrupted {
		t.Fatalf("attempt state=%s, want interrupted", recoveredAttempt.State)
	}
	recoveredResult, err := store.Phase4Result(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveredResult.Status != core.ResultInterrupted || recoveredResult.FailureCode != "runtime_execution_unknown" {
		t.Fatalf("recovery result=%#v", recoveredResult)
	}
}

func TestProductionAssemblyRespondsWithoutManualResponderAttachment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "production.db"))
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
	workspace := t.TempDir()
	instance := core.HarnessInstance{ID: "local/fx", Node: "local", Kind: core.HarnessFX, Version: "fixture", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	snapshot, err := json.Marshal(core.ProjectSnapshot{ID: "project", Node: "local", HarnessInstance: instance})
	if err != nil {
		t.Fatal(err)
	}
	profile := node.ManagedProfile{Version: "fixture", Name: "worker", Content: "Synthetic instructions", SourceHash: "fixture", Runtime: "fx", Delivery: "workspace_instructions"}
	profile.Hash = profile.SnapshotHash()
	profileJSON, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "production-worker", Title: "Production worker", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", Intent: "inspect", PolicySnapshot: "{}", ProjectSnapshot: string(snapshot), ProfileSnapshot: string(profileJSON), Workspace: workspace}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	prompts := make(chan string, 3)
	local := node.NewLocal(&productionRuntime{prompts: prompts})
	if _, err := local.Dispatch(ctx, node.StartRequest{WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Task: "inspect", HarnessInstance: instance, Profile: profile, Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{EventID: "production-approval", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "production-request", Summary: "write file"}}); err != nil {
		t.Fatal(err)
	}
	api, err := webapi.New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	// This is the production assembly seam. The test must not attach a responder itself.
	attachProductionWorkerServices(ctx, api, store, person.ID, capability, local, nil, nil, nil, nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustProductionCookieJar(t)}
	loginProduction(t, client, server.URL)
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/approvals/production-request/approve", bytes.NewBufferString("{}"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "production-approval")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("approval status=%d", response.StatusCode)
	}
	approval, err := store.Approval(ctx, "production-request")
	if err != nil || approval.State != core.ApprovalApproved {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/workers/production-worker/message", bytes.NewBufferString(`{"text":"/q continue"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "production-queue")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("queue status=%d", response.StatusCode)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		response, err := client.Get(server.URL + "/v1/workers/production-worker")
		if err != nil {
			t.Fatal(err)
		}
		var details struct {
			QueuedMessages []core.QueuedWorkerMessage `json:"queued_messages"`
		}
		err = json.NewDecoder(response.Body).Decode(&details)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(details.QueuedMessages) == 1 && details.QueuedMessages[0].State == "delivered" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("production queue pump did not promote terminal Follow-up: %#v", details)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		select {
		case prompt := <-prompts:
			if prompt == "continue" {
				return
			}
		case <-time.After(time.Second):
			t.Fatal("delivered queue did not prompt the resumed native session")
		}
	}
}

type productionRuntime struct{ prompts chan string }

func (r *productionRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	return &productionSession{prompts: r.prompts}, nil
}

func (r *productionRuntime) Resume(ctx context.Context, request node.StartRequest, _ string) (node.Session, error) {
	return r.Start(ctx, request)
}

type productionSession struct{ prompts chan string }

func (*productionSession) ID() string { return "production-native" }
func (s *productionSession) Prompt(_ context.Context, text string) error {
	if s.prompts != nil {
		s.prompts <- text
	}
	return nil
}
func (*productionSession) Steer(context.Context, string) (bool, error)   { return true, nil }
func (*productionSession) Cancel(context.Context) error                  { return nil }
func (*productionSession) Activity() <-chan node.Activity                { return nil }
func (*productionSession) Result() <-chan node.Result                    { return nil }
func (*productionSession) Close() error                                  { return nil }
func (*productionSession) Respond(context.Context, string, string) error { return nil }

func mustProductionCookieJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestSecretaryMCPCommandHonorsOverride(t *testing.T) {
	t.Setenv("SECRETARY_MCP_COMMAND", "custom-secretary-mcp")
	got, err := secretaryMCPCommand()
	if err != nil {
		t.Fatal(err)
	}
	if got != "custom-secretary-mcp" {
		t.Fatalf("secretaryMCPCommand()=%q, want override", got)
	}
}

func TestSecretaryMCPServerURLUsesLoopbackForWildcardListeners(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8081": "http://127.0.0.1:8081",
		":8081":          "http://127.0.0.1:8081",
		"0.0.0.0:8081":   "http://127.0.0.1:8081",
		"[::]:8081":      "http://127.0.0.1:8081",
		"[::1]:8081":     "http://[::1]:8081",
	}
	for listen, want := range cases {
		if got := secretaryMCPServerURL(listen); got != want {
			t.Errorf("secretaryMCPServerURL(%q)=%q, want %q", listen, got, want)
		}
	}
}

func loginProduction(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	response, err := client.Post(baseURL+"/v1/web/session", "application/json", strings.NewReader(`{"bootstrap_token":"bootstrap"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login status=%d", response.StatusCode)
	}
}

func TestValidateListenRejectsNonLoopback(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:8081", "[::1]:8081"} {
		if err := validateListen(listen); err != nil {
			t.Errorf("validateListen(%q)=%v, want nil", listen, err)
		}
	}
	for _, listen := range []string{"0.0.0.0:8081", ":8081", "localhost:8081", "192.168.1.10:8081", "secretary.internal:8081", "8081"} {
		if err := validateListen(listen); err == nil {
			t.Errorf("validateListen(%q)=nil, want rejection of non-loopback bind", listen)
		}
	}
}

func servedResponse(handler http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestControlRoutesStayDebugOnly(t *testing.T) {
	spy := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(name)) })
	}
	normal := rootHandler(spy("api"), spy("control"), spy("static"), spy("control-static"), nil, false)
	for _, target := range []string{"/v1/control/overview", "/control-room"} {
		if response := servedResponse(normal, target); response.Code != http.StatusNotFound {
			t.Errorf("normal mode %s status=%d, want 404", target, response.Code)
		}
	}
	if response := servedResponse(normal, "/v1/health"); response.Code == http.StatusNotFound {
		t.Errorf("normal mode /v1/health status=%d, want API route to answer", response.Code)
	}

	debug := rootHandler(spy("api"), spy("control"), spy("static"), spy("control-static"), nil, true)
	if response := servedResponse(debug, "/v1/control/overview"); response.Code != http.StatusOK || response.Body.String() != "control" {
		t.Errorf("debug mode /v1/control/overview status=%d body=%q, want control handler", response.Code, response.Body.String())
	}
	if response := servedResponse(debug, "/control-room"); response.Code != http.StatusOK || response.Body.String() != "control-static" {
		t.Errorf("debug mode /control-room status=%d body=%q, want control static handler", response.Code, response.Body.String())
	}
}

// TestNodesConnectRequiresNodeReference pins only the gateway check: without a
// valid ?node= parameter the protocol endpoint answers 400 before any
// WebSocket upgrade or credential evaluation. It proves nothing about
// credentials; TestNodesConnectRejectsClientCredential covers that.
func TestNodesConnectRequiresNodeReference(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "nodes-connect.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
		PairingTokens: []string{"pairing-token"}, AdminToken: "admin-token", ClientBootstrapToken: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := rootHandler(http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), manager, false)

	request := httptest.NewRequest(http.MethodGet, "/v1/nodes/connect", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "valid Node reference") {
		t.Fatalf("/v1/nodes/connect without ?node= status=%d body=%q, want 400 requiring a Node reference", recorder.Code, recorder.Body.String())
	}
}

// TestNodesConnectRejectsClientCredential proves a paired Client credential
// cannot stand in for a Node capability on /v1/nodes/connect:
//  1. a real Node is enrolled, so ?node= is valid and its record exists;
//  2. a positive control completes the handshake with the enrolled Node
//     credential, proving routing, query parameter, and record lookup all pass;
//  3. the negative case reuses the same valid ?node=, a real WebSocket upgrade,
//     and the client credential in HTTP Authorization, but signs the protocol
//     handshake with the client secret instead of the Node capability.
//
// The rejection must come from protocol authentication
// (StatusPolicyViolation "node protocol: authentication failed"), not from the
// missing-parameter 400. Note the protocol never reads the Authorization
// header; carrying the client credential there must not grant access.
func TestNodesConnectRejectsClientCredential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "nodes-connect.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := webapi.New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	provision := httptest.NewServer(api.Handler())
	defer provision.Close()
	credential := pairedReadOnlyClientCredential(t, provision)

	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
		PairingTokens: []string{"pairing-token"}, AdminToken: "admin-token", ClientBootstrapToken: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	api.AttachNodeService(manager)
	handler := rootHandler(api.Handler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), manager, false)
	server := httptest.NewServer(handler)
	defer server.Close()

	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "pairing-token", "client-review-node")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(identity.ConnectURL, "node=client-review-node") {
		t.Fatalf("ConnectURL=%q lacks the enrolled ?node= parameter", identity.ConnectURL)
	}
	inventory := core.HarnessInventorySnapshot{Node: identity.Node, ObservedAt: time.Now().UTC()}

	nodeAuth, err := identity.Authenticator()
	if err != nil {
		t.Fatal(err)
	}
	control, err := node.DialProtocol(ctx, identity.ConnectURL, identity.Node, nodeAuth, node.Handshake{
		Node: identity.Node, ProtocolVersion: node.ProtocolVersion, Inventory: inventory, Nonce: "control-nonce",
	})
	if err != nil {
		t.Fatalf("positive control with the enrolled Node credential failed: %v", err)
	}
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+credential)
	connection, response, err := websocket.Dial(ctx, identity.ConnectURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("WebSocket upgrade with valid ?node= and client Authorization failed: %v response=%v", err, response)
	}
	defer connection.Close(websocket.StatusNormalClosure, "")

	clientAuth := node.NewAuthenticator([]byte(credential))
	clientHandshake := node.Handshake{
		Node: identity.Node, ProtocolVersion: node.ProtocolVersion, Inventory: inventory, Nonce: "client-nonce",
	}
	clientHandshake.NonceSignature = clientAuth.SignNonce(clientHandshake.Node, clientHandshake.Nonce)
	payload, err := json.Marshal(clientHandshake)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := node.NewEnvelope(node.MessageHandshake, identity.Node, 0, 0, payload, clientAuth)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Write(ctx, websocket.MessageText, encoded); err != nil {
		t.Fatalf("send client-signed handshake: %v", err)
	}

	_, _, err = connection.Read(ctx)
	if err == nil {
		t.Fatal("Node protocol accepted a handshake signed with the client credential")
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("want protocol close, got %T: %v", err, err)
	}
	if closeErr.Code != websocket.StatusPolicyViolation || !strings.Contains(closeErr.Reason, "authentication failed") {
		t.Fatalf("close code=%d reason=%q, want StatusPolicyViolation for node protocol authentication failure", closeErr.Code, closeErr.Reason)
	}
}

// pairedReadOnlyClientCredential mints a Client credential with exactly
// conversation:read, worker:read, and approval:read.
func pairedReadOnlyClientCredential(t *testing.T, server *httptest.Server) string {
	t.Helper()
	pairRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/clients/pair", strings.NewReader(
		`{"bootstrap_token":"bootstrap","device_id":"readonly-client","display_name":"Client","platform":"test","scopes":["conversation:read","worker:read","approval:read"]}`))
	if err != nil {
		t.Fatal(err)
	}
	pairRequest.Header.Set("Content-Type", "application/json")
	pairRequest.Header.Set("Idempotency-Key", "test-pair-readonly-client")
	pairResponse, err := http.DefaultClient.Do(pairRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer pairResponse.Body.Close()
	rawPair, err := io.ReadAll(pairResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if pairResponse.StatusCode != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s, want 201", pairResponse.StatusCode, rawPair)
	}
	var pair struct {
		Status   string `json:"status"`
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(rawPair, &pair); err != nil {
		t.Fatal(err)
	}
	if pair.Status != "pending" || pair.ClientID == "" {
		t.Fatalf("pair response=%#v, want pending client_id", pair)
	}

	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	loginProduction(t, owner, server.URL)

	approveRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/clients/"+pair.ClientID+"/approve", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	approveRequest.Header.Set("Content-Type", "application/json")
	approveRequest.Header.Set("Idempotency-Key", "test-approve-readonly-client")
	approveResponse, err := owner.Do(approveRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer approveResponse.Body.Close()
	if approveResponse.StatusCode != http.StatusOK {
		t.Fatalf("approve status=%d, want 200", approveResponse.StatusCode)
	}
	var approved struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(approveResponse.Body).Decode(&approved); err != nil {
		t.Fatal(err)
	}
	if approved.Credential == "" {
		t.Fatal("approve returned no credential")
	}
	return approved.Credential
}
