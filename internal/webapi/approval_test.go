package webapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

func TestApprovalAPIDenyUsesRealNodeRuntimeBeforeFinalization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "approval-node.db"))
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
	manager, err := node.NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	manager.SetEventSink(node.NewStoreEventSink(store))
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	identity, err := node.EnrollNode(ctx, httpServer.Client(), httpServer.URL, "pair-token", "node")
	if err != nil {
		t.Fatal(err)
	}
	inventory := core.HarnessInventorySnapshot{Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{{
		ID: "node/fx", Node: "node", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities:   core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}, Activity: []core.ActivityCapability{core.ActivityPermissionRequest}},
	}}}
	localStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	runtime := &denyNodeRuntime{}
	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemon := &node.Daemon{Identity: identity, Store: localStore, Runtime: runtime, Inventory: staticNodeInventory{snapshot: inventory}, Capacity: 1, HeartbeatInterval: 200 * time.Millisecond, InventoryInterval: time.Hour, OutboxPollInterval: 10 * time.Millisecond, ReconnectMin: 10 * time.Millisecond, ReconnectMax: 40 * time.Millisecond}
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		select {
		case err := <-daemonDone:
			if err != nil {
				t.Errorf("daemon exit: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("daemon did not stop")
		}
	}()
	if !waitForWebAPI(t, ctx, func() bool {
		status, statusErr := manager.Status(context.Background(), "node")
		return statusErr == nil && status.Online && !status.LastHeartbeatAt.IsZero()
	}) {
		t.Fatal("Node did not connect")
	}
	time.Sleep(20 * time.Millisecond)
	service := ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: ctl.NodeRuntime{Manager: manager}}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(service)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	details, err := service.SpawnWorker(ctx, ctl.SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, NodeID: "node", HarnessKind: core.HarnessFX, IdempotencyKey: "node-deny-spawn"})
	if err != nil {
		t.Fatal(err)
	}
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	binding, err := store.ResolveWorkerBinding(ctx, details.Worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := details.Turns[0]
	if err := (ctl.NodeRuntime{Manager: manager}).Dispatch(ctx, "acceptance-dispatch", details.Worker, turn, attempt, core.DispatchResolution{ProjectDispatch: binding}); err != nil {
		status, statusErr := manager.Status(context.Background(), "node")
		t.Fatalf("dispatch error=%v status=%#v statusErr=%v", err, status, statusErr)
	}
	if !waitForWebAPI(t, ctx, func() bool {
		approval, approvalErr := store.Approval(ctx, "node-deny-request")
		return approvalErr == nil && approval.State == core.ApprovalPending
	}) {
		current, currentErr := service.GetWorker(ctx, details.Worker.WorkerRef)
		status, statusErr := manager.Status(context.Background(), "node")
		t.Fatalf("approval not observed: worker=%#v workerErr=%v node=%#v nodeErr=%v", current, currentErr, status, statusErr)
	}
	for range 2 {
		response, postErr := client.Post(server.URL+"/v1/approvals/node-deny-request/deny", "application/json", nil)
		if postErr != nil {
			t.Fatal(postErr)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			t.Fatalf("deny status=%d", response.StatusCode)
		}
		response.Body.Close()
	}
	if got := runtime.responds.Load(); got != 1 {
		t.Fatalf("Node.respond_worker handoffs=%d", got)
	}
	approval, err := store.Approval(ctx, "node-deny-request")
	if err != nil || approval.State != core.ApprovalDenied {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
	final, err := store.WorkerDetailsForConversation(ctx, conversation.ID, details.Worker.WorkerRef)
	if err != nil || final.Worker.Status != core.WorkerIdle || len(final.Outcomes) != 1 || len(final.Results) != 1 {
		t.Fatalf("final details=%#v err=%v", final, err)
	}
}

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

type staticNodeInventory struct{ snapshot core.HarnessInventorySnapshot }

func (s staticNodeInventory) Discover(context.Context) (core.HarnessInventorySnapshot, error) {
	return s.snapshot, nil
}

type denyNodeRuntime struct{ responds atomic.Int32 }

func (r *denyNodeRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	activity := make(chan node.Activity, 1)
	activity <- node.Activity{Kind: node.ActivityPermission, RequestID: "node-deny-request", Summary: "run shell"}
	return &denyNodeSession{activity: activity, results: make(chan node.Result), responds: &r.responds}, nil
}

type denyNodeSession struct {
	activity chan node.Activity
	results  chan node.Result
	responds *atomic.Int32
}

func (s *denyNodeSession) ID() string                                { return "node-native-session" }
func (*denyNodeSession) Prompt(context.Context, string) error        { return nil }
func (*denyNodeSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (*denyNodeSession) Cancel(context.Context) error                { return nil }
func (s *denyNodeSession) Activity() <-chan node.Activity            { return s.activity }
func (s *denyNodeSession) Result() <-chan node.Result                { return s.results }
func (*denyNodeSession) Close() error                                { return nil }
func (s *denyNodeSession) Respond(_ context.Context, requestID, response string) error {
	if requestID != "node-deny-request" || response != "denied" {
		return errors.New("unexpected Node response")
	}
	s.responds.Add(1)
	return nil
}

func waitForWebAPI(t *testing.T, ctx context.Context, condition func() bool) bool {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func mustWebCookieJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
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
