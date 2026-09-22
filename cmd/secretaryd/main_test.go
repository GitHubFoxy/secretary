package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/webapi"
)

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
	ctx := context.Background()
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
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "production-worker", Title: "Production worker", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", Intent: "inspect", PolicySnapshot: "{}"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	local := node.NewLocal(&productionRuntime{})
	if _, err := local.Dispatch(ctx, node.StartRequest{WorkerRef: worker.WorkerRef}); err != nil {
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
	attachProductionWorkerServices(api, store, person.ID, capability, local, nil, nil)
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
}

type productionRuntime struct{}

func (*productionRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	return &productionSession{}, nil
}

type productionSession struct{}

func (*productionSession) ID() string                                    { return "production-native" }
func (*productionSession) Prompt(context.Context, string) error          { return nil }
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

func TestNodesConnectRefusesRealViewerCredential(t *testing.T) {
	ctx := context.Background()
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
	credential := pairedViewerCredential(t, provision)

	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
		PairingTokens: []string{"pairing-token"}, AdminToken: "admin-token", ClientBootstrapToken: "bootstrap",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := rootHandler(http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), manager, false)

	request := httptest.NewRequest(http.MethodGet, "/v1/nodes/connect", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("/v1/nodes/connect status=%d with a real viewer Client credential, want protocol rejection 400", recorder.Code)
	}
}

// pairedViewerCredential mints the contract's Pi viewer credential: a Client
// approved with exactly the three viewer scopes from docs/pi-viewer.md.
func pairedViewerCredential(t *testing.T, server *httptest.Server) string {
	t.Helper()
	pairRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/clients/pair", strings.NewReader(
		`{"bootstrap_token":"bootstrap","device_id":"pi-review","display_name":"Pi","platform":"pi","scopes":["conversation:read","worker:read","approval:read"]}`))
	if err != nil {
		t.Fatal(err)
	}
	pairRequest.Header.Set("Content-Type", "application/json")
	pairRequest.Header.Set("Idempotency-Key", "test-pair-pi-review")
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
	approveRequest.Header.Set("Idempotency-Key", "test-approve-pi-review")
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
