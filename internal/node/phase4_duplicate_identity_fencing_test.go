package node

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestDuplicateDaemonIdentityCannotDisplaceLiveOwnerOrReceiveNativeCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serverStore, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer serverStore.Close()
	manager, err := NewServerManager(ctx, serverStore, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	manager.SetEventSink(func(context.Context, NodeEvent) error { return nil })
	outcomes := make(chan CommandOutcome, 8)
	manager.SetCommandOutcomeSink(func(_ context.Context, _ core.NodeReference, outcome CommandOutcome) error {
		outcomes <- outcome
		return nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "cloned-node")
	if err != nil { t.Fatal(err) }
	workspacePaths := map[string]string{
		"owner": filepath.Join(t.TempDir(), "owner-workspace"),
		"copy": filepath.Join(t.TempDir(), "copy-workspace"),
	}
	for name, path := range workspacePaths {
		if err := os.MkdirAll(path, 0o700); err != nil { t.Fatal(err) }
		if _, err := serverStore.CreateProject(ctx, core.ProjectSpec{
			ID: name + "-project", Name: name + " project",
			Mappings: []core.ProjectPathMapping{{Node: identity.Node, Path: path}},
		}); err != nil { t.Fatal(err) }
	}

	makeDaemon := func(name, version string) (*Daemon, *daemonCountingRuntime, *LocalStore) {
		t.Helper()
		directory := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(directory, 0o700); err != nil { t.Fatal(err) }
		identityPath := filepath.Join(directory, "identity.json")
		if err := SaveNodeIdentity(identityPath, identity); err != nil { t.Fatal(err) }
		copiedIdentity, err := LoadNodeIdentity(identityPath)
		if err != nil || copiedIdentity.Node != identity.Node || copiedIdentity.Credential != identity.Credential {
			t.Fatalf("identity copy failed: node=%#v err=%v", copiedIdentity.Node, err)
		}
		state, err := OpenLocalStore(filepath.Join(directory, "node-state.json"))
		if err != nil { t.Fatal(err) }
		runtime := &daemonCountingRuntime{}
		inventory := daemonInventoryFixture(identity.Node)
		inventory.Instances[0].Version = version
		daemon := &Daemon{
			Identity: copiedIdentity, Store: state, Runtime: runtime,
			Inventory: daemonStaticInventory{snapshot: inventory},
			Workspaces: []Workspace{{ProjectID: name + "-project", Path: workspacePaths[name]}},
			Capacity: 1, HeartbeatInterval: 30*time.Millisecond,
			InventoryInterval: time.Second, OutboxPollInterval: 20*time.Millisecond,
			ReconnectMin: 30*time.Millisecond, ReconnectMax: 60*time.Millisecond,
		}
		return daemon, runtime, state
	}
	owner, ownerRuntime, ownerState := makeDaemon("owner", "owner-v1")
	defer ownerState.Close()
	clone, cloneRuntime, cloneState := makeDaemon("copy", "clone-v2")
	defer cloneState.Close()
	ownerCtx, stopOwner := context.WithCancel(ctx)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- owner.Run(ownerCtx) }()
	ownerStopped := false
	defer func() {
		stopOwner()
		if !ownerStopped {
			select {
			case err := <-ownerDone: if err != nil { t.Errorf("owner daemon: %v", err) }
			case <-time.After(2*time.Second): t.Error("owner daemon did not stop")
			}
		}
	}()
	waitFor(t, ctx, "first daemon authenticated and command-ready", func() bool {
		ready, err := manager.CommandReady(ctx, identity.Node)
		if err != nil || !ready { return false }
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && len(status.Inventory.Instances) == 1 && status.Inventory.Instances[0].Version == "owner-v1"
	})
	manager.mu.Lock()
	originalSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	if originalSocket == nil { t.Fatal("first daemon has no registered connection") }
	initialRecord, err := serverStore.NodeRecord(ctx, identity.Node)
	if err != nil || len(initialRecord.Workspaces) != 1 ||
		initialRecord.Workspaces[0].ProjectID != "owner-project" ||
		initialRecord.Workspaces[0].Path != workspacePaths["owner"] {
		t.Fatalf("original Node workspace is not durable: %#v err=%v", initialRecord.Workspaces, err)
	}

	cloneCtx, stopClone := context.WithCancel(ctx)
	cloneDone := make(chan error, 1)
	go func() { cloneDone <- clone.Run(cloneCtx) }()
	defer func() {
		stopClone()
		select {
		case err := <-cloneDone: if err != nil { t.Errorf("duplicate daemon: %v", err) }
		case <-time.After(2*time.Second): t.Error("duplicate daemon did not stop")
		}
	}()

	time.Sleep(200 * time.Millisecond)
	manager.mu.Lock()
	retainedSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	status, err := manager.Status(ctx, identity.Node)
	if err != nil || retainedSocket != originalSocket || !status.Online ||
		len(status.Inventory.Instances) != 1 || status.Inventory.Instances[0].Version != "owner-v1" ||
		len(status.Workspaces) != 1 || status.Workspaces[0].ProjectID != "owner-project" ||
		status.Workspaces[0].Path != workspacePaths["owner"] {
		t.Fatalf("copied Node identity displaced the live owner: status=%#v err=%v", status, err)
	}
	retainedRecord, err := serverStore.NodeRecord(ctx, identity.Node)
	if err != nil || len(retainedRecord.Workspaces) != 1 ||
		retainedRecord.Workspaces[0] != initialRecord.Workspaces[0] {
		t.Fatalf("duplicate identity changed persisted Node workspace: %#v err=%v", retainedRecord.Workspaces, err)
	}

	dispatch := daemonDispatchFixture(identity.Node, "original-owned-dispatch")
	dispatch.Dispatch.Envelope.HarnessInstance = status.Inventory.Instances[0]
	dispatch.Dispatch.Envelope.Profile = workerTemplateFixture(dispatch.Dispatch.Envelope.HarnessInstance, "", "")
	if err := manager.SendCommand(ctx, identity.Node, dispatch); err != nil { t.Fatal(err) }
	received := waitCommandOutcome(t, ctx, outcomes)
	if received.CommandID != dispatch.Metadata().CommandID || received.State != CommandAccepted {
		t.Fatalf("original Node did not accept its native command: %#v", received)
	}
	if ownerRuntime.starts.Load() != 1 || cloneRuntime.starts.Load() != 0 {
		t.Fatalf("native execution routed to cloned Node: owner=%d clone=%d", ownerRuntime.starts.Load(), cloneRuntime.starts.Load())
	}
	if _, mapped := cloneState.SessionMapping(dispatch.Metadata().AttemptID); mapped {
		t.Fatal("rejected clone acquired original native session mapping")
	}

	stopOwner()
	select {
	case err := <-ownerDone: if err != nil { t.Fatalf("first Node shutdown: %v", err) }
		ownerStopped = true
	case <-ctx.Done(): t.Fatal("first Node never released its connection")
	}
	waitFor(t, ctx, "duplicate takes ownership only after original disconnect", func() bool {
		ready, err := manager.CommandReady(ctx, identity.Node)
		if err != nil || !ready { return false }
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && len(status.Inventory.Instances) == 1 &&
			status.Inventory.Instances[0].Version == "clone-v2" &&
			len(status.Workspaces) == 1 && status.Workspaces[0].ProjectID == "copy-project" &&
			status.Workspaces[0].Path == workspacePaths["copy"]
	})

	meta := dispatch.Metadata()
	meta.CommandID = "foreign-native-session-resume"
	meta.TurnID = "foreign-turn"
	meta.AttemptID = "foreign-attempt"
	meta.IssuedAt = time.Now().UTC()
	envelope := dispatch.Dispatch.Envelope
	envelope.TurnID = meta.TurnID
	envelope.AttemptID = meta.AttemptID
	envelope.PreviousAttemptID = dispatch.Metadata().AttemptID
	envelope.ContinuationPrompt = "Continue the original native session"
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: meta, Envelope: envelope}}
	if err := manager.SendCommand(ctx, identity.Node, resume); err != nil { t.Fatal(err) }
	denied := waitCommandOutcome(t, ctx, outcomes)
	if denied.CommandID != meta.CommandID || denied.State != CommandFailed || denied.ErrorCode != "runtime_session_unavailable" {
		t.Fatalf("cloned Node without original durable mapping resumed native execution: %#v", denied)
	}
	if cloneRuntime.starts.Load() != 0 || ownerRuntime.starts.Load() != 1 {
		t.Fatalf("native action was repeated across identity handover: old=%d copied=%d", ownerRuntime.starts.Load(), cloneRuntime.starts.Load())
	}
}

