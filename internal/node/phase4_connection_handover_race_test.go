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

func TestLeaseHandoverDispatchWaitsForCommittedConnectionOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	manager.SetEventSink(func(context.Context, NodeEvent) error { return nil })
	outcomes := make(chan CommandOutcome, 4)
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
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "handover-node")
	if err != nil { t.Fatal(err) }

	makeDaemon := func(version string, heartbeat time.Duration) (*Daemon, *daemonCountingRuntime, *LocalStore) {
		t.Helper()
		local, err := OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
		if err != nil { t.Fatal(err) }
		runtime := &daemonCountingRuntime{}
		inventory := daemonInventoryFixture(identity.Node)
		inventory.Instances[0].Version = version
		return &Daemon{
			Identity: identity, Store: local, Runtime: runtime,
			Inventory: daemonStaticInventory{snapshot: inventory},
			Capacity: 1, HeartbeatInterval: heartbeat,
			InventoryInterval: time.Hour, OutboxPollInterval: 20*time.Millisecond,
			ReconnectMin: 30*time.Millisecond, ReconnectMax: 50*time.Millisecond,
		}, runtime, local
	}
	owner, ownerRuntime, ownerLocal := makeDaemon("owner-version", time.Hour)
	defer ownerLocal.Close()
	replacement, replacementRuntime, replacementLocal := makeDaemon("replacement-version", 30*time.Millisecond)
	defer replacementLocal.Close()

	ownerCtx, stopOwner := context.WithCancel(ctx)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- owner.Run(ownerCtx) }()
	defer func() {
		stopOwner()
		select {
		case err := <-ownerDone: if err != nil { t.Errorf("owner daemon: %v", err) }
		case <-time.After(2*time.Second): t.Error("owner daemon did not stop")
		}
	}()
	waitFor(t, ctx, "original Node lease", func() bool {
		ready, err := manager.CommandReady(ctx, identity.Node)
		if err != nil || !ready { return false }
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && len(status.Inventory.Instances) == 1 &&
			status.Inventory.Instances[0].Version == "owner-version"
	})
	manager.mu.Lock()
	oldSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	if oldSocket == nil { t.Fatal("original Node socket missing") }

	manager.now = func() time.Time { return time.Now().UTC().Add(time.Hour) }
	if err := manager.WatchdogTick(ctx); err != nil { t.Fatal(err) }
	record, err := manager.Status(ctx, identity.Node)
	if err != nil || record.Online { t.Fatalf("Node lease did not expire: %#v err=%v", record, err) }

	admissionReached := make(chan struct{})
	releaseAdmission := make(chan struct{})
	defer func() { select { case <-releaseAdmission: default: close(releaseAdmission) } }()
	manager.beforeConnectionRegistration = func() {
		close(admissionReached)
		<-releaseAdmission
	}
	cloneCtx, stopClone := context.WithCancel(ctx)
	cloneDone := make(chan error, 1)
	go func() { cloneDone <- replacement.Run(cloneCtx) }()
	defer func() {
		stopClone()
		select {
		case err := <-cloneDone: if err != nil { t.Errorf("replacement daemon: %v", err) }
		case <-time.After(2*time.Second): t.Error("replacement daemon did not stop")
		}
	}()
	select {
	case <-admissionReached:
	case <-ctx.Done(): t.Fatal("replacement never reached paused admission")
	}
	intermediate, err := manager.Status(ctx, identity.Node)
	if err != nil || !intermediate.Online {
		t.Fatalf("Node was not marked online before connection registration: status=%#v err=%v", intermediate, err)
	}
	manager.mu.Lock()
	stillOld := manager.connections[identity.Node]
	manager.mu.Unlock()
	if stillOld != oldSocket { t.Fatal("old socket was replaced before paused registration") }

	dispatch := daemonDispatchFixture(identity.Node, "handover-race-dispatch")
	newInventory := daemonInventoryFixture(identity.Node)
	newInventory.Instances[0].Version = "replacement-version"
	dispatch.Dispatch.Envelope.HarnessInstance = newInventory.Instances[0]
	dispatch.Dispatch.Envelope.Profile = workerTemplateFixture(newInventory.Instances[0], "", "")
	sendStarted := make(chan struct{})
	sent := make(chan error, 1)
	go func() {
		close(sendStarted)
		sent <- manager.SendCommand(ctx, identity.Node, dispatch)
	}()
	<-sendStarted
	select {
	case err := <-sent:
		t.Fatalf("Dispatch raced ahead of the owned socket registration: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if ownerRuntime.starts.Load() != 0 || replacementRuntime.starts.Load() != 0 {
		t.Fatalf("native action began while ownership was uncommitted: old=%d new=%d",
			ownerRuntime.starts.Load(), replacementRuntime.starts.Load())
	}

	close(releaseAdmission)
	select {
	case err := <-sent:
		if err != nil { t.Fatalf("Dispatch did not use newly committed socket: %v", err) }
	case <-ctx.Done(): t.Fatal("Dispatch blocked after replacement completed")
	}
	outcome := waitCommandOutcome(t, ctx, outcomes)
	if outcome.CommandID != dispatch.Metadata().CommandID || outcome.State != CommandAccepted {
		t.Fatalf("replacement did not accept exactly the intended Dispatch: %#v", outcome)
	}
	if ownerRuntime.starts.Load() != 0 || replacementRuntime.starts.Load() != 1 {
		t.Fatalf("Dispatch was executed by the wrong native owner or repeated: original=%d new=%d",
			ownerRuntime.starts.Load(), replacementRuntime.starts.Load())
	}
	manager.mu.Lock()
	currentSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	if currentSocket == nil || currentSocket == oldSocket {
		t.Fatal("replacement Node did not retain exclusive socket ownership")
	}
}
