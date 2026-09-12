package webapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestApprovalAPIRejectsWrongClientAuthentication(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "approval-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/v1/approvals")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong client status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestApprovalAPIDenyRespondsExactlyOnceBeforeDurableFinalization(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "approval-deny.db"))
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
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "repo", Name: "Repo", Mappings: []core.ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{ID: "node/fx", Node: "node", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}}}
	if _, err := store.EnrollNode(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", core.HarnessInventorySnapshot{Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	runtime := &apiApprovalRuntime{}
	service := ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: runtime}
	details, err := service.SpawnWorker(ctx, ctl.SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: "deny-spawn"})
	if err != nil {
		t.Fatal(err)
	}
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{EventID: "deny-permission", Node: "node", HarnessInstanceID: "node/fx", WorkerRef: details.Worker.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "deny-request", Summary: "run shell"}}); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(service)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login(t, client, server.URL)
	for range 2 {
		response, err := client.Post(server.URL+"/v1/approvals/deny-request/deny", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("deny status=%d", response.StatusCode)
		}
		response.Body.Close()
	}
	if runtime.responds != 1 || len(runtime.requestIDs) != 1 || runtime.requestIDs[0] != "deny-request" {
		t.Fatalf("Respond calls=%d requestIDs=%v", runtime.responds, runtime.requestIDs)
	}
	approval, err := store.Approval(ctx, "deny-request")
	if err != nil || approval.State != core.ApprovalDenied {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
	details, err = store.WorkerDetailsForConversation(ctx, conversation.ID, details.Worker.WorkerRef)
	if err != nil || details.Worker.Status != core.WorkerIdle || len(details.Outcomes) != 1 || len(details.Results) != 1 {
		t.Fatalf("final details=%#v err=%v", details, err)
	}
}

type apiApprovalRuntime struct {
	responds   int
	requestIDs []string
}

func (r *apiApprovalRuntime) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error {
	return nil
}
func (r *apiApprovalRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return nil
}
func (r *apiApprovalRuntime) Respond(_ context.Context, _ string, _ core.Worker, _ core.Phase4Attempt, requestID, _ string) error {
	r.responds++
	r.requestIDs = append(r.requestIDs, requestID)
	return nil
}
func (r *apiApprovalRuntime) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error {
	return nil
}
func (r *apiApprovalRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return nil
}