func TestExpiredNodeLeaseFencesStillRegisteredSocketWithoutRepeatingNativeAction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverStore, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer serverStore.Close()
	manager, err := NewServerManager(ctx, serverStore, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	manager.SetEventSink(func(context.Context, NodeEvent) error { return nil })
	outcomes := make(chan CommandOutcome, 8)
	manager.SetCommandOutcomeSink(func(_ context.Context, _ core.NodeReference, outcome CommandOutcome) error {
		outcomes <- outcome
		return nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "expired-node")
	if err != nil { t.Fatal(err) }
	create := func(label, version string, heartbeat time.Duration) (*Daemon, *daemonCountingRuntime, *LocalStore) {
		t.Helper()
		directory := filepath.Join(t.TempDir(), label)
		state, err := OpenLocalStore(filepath.Join(directory, "node-state.json"))
		if err != nil { t.Fatal(err) }
		runtime := &daemonCountingRuntime{}
		inventory := daemonInventoryFixture(identity.Node)
		inventory.Instances[0].Version = version
		return &Daemon{
			Identity: identity, Store: state, Runtime: runtime,
			Inventory: daemonStaticInventory{snapshot: inventory},
			Capacity: 1, HeartbeatInterval: heartbeat,
			InventoryInterval: time.Hour, OutboxPollInterval: 25*time.Millisecond,
			ReconnectMin: 30*time.Millisecond, ReconnectMax: 50*time.Millisecond,
		}, runtime, state
	}
	original, originalRuntime, originalState := create("original", "original-version", time.Hour)
	defer originalState.Close()
	clone, cloneRuntime, cloneState := create("clone", "clone-version", 50*time.Millisecond)
	defer cloneState.Close()
	originalCtx, stopOriginal := context.WithCancel(ctx)
	originalDone := make(chan error, 1)
	go func() { originalDone <- original.Run(originalCtx) }()
	originalStopped := false
	defer func() {
		stopOriginal()
		if !originalStopped {
			select {
			case err := <-originalDone: if err != nil { t.Errorf("original daemon: %v", err) }
			case <-time.After(2*time.Second): t.Error("original daemon did not stop")
			}
		}
	}()
	waitFor(t, ctx, "original Node with lease and command socket", func() bool {
		ready, err := manager.CommandReady(ctx, identity.Node)
		if err != nil || !ready { return false }
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && len(status.Inventory.Instances) == 1 &&
			status.Inventory.Instances[0].Version == "original-version"
	})
	manager.mu.Lock()
	staleSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	if staleSocket == nil { t.Fatal("original Node socket missing") }
	dispatch := daemonDispatchFixture(identity.Node, "lease-original-dispatch")
	observed, err := manager.Status(ctx, identity.Node)
	if err != nil || len(observed.Inventory.Instances) != 1 ||
		observed.Inventory.Instances[0].Version != "original-version" {
		t.Fatalf("original harness inventory unavailable for Dispatch: %#v err=%v", observed.Inventory, err)
	}
	dispatch.Dispatch.Envelope.HarnessInstance = observed.Inventory.Instances[0]
	dispatch.Dispatch.Envelope.Profile = workerTemplateFixture(dispatch.Dispatch.Envelope.HarnessInstance, "", "")
	if err := manager.SendCommand(ctx, identity.Node, dispatch); err != nil { t.Fatal(err) }
	accepted := waitCommandOutcome(t, ctx, outcomes)
	if accepted.CommandID != dispatch.Metadata().CommandID || accepted.State != CommandAccepted ||
		originalRuntime.starts.Load() != 1 || cloneRuntime.starts.Load() != 0 {
		t.Fatalf("original native command ownership is wrong: receipt=%#v old=%d cloned=%d",
			accepted, originalRuntime.starts.Load(), cloneRuntime.starts.Load())
	}
	manager.now = func() time.Time { return time.Now().UTC().Add(time.Hour) }
	if err := manager.WatchdogTick(ctx); err != nil { t.Fatal(err) }
	status, err := manager.Status(ctx, identity.Node)
	if err != nil || status.Online {
		t.Fatalf("expired heartbeat lease was not marked offline: status=%#v err=%v", status, err)
	}
	manager.mu.Lock()
	stillRegistered := manager.connections[identity.Node]
	manager.mu.Unlock()
	if stillRegistered != staleSocket {
		t.Fatal("expired lease test lost original socket before replacement")
	}
	cloneCtx, stopClone := context.WithCancel(ctx)
	cloneDone := make(chan error, 1)
	go func() { cloneDone <- clone.Run(cloneCtx) }()
	defer func() {
		stopClone()
		select {
		case err := <-cloneDone: if err != nil { t.Errorf("clone daemon: %v", err) }
		case <-time.After(2*time.Second): t.Error("clone daemon did not stop")
		}
	}()
	waitFor(t, ctx, "lease-expiry replacement authenticated", func() bool {
		ready, err := manager.CommandReady(ctx, identity.Node)
		if err != nil || !ready { return false }
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && len(status.Inventory.Instances) == 1 &&
			status.Inventory.Instances[0].Version == "clone-version"
	})
	manager.mu.Lock()
	replacement := manager.connections[identity.Node]
	manager.mu.Unlock()
	if replacement == nil || replacement == staleSocket {
		t.Fatal("expired lease did not fence original authenticated socket")
	}
	staleHandler := &serverProtocolHandler{manager: manager, expected: identity.Node, connection: staleSocket}
	if err := staleHandler.HandleNodeHeartbeat(ctx, Heartbeat{
		Node: identity.Node, Online: true, Capacity: 1, Inventory: daemonInventoryFixture(identity.Node),
	}); !errors.Is(err, errStaleNodeConnection) {
		t.Fatalf("old socket restored liveness after fencing: %v", err)
	}
	cloneRecord, err := serverStore.NodeRecord(ctx, identity.Node)
	if err != nil || len(cloneRecord.Inventory.Instances) != 1 ||
		cloneRecord.Inventory.Instances[0].Version != "clone-version" {
		t.Fatalf("old socket altered replacement inventory: %#v err=%v", cloneRecord, err)
	}
	meta := dispatch.Metadata()
	meta.CommandID = "expired-lease-cross-directory-resume"
	meta.TurnID = "expired-lease-followup"
	meta.AttemptID = "expired-lease-attempt"
	meta.IssuedAt = time.Now().UTC()
	envelope := dispatch.Dispatch.Envelope
	envelope.TurnID = meta.TurnID
	envelope.AttemptID = meta.AttemptID
	envelope.PreviousAttemptID = dispatch.Metadata().AttemptID
	envelope.ContinuationPrompt = "Continue existing native session"
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: meta, Envelope: envelope}}
	if err := manager.SendCommand(ctx, identity.Node, resume); err != nil { t.Fatal(err) }
	denied := waitCommandOutcome(t, ctx, outcomes)
	if denied.CommandID != meta.CommandID || denied.State != CommandFailed || denied.ErrorCode != "runtime_session_unavailable" {
		t.Fatalf("clone resumed absent native session mapping: %#v", denied)
	}
	if originalRuntime.starts.Load() != 1 || cloneRuntime.starts.Load() != 0 {
		t.Fatalf("duplicate native action after expired lease: original=%d cloned=%d",
			originalRuntime.starts.Load(), cloneRuntime.starts.Load())
	}
	stopOriginal()
	select {
	case err := <-originalDone: if err != nil { t.Fatal(err) }
		originalStopped = true
	case <-ctx.Done(): t.Fatal("original Node daemon did not stop")
	}
}
