package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestDaemonPairHeartbeatDrainReconnectReplayDeduplicateAndRevoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	serverStore, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	manager, err := NewServerManager(ctx, serverStore, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	identity, err := EnrollNode(ctx, httpServer.Client(), httpServer.URL, "pair-token", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Node != "macbook" || !strings.HasPrefix(identity.ConnectURL, "ws://") {
		t.Fatalf("identity=%#v", identity)
	}

	inventory := daemonInventoryFixture("macbook")
	wrongHandshake := Handshake{Node: "macbook", ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: "wrong-credential"}
	if connection, err := DialProtocol(ctx, identity.ConnectURL, "macbook", NewAuthenticator([]byte("admin-token")), wrongHandshake); err == nil {
		_ = connection.Close()
		t.Fatal("server accepted a non-Node/admin credential for Node protocol")
	}

	localStore, err := OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	runtime := &daemonCountingRuntime{}
	daemon := &Daemon{
		Identity: identity,
		Store: localStore,
		Runtime: runtime,
		Inventory: daemonStaticInventory{snapshot: inventory},
		Capacity: 2,
		HeartbeatInterval: 20 * time.Millisecond,
		InventoryInterval: time.Hour,
		OutboxPollInterval: 10 * time.Millisecond,
		ReconnectMin: 10 * time.Millisecond,
		ReconnectMax: 40 * time.Millisecond,
	}
	daemonCtx, stopDaemon := context.WithCancel(ctx)
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

	waitFor(t, ctx, "Node heartbeat", func() bool {
		status, err := manager.Status(context.Background(), "macbook")
		return err == nil && status.Online && !status.LastHeartbeatAt.IsZero() && status.Inventory.Node == "macbook" && len(status.Inventory.Instances) == 1
	})

	unauthorized, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/nodes", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := httpServer.Client().Do(unauthorized)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Node control accepted missing admin credential: %d", response.StatusCode)
	}

	if status := setDrainHTTP(t, ctx, httpServer, "macbook", true); !status.Draining || !status.Online {
		t.Fatalf("drain status=%#v", status)
	}
	command := daemonDispatchFixture("macbook", "dispatch-1")
	if err := manager.SendCommand(ctx, "macbook", command); err == nil || !strings.Contains(err.Error(), "draining") {
		t.Fatalf("dispatch on draining Node err=%v", err)
	}
	if starts := runtime.starts.Load(); starts != 0 {
		t.Fatalf("draining Node started %d runtimes", starts)
	}
	if status := setDrainHTTP(t, ctx, httpServer, "macbook", false); status.Draining {
		t.Fatalf("undrain status=%#v", status)
	}

	outcomes := make(chan CommandOutcome, 4)
	manager.SetCommandOutcomeSink(func(_ context.Context, nodeRef core.NodeReference, outcome CommandOutcome) error {
		if nodeRef == "macbook" {
			outcomes <- outcome
		}
		return nil
	})
	if err := manager.SendCommand(ctx, "macbook", command); err != nil {
		t.Fatal(err)
	}
	first := waitCommandOutcome(t, ctx, outcomes)
	if first.CommandID != "dispatch-1" || first.State != CommandAccepted {
		t.Fatalf("first command outcome=%#v", first)
	}
	if err := manager.SendCommand(ctx, "macbook", command); err != nil {
		t.Fatal(err)
	}
	second := waitCommandOutcome(t, ctx, outcomes)
	if second.CommandID != first.CommandID || second.State != first.State {
		t.Fatalf("duplicate command outcome=%#v first=%#v", second, first)
	}
	if starts := runtime.starts.Load(); starts != 1 {
		t.Fatalf("duplicate command started %d runtimes", starts)
	}
	mapping, ok := localStore.SessionMapping("attempt-1")
	if !ok || mapping.RuntimeSessionID == "" {
		t.Fatalf("native runtime session mapping not persisted: %#v ok=%v", mapping, ok)
	}

	firstDelivery := make(chan NodeEvent, 1)
	manager.SetEventSink(func(_ context.Context, event NodeEvent) error {
		firstDelivery <- event
		return errors.New("simulate network loss before acknowledgement")
	})
	replayOutcome := core.AttemptOutcomeEnvelope{
		EventID: "replay-me", Node: "macbook", HarnessInstanceID: "macbook/fx",
		WorkerRef: "worker-replay", TurnID: "turn-replay", AttemptID: "attempt-replay",
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done", OccurredAt: time.Now().UTC(),
	}
	if _, err := localStore.QueueOutcome(replayOutcome); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-firstDelivery:
		if event.EventID != "replay-me" {
			t.Fatalf("first outbox event=%#v", event)
		}
	case <-ctx.Done():
		t.Fatal("outbox event was not delivered before simulated loss")
	}
	pending, err := localStore.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].EventID != "replay-me" {
		t.Fatalf("unacknowledged outbox was not retained: %#v", pending)
	}

	replayed := make(chan NodeEvent, 1)
	manager.SetEventSink(func(_ context.Context, event NodeEvent) error {
		replayed <- event
		return nil
	})
	select {
	case event := <-replayed:
		if event.EventID != "replay-me" {
			t.Fatalf("replayed event=%#v", event)
		}
	case <-ctx.Done():
		t.Fatal("Node did not reconnect and replay its outbox")
	}
	waitFor(t, ctx, "outbox acknowledgement", func() bool {
		pending, err := localStore.PendingEvents()
		return err == nil && len(pending) == 0
	})
	waitFor(t, ctx, "authenticated reconnect", func() bool {
		status, err := manager.Status(context.Background(), "macbook")
		return err == nil && status.Online && status.Inventory.Node == "macbook"
	})
	if starts := runtime.starts.Load(); starts != 1 {
		t.Fatalf("reconnect restarted existing Attempt; starts=%d", starts)
	}

	revoked := revokeHTTP(t, ctx, httpServer, "macbook")
	if !revoked.Revoked || revoked.Online || !revoked.Draining {
		t.Fatalf("revoke status=%#v", revoked)
	}
	waitFor(t, ctx, "revoked Node stays offline", func() bool {
		status, err := manager.Status(context.Background(), "macbook")
		return err == nil && status.Revoked && !status.Online
	})
	if err := manager.SendCommand(ctx, "macbook", command); !errors.Is(err, core.ErrNodeRevoked) {
		t.Fatalf("command to revoked Node err=%v", err)
	}
}

