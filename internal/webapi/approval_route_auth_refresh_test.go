package webapi

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestApprovalRouteRevokedClientBetweenAuthChecksDoesNotFallBackOrMutate(t *testing.T) {
	fixture := newApprovalRouteAuthFixture(t, core.ApprovalApproved, "private-saved-response-marker")
	ownerJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, fixture.server.URL)
	writeOnlyID, writeOnlyCredential := pairApprovalMutationClient(t, owner, fixture.server, "approval:write", "revoked-between-checks")
	readOnlyID, readOnlyCredential := pairApprovalMutationClient(t, owner, fixture.server, "approval:read", "wrong-approval-scope")
	_ = readOnlyID

	revocation := make(chan int, 1)
	var revoked atomic.Bool
	fixture.api.approvalAuthorizationCheckpoint = func() {
		if !revoked.CompareAndSwap(false, true) {
			return
		}
		request, requestErr := http.NewRequest(http.MethodPost, fixture.server.URL+"/v1/clients/"+writeOnlyID+"/revoke", strings.NewReader(`{}`))
		if requestErr != nil {
			revocation <- 0
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "revoke-between-approval-checks")
		response, requestErr := owner.Do(request)
		if requestErr != nil {
			revocation <- 0
			return
		}
		response.Body.Close()
		revocation <- response.StatusCode
	}

	client := &http.Client{}
	request, err := http.NewRequest(http.MethodPost, fixture.server.URL+"/v1/approvals/"+fixture.approval.ID+"/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+writeOnlyCredential)
	request.Header.Set("Idempotency-Key", "revoked-owner-retry")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	revokeStatus := 0
	select {
	case revokeStatus = <-revocation:
	default:
	}
	if revokeStatus != http.StatusOK {
		t.Fatalf("public revoke status=%d", revokeStatus)
	}
	singleUnauthorizedResponse := string(responseBody) == "Client or web session required\n"
	if response.StatusCode != http.StatusUnauthorized || fixture.runtime.calls.Load() != 0 || hasOwnerApprovalMutationDTO(responseBody) || !singleUnauthorizedResponse {
		t.Fatalf("revoked_client_status=%d mutation_calls_zero=%t owner_dto_absent=%t single_unauthorized_response=%t", response.StatusCode, fixture.runtime.calls.Load() == 0, !hasOwnerApprovalMutationDTO(responseBody), singleUnauthorizedResponse)
	}
	assertApprovalRouteFixtureUnchanged(t, fixture)

	wrongScopeRequest, err := http.NewRequest(http.MethodPost, fixture.server.URL+"/v1/approvals/"+fixture.approval.ID+"/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongScopeRequest.Header.Set("Authorization", "Bearer "+readOnlyCredential)
	wrongScopeRequest.Header.Set("Idempotency-Key", "wrong-scope-owner-retry")
	wrongScopeResponse, err := client.Do(wrongScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	wrongScopeBody, err := io.ReadAll(wrongScopeResponse.Body)
	wrongScopeResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	singleForbiddenResponse := string(wrongScopeBody) == "Client scope required\n"
	if wrongScopeResponse.StatusCode != http.StatusForbidden || fixture.runtime.calls.Load() != 0 || hasOwnerApprovalMutationDTO(wrongScopeBody) || !singleForbiddenResponse {
		t.Fatalf("wrong_scope_status=%d mutation_calls_zero=%t owner_dto_absent=%t single_forbidden_response=%t", wrongScopeResponse.StatusCode, fixture.runtime.calls.Load() == 0, !hasOwnerApprovalMutationDTO(wrongScopeBody), singleForbiddenResponse)
	}
	assertApprovalRouteFixtureUnchanged(t, fixture)
}

func TestOwnerWorkerRefreshShowsSavedApprovalDecisionWithoutPrivateIntent(t *testing.T) {
	const savedResponse = "private-saved-response-marker"
	for _, decision := range []core.ApprovalState{core.ApprovalApproved, core.ApprovalDenied} {
		t.Run(string(decision), func(t *testing.T) {
			fixture := newApprovalRouteAuthFixture(t, decision, savedResponse)
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			owner := &http.Client{Jar: jar}
			login(t, owner, fixture.server.URL)
			response, err := owner.Get(fixture.server.URL + "/v1/workers/" + fixture.worker.WorkerRef)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			approvals, ok := decoded["approvals"].([]any)
			if !ok || len(approvals) != 1 {
				t.Fatalf("owner_refresh_status=%d approval_present=%t", response.StatusCode, ok && len(approvals) == 1)
			}
			approval, ok := approvals[0].(map[string]any)
			if !ok {
				t.Fatal("owner refresh Approval DTO missing")
			}
			approvalIDVisible := approval["id"] == fixture.approval.ID
			stateVisible := approval["state"] == string(core.ApprovalResolving)
			decisionVisible := approval["resolution_state"] == string(decision)
			privateResponseField := hasJSONKey(approval, "response") || hasJSONKey(approval, "resolution_response")
			privateCommandField := hasJSONKey(approval, "resolution_command_id") || hasJSONKey(approval, "command_id")
			privateActorField := hasJSONKey(approval, "request_id") || hasJSONKey(approval, "resolved_by") || hasJSONKey(approval, "resolution_resolved_by")
			responseMarkerLeaked := strings.Contains(string(body), savedResponse)
			commandMarkerLeaked := strings.Contains(string(body), fixture.command.ID)
			if response.StatusCode != http.StatusOK || !approvalIDVisible || !stateVisible || !decisionVisible || privateResponseField || privateCommandField || privateActorField || responseMarkerLeaked || commandMarkerLeaked {
				t.Fatalf("owner_refresh_ok=%t approval_id_visible=%t resolving_visible=%t decision_visible=%t response_field=%t command_field=%t actor_field=%t response_marker=%t command_marker=%t", response.StatusCode == http.StatusOK, approvalIDVisible, stateVisible, decisionVisible, privateResponseField, privateCommandField, privateActorField, responseMarkerLeaked, commandMarkerLeaked)
			}
		})
	}
}

type approvalRouteAuthFixture struct {
	ctx          context.Context
	store        *core.Store
	api          *Server
	server       *httptest.Server
	personID     string
	conversation core.Conversation
	worker       core.Worker
	turn         core.Turn
	attempt      core.Phase4Attempt
	approval     core.Approval
	command      core.WorkerCommand
	runtime      *approvalRouteAuthRuntime
}

func newApprovalRouteAuthFixture(t *testing.T, decision core.ApprovalState, savedResponse string) *approvalRouteAuthFixture {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "approval-auth-refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	personID := api.OwnerID()
	conversation, err := store.ConversationForPerson(ctx, personID)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, personID)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "approval-auth-refresh-project", Name: "Approval auth refresh", Mappings: []core.ProjectPathMapping{{Node: "auth-node", Path: t.TempDir()}}})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "approval-auth-refresh-worker", Intent: "permission request", ProjectID: project.ID,
		NodeID: "auth-node", HarnessInstanceID: "auth-node/fx", PolicySnapshot: "private-policy-marker",
	}, core.TurnSpec{Input: "permission request", ContextSnapshot: "private-context-marker"})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		store.Close()
		t.Fatal(err)
	}
	const requestID = "approval-auth-refresh-request"
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{
		EventID: "approval-auth-refresh-event", Node: "auth-node", HarnessInstanceID: "auth-node/fx", WorkerRef: worker.WorkerRef,
		TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
	}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: requestID, Summary: "synthetic permission"}}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	command, _, err := store.ClaimWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, _, err := store.BeginApprovalResolution(ctx, requestID, command.ID, decision, "owner-session", savedResponse); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandUncertain(ctx, command.ID, "synthetic unresolved handoff"); err != nil {
		store.Close()
		t.Fatal(err)
	}
	approval, err := store.Approval(ctx, requestID)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	runtime := &approvalRouteAuthRuntime{}
	api.AttachWorkerResponder(ctl.WorkerService{Store: store, PersonID: personID, Capability: capability, Runtime: runtime})
	server := httptest.NewServer(api.Handler())
	fixture := &approvalRouteAuthFixture{
		ctx: ctx, store: store, api: api, server: server, personID: personID, conversation: conversation,
		worker: worker, turn: turn, attempt: attempt, approval: approval, command: command, runtime: runtime,
	}
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return fixture
}

