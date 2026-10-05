package webapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestApprovalRetryResponsesForApprovalWriteOnlyClientAreStrictlyAllowlisted(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "approval-retry-scope.db"))
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
	capability, err := store.RotateSecretaryCapability(ctx, personID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "approval-retry-scope-project", Name: "Approval retry privacy", Mappings: []core.ProjectPathMapping{{Node: "native-node-private-marker", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}

	const (
		requestID           = "approval-retry-scope-request"
		savedResponse       = "saved-response-private-marker"
		contextMarker       = "context-snapshot-private-marker"
		policyMarker        = "policy-snapshot-private-marker"
		profileMarker       = "profile-private-marker"
		projectMarker       = "project-snapshot-private-marker"
		workspaceMarker     = "workspace-private-marker"
		nativeNodeMarker    = "native-node-private-marker"
		nativeHarnessMarker = "native-session-private-marker"
	)
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "approval-retry-scope-worker", Intent: "permission request", ProjectID: project.ID,
		NodeID: nativeNodeMarker, HarnessInstanceID: nativeHarnessMarker,
		PolicySnapshot: policyMarker + " " + profileMarker, ProjectSnapshot: projectMarker, Workspace: workspaceMarker,
	}, core.TurnSpec{Input: "permission request", ContextSnapshot: contextMarker})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{
		EventID: "approval-retry-scope-event", Node: core.NodeReference(nativeNodeMarker),
		HarnessInstanceID: core.HarnessInstanceID(nativeHarnessMarker), WorkerRef: worker.WorkerRef,
		TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
	}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: requestID, Summary: "synthetic permission"}}); err != nil {
		t.Fatal(err)
	}
	approval, err := store.Approval(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := store.ClaimWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.BeginApprovalResolution(ctx, requestID, command.ID, core.ApprovalDenied, "owner-session", savedResponse); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandUncertain(ctx, command.ID, "synthetic interrupted handoff"); err != nil {
		t.Fatal(err)
	}

	runtime := &approvalRetryScopeRuntime{}
	api.AttachWorkerResponder(ctl.WorkerService{Store: store, PersonID: personID, Capability: capability, Runtime: runtime})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	credential := pairApprovalWriteOnlyCredential(t, server)
	client := &http.Client{}
	for _, path := range []string{"/v1/workers", "/v1/approvals"} {
		request, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+credential)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("write-only credential read status=%d", response.StatusCode)
		}
	}

	markers := []string{savedResponse, contextMarker, policyMarker, profileMarker, projectMarker, workspaceMarker, nativeNodeMarker, nativeHarnessMarker, command.ID}
	postApprovalAction := func(action, idempotencyKey string) ([]byte, int) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/approvals/"+approval.ID+"/"+action, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set("Idempotency-Key", idempotencyKey)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return body, response.StatusCode
	}
	postRetry := func(idempotencyKey string) ([]byte, int) {
		return postApprovalAction("retry", idempotencyKey)
	}
	assertAllowlisted := func(body []byte) {
		t.Helper()
		details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(sanitizePublicJSON(publicWorkerDetailsStrictFromDetails(details)))
		if err != nil {
			t.Fatal(err)
		}
		var actualValue, expectedValue any
		if json.Unmarshal(body, &actualValue) != nil || json.Unmarshal(expected, &expectedValue) != nil {
			t.Fatal("retry response JSON decode failed")
		}
		markerLeaks := make([]bool, len(markers))
		for i, marker := range markers {
			markerLeaks[i] = strings.Contains(string(body), marker)
		}
		allowlistMatch := reflect.DeepEqual(actualValue, expectedValue)
		forbiddenFields := hasUnapprovedApprovalRetryField(actualValue)
		leakedMarker := false
		for _, leaks := range markerLeaks {
			leakedMarker = leakedMarker || leaks
		}
		if !allowlistMatch || forbiddenFields || leakedMarker {
			t.Fatalf("allowlist_match=%t forbidden_fields=%t marker_leaks=%v", allowlistMatch, forbiddenFields, markerLeaks)
		}
	}

	// First call retries the resolving intent; the same ledger key exercises
	// the cached response branch, and a fresh key exercises resolved no-handoff.
	body, status := postRetry("scope-retry-once")
	if status != http.StatusOK {
		t.Fatalf("pending retry status=%d", status)
	}
	assertAllowlisted(body)
	if runtime.calls.Load() != 1 {
		t.Fatalf("pending retry handoffs=%d", runtime.calls.Load())
	}
	duplicateBody, duplicateStatus := postRetry("scope-retry-once")
	if duplicateStatus != http.StatusOK {
		t.Fatalf("duplicate idempotency response status=%d", duplicateStatus)
	}
	assertAllowlisted(duplicateBody)
	if runtime.calls.Load() != 1 {
		t.Fatalf("duplicate idempotency handoffs=%d", runtime.calls.Load())
	}
	resolvedBody, resolvedStatus := postRetry("scope-retry-already-resolved")
	if resolvedStatus != http.StatusOK {
		t.Fatalf("resolved no-handoff response status=%d", resolvedStatus)
	}
	assertAllowlisted(resolvedBody)
	if runtime.calls.Load() != 1 {
		t.Fatalf("resolved retry handoffs=%d", runtime.calls.Load())
	}
	relatedBody, relatedStatus := postApprovalAction("deny", "scope-resolved-deny")
	if relatedStatus != http.StatusOK {
		t.Fatalf("resolved deny response status=%d", relatedStatus)
	}
	assertAllowlisted(relatedBody)
	if runtime.calls.Load() != 1 {
		t.Fatalf("resolved deny handoffs=%d", runtime.calls.Load())
	}
}

func pairApprovalWriteOnlyCredential(t *testing.T, server *httptest.Server) string {
	t.Helper()
	paired := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"approval-write-only","display_name":"Approval writer","platform":"test","scopes":["approval:write"]}`)
	if paired.status != http.StatusCreated || paired.body["client_id"] == nil {
		t.Fatalf("narrow client pairing status=%d", paired.status)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := &http.Client{Jar: jar}
	login(t, owner, server.URL)
	approved := postJSON(t, owner, server.URL+"/v1/clients/"+paired.body["client_id"].(string)+"/approve", `{}`)
	credential, ok := approved.body["credential"].(string)
	if approved.status != http.StatusOK || !ok || credential == "" {
		t.Fatalf("narrow client approval status=%d credential_present=%t", approved.status, ok && credential != "")
	}
	return credential
}

func hasUnapprovedApprovalRetryField(value any) bool {
	forbidden := map[string]struct{}{
		"context_snapshot": {}, "policy_snapshot": {}, "project_snapshot": {}, "workspace": {},
		"profile": {}, "profile_snapshot": {}, "node_id": {}, "harness_instance_id": {},
		"runtime_session_id": {}, "native_session_id": {}, "native_id": {},
		"response": {}, "resolution_response": {}, "resolution_command_id": {}, "command_id": {},
	}
	var walk func(any) bool
	walk = func(current any) bool {
		switch node := current.(type) {
		case map[string]any:
			for key, child := range node {
				if _, blocked := forbidden[key]; blocked || walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range node {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}

type approvalRetryScopeRuntime struct{ calls atomic.Int32 }

func (*approvalRetryScopeRuntime) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error {
	return nil
}
func (*approvalRetryScopeRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return nil
}
func (r *approvalRetryScopeRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	r.calls.Add(1)
	return nil
}
func (*approvalRetryScopeRuntime) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error {
	return nil
}
func (*approvalRetryScopeRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return nil
}
