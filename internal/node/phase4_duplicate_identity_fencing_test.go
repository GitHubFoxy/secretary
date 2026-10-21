package node

import (
	"context"
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

	// The copied identity actively attempts to connect while the original owns
	// its authenticated Node socket. It must not replace the original owner.
	time.Sleep(200 * time.Millisecond)
	manager.mu.Lock()
	retainedSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	status, err := manager.Status(ctx, identity.Node)
	if err != nil || retainedSocket != originalSocket || !status.Online ||
		len(status.Inventory.Instances) != 1 || status.Inventory.Instances[0].Version != "owner-v1" {
		t.Fatalf("copied Node identity displaced the live owner: status=%#v err=%v", status, err)
	}

	dispatch := daemonDispatchFixture(identity.Node, "original-owned-dispatch")
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
		return err == nil && len(status.Inventory.Instances) == 1 && status.Inventory.Instances[0].Version == "clone-v2"
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
