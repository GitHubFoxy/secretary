package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestNodePairingTokenIsOneTimeAndCredentialsAreDistinct(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"node-pair"}, AdminToken: "node-admin"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(manager)
	defer server.Close()

	first, err := EnrollNode(ctx, server.Client(), server.URL, "node-pair", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnrollNode(ctx, server.Client(), server.URL, "node-pair", "second"); err == nil {
		t.Fatal("pairing token was reusable")
	}
	if first.Credential == "" {
		t.Fatal("pairing returned empty Node credential")
	}
	record, err := store.NodeRecord(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	if record.CredentialHash == "" || record.CredentialHash == "node-pair" {
		t.Fatalf("Node credential hash=%q", record.CredentialHash)
	}
	if first.Credential == "node-pair" {
		t.Fatal("pairing returned the pairing token as Node credential")
	}
}

func TestNodeWatchdogMarksSilentSocketOffline(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := time.Now().UTC()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{
		PairingTokens: []string{"pair"}, AdminToken: "admin", HeartbeatTimeout: time.Minute,
		Now: func() time.Time { return clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	// The registry seam is deterministic and does not require a live socket.
	if _, err := store.EnrollNodeWithPairing(ctx, "pair", "silent", bytesOf(32, 'x')); err != nil {
		t.Fatal(err)
	}
	inventory := daemonInventoryFixture("silent")
	if err := store.MarkNodeConnected(ctx, "silent", inventory); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Second)
	if err := manager.WatchdogTick(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(ctx, "silent")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Online {
		t.Fatal("Node went offline before heartbeat timeout")
	}
	clock = clock.Add(time.Minute)
	if err := manager.WatchdogTick(ctx); err != nil {
		t.Fatal(err)
	}
	status, err = manager.Status(ctx, "silent")
	if err != nil {
		t.Fatal(err)
	}
	if status.Online {
		t.Fatal("silent Node remained online after watchdog timeout")
	}
}

func TestStoreEventSinkDurablyAcceptsActivityAndTerminalOutcome(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpecForNode()
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, core.TurnSpec{Input: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureNodeRegistry(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "node-a"); err != nil {
		t.Fatal(err)
	}
	fixture := daemonInventoryFixture("node-a").Instances[0]
	if err := store.MarkNodeConnected(ctx, "node-a", daemonInventoryFixture("node-a")); err != nil {
		t.Fatal(err)
	}
	sink := NewStoreEventSink(store)
	activity := core.Activity{Metadata: core.ActivityMetadata{EventID: "activity-1", Node: "node-a", HarnessInstanceID: fixture.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityStatus, Status: "working"}
	activityEvent := NodeEvent{EventID: "activity-1", Node: "node-a", Kind: "activity", Activity: &activity}
	if err := sink(ctx, activityEvent); err != nil {
		t.Fatal(err)
	}
	outcome := core.AttemptOutcomeEnvelope{EventID: "outcome-1", Node: "node-a", HarnessInstanceID: fixture.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done", OccurredAt: time.Now().UTC()}
	if err := sink(ctx, NodeEvent{EventID: "outcome-1", Node: "node-a", Kind: "attempt.outcome", Outcome: &outcome}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Body != "done" {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestTerminalOutcomeUsesProductionSinkAndReachesConversation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"terminal-pair"}, AdminToken: "terminal-admin"})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetEventSink(NewStoreEventSink(store))
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "terminal-pair", "terminal-node")
	if err != nil {
		t.Fatal(err)
	}
	localStore, err := OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	daemon := &Daemon{Identity: identity, Store: localStore, Runtime: terminalDaemonRuntime{}, Inventory: daemonStaticInventory{snapshot: daemonInventoryFixture("terminal-node")}, HeartbeatInterval: 20 * time.Millisecond, InventoryInterval: time.Hour, OutboxPollInterval: 10 * time.Millisecond, ReconnectMin: 10 * time.Millisecond, ReconnectMax: 30 * time.Millisecond}
	daemonCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() { _ = daemon.Run(daemonCtx) }()
	waitFor(t, ctx, "terminal Node online", func() bool {
		status, statusErr := manager.Status(context.Background(), "terminal-node")
		return statusErr == nil && status.Online
	})
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := core.WorkerSpec{WorkerRef: "terminal-worker", Title: "terminal", Intent: "run", ProjectID: "project", NodeID: "terminal-node", HarnessInstanceID: "terminal-node/fx"}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, core.TurnSpec{Input: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture("terminal-node").Instances[0]
	command := Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{Metadata: core.CommandMetadata{CommandID: "terminal-command", Node: "terminal-node", HarnessInstanceID: instance.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC()}, Envelope: WorkerEnvelope{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, OriginalUserIntent: "run", Workspace: tWorkspace, HarnessInstance: instance}}}
	if err := manager.SendCommand(ctx, "terminal-node", command); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "terminal conversation entry", func() bool {
		entries, entriesErr := store.EntriesAfter(context.Background(), conversation.ID, 0)
		return entriesErr == nil && len(entries) == 1 && entries[0].Body == "terminal result"
	})
}

func TestNodeServiceRejectsSharedPairingAndAdminCredential(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"same"}, AdminToken: "same"}); err == nil {
		t.Fatal("Node pairing and admin credentials were allowed to be identical")
	}
	if _, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"client-bootstrap"}, AdminToken: "node-admin", ClientBootstrapToken: "client-bootstrap"}); err == nil {
		t.Fatal("Client bootstrap token was accepted as Node pairing credential")
	}
}

type terminalDaemonRuntime struct{}

func (terminalDaemonRuntime) Start(context.Context, StartRequest) (Session, error) {
	activity := make(chan Activity)
	close(activity)
	results := make(chan Result, 1)
	results <- Result{Status: "succeeded", Summary: "terminal result"}
	close(results)
	return &daemonSession{id: "terminal-native", activity: activity, results: results}, nil
}

func phase4WorkerSpecForNode() core.WorkerSpec {
	return core.WorkerSpec{WorkerRef: "worker-node", Title: "node", Intent: "run", ProjectID: "project", NodeID: "node-a", HarnessInstanceID: "node-a/fx"}
}

func bytesOf(n int, value byte) []byte {
	result := make([]byte, n)
	for i := range result {
		result[i] = value
	}
	return result
}