func TestNodeIdentityPersistsWithRestrictedPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.json")
	identity := NodeIdentity{Node: "home-server", Credential: "QUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUFBQUE", ConnectURL: "ws://127.0.0.1:8081/v1/nodes/connect?node=home-server"}
	if err := SaveNodeIdentity(path, identity); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadNodeIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != identity {
		t.Fatalf("loaded identity=%#v want=%#v", loaded, identity)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity permissions=%#o", info.Mode().Perm())
	}
}

type daemonStaticInventory struct{ snapshot core.HarnessInventorySnapshot }

func (s daemonStaticInventory) Discover(context.Context) (core.HarnessInventorySnapshot, error) {
	copy := s.snapshot
	copy.ObservedAt = time.Now().UTC()
	return copy, nil
}

type daemonCountingRuntime struct{ starts atomic.Int32 }

func (r *daemonCountingRuntime) Start(_ context.Context, request StartRequest) (Session, error) {
	r.starts.Add(1)
	activity := make(chan Activity)
	results := make(chan Result)
	close(activity)
	close(results)
	return &daemonSession{id: "native-" + request.WorkerRef, activity: activity, results: results}, nil
}

type daemonSession struct {
	id       string
	activity chan Activity
	results  chan Result
}

func (s *daemonSession) ID() string                                  { return s.id }
func (s *daemonSession) Prompt(context.Context, string) error        { return nil }
func (s *daemonSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (s *daemonSession) Cancel(context.Context) error                { return nil }
func (s *daemonSession) Activity() <-chan Activity                   { return s.activity }
func (s *daemonSession) Result() <-chan Result                       { return s.results }
func (s *daemonSession) Close() error                                { return nil }

func daemonInventoryFixture(nodeRef core.NodeReference) core.HarnessInventorySnapshot {
	return core.HarnessInventorySnapshot{
		Node: nodeRef,
		ObservedAt: time.Now().UTC(),
		Instances: []core.HarnessInstance{{
			ID: core.HarnessInstanceID(string(nodeRef) + "/fx"), Node: nodeRef, Kind: core.HarnessFX, Version: "1.2.3",
			Authentication: core.HarnessAuthentication{Authenticated: true, Method: "local"}, Status: core.HarnessReady,
			Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}, Activity: []core.ActivityCapability{core.ActivityStatus}},
		}},
	}
}

func daemonDispatchFixture(nodeRef core.NodeReference, commandID string) Command {
	instance := daemonInventoryFixture(nodeRef).Instances[0]
	return Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{
		Metadata: core.CommandMetadata{
			CommandID: commandID, Node: nodeRef, HarnessInstanceID: instance.ID,
			WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1", IssuedAt: time.Now().UTC(),
		},
		Envelope: WorkerEnvelope{
			WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1",
			OriginalUserIntent: "inspect repository", Workspace: tWorkspace, HarnessInstance: instance,
		},
	}}
}

func waitCommandOutcome(t *testing.T, ctx context.Context, outcomes <-chan CommandOutcome) CommandOutcome {
	t.Helper()
	select {
	case outcome := <-outcomes:
		return outcome
	case <-ctx.Done():
		t.Fatal("timed out waiting for command outcome")
		return CommandOutcome{}
	}
}

func waitFor(t *testing.T, ctx context.Context, name string, condition func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", name)
		case <-ticker.C:
		}
	}
}

func setDrainHTTP(t *testing.T, ctx context.Context, server *httptest.Server, nodeRef string, draining bool) ServerNodeStatus {
	t.Helper()
	body := strings.NewReader(`{"draining":` + map[bool]string{true: "true", false: "false"}[draining] + `}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/nodes/"+nodeRef+"/drain", body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer admin-token")
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("drain HTTP status=%d", response.StatusCode)
	}
	var status ServerNodeStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func revokeHTTP(t *testing.T, ctx context.Context, server *httptest.Server, nodeRef string) ServerNodeStatus {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/nodes/"+nodeRef+"/revoke", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer admin-token")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("revoke HTTP status=%d", response.StatusCode)
	}
	var status ServerNodeStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	return status
}