func pairApprovalMutationClient(t *testing.T, owner *http.Client, server *httptest.Server, scope, deviceID string) (string, string) {
	t.Helper()
	paired := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"`+deviceID+`","display_name":"Approval mutation client","platform":"test","scopes":["`+scope+`"]}`)
	clientID, ok := paired.body["client_id"].(string)
	if paired.status != http.StatusCreated || !ok || clientID == "" {
		t.Fatalf("client_pair_status=%d client_id_present=%t", paired.status, ok && clientID != "")
	}
	approved := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/approve", `{}`)
	credential, ok := approved.body["credential"].(string)
	if approved.status != http.StatusOK || !ok || credential == "" {
		t.Fatalf("client_approve_status=%d credential_present=%t", approved.status, ok && credential != "")
	}
	return clientID, credential
}

func assertApprovalRouteFixtureUnchanged(t *testing.T, fixture *approvalRouteAuthFixture) {
	t.Helper()
	approval, approvalErr := fixture.store.Approval(fixture.ctx, fixture.approval.RequestID)
	command, found, commandErr := fixture.store.FindWorkerCommand(fixture.ctx, "respond", "request:"+fixture.approval.RequestID, fixture.worker.ID, fixture.attempt.ID)
	unchanged := approvalErr == nil && commandErr == nil && found && approval.State == core.ApprovalResolving && approval.ResolutionState == fixture.approval.ResolutionState && command.ID == fixture.command.ID && command.State == core.WorkerCommandUncertain
	if !unchanged || fixture.runtime.calls.Load() != 0 {
		t.Fatalf("approval_state_unchanged=%t runtime_calls_zero=%t", unchanged, fixture.runtime.calls.Load() == 0)
	}
}

func hasOwnerApprovalMutationDTO(raw []byte) bool {
	return bytes.Contains(raw, []byte(`"worker":`)) || bytes.Contains(raw, []byte(`"turns":`)) || bytes.Contains(raw, []byte(`"approvals":`))
}

func hasJSONKey(value any, key string) bool {
	switch current := value.(type) {
	case map[string]any:
		for childKey, child := range current {
			if childKey == key || hasJSONKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if hasJSONKey(child, key) {
				return true
			}
		}
	}
	return false
}

type approvalRouteAuthRuntime struct{ calls atomic.Int32 }

func (*approvalRouteAuthRuntime) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error {
	return nil
}
func (*approvalRouteAuthRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return nil
}
func (r *approvalRouteAuthRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	r.calls.Add(1)
	return nil
}
func (*approvalRouteAuthRuntime) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error {
	return nil
}
func (*approvalRouteAuthRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return nil
}
