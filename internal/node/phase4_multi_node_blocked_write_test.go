package node

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestBlockedNodeWriteDoesNotBlockOtherNodeDispatchHeartbeatOrReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{
		PairingTokens: []string{"pair-node-a", "pair-node-b"}, AdminToken: "admin-token",
	})
	if err != nil { t.Fatal(err) }
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()

	nodeA, err := EnrollNode(ctx, server.Client(), server.URL, "pair-node-a", "blocked-node")
	if err != nil { t.Fatal(err) }
	nodeB, err := EnrollNode(ctx, server.Client(), server.URL, "pair-node-b", "healthy-node")
	if err != nil { t.Fatal(err) }
	connectionA := dialEnrolledNode(t, ctx, nodeA, daemonInventoryFixture(nodeA.Node), "blocked-first")
	defer connectionA.Close()
	connectionB := dialEnrolledNode(t, ctx, nodeB, daemonInventoryFixture(nodeB.Node), "healthy-first")
	defer connectionB.Close()
	waitFor(t, ctx, "both independent Node connections", func() bool {
		onlineA, errA := manager.Status(ctx, nodeA.Node)
		onlineB, errB := manager.Status(ctx, nodeB.Node)
		return errA == nil && errB == nil && onlineA.Online && onlineB.Online
	})

	manager.mu.Lock()
	serverConnectionA := manager.connections[nodeA.Node]
	manager.mu.Unlock()
	if serverConnectionA == nil { t.Fatal("server socket for blocked Node is missing") }
	serverConnectionA.writes.Lock()
	var releaseWriter sync.Once
	release := func() { releaseWriter.Do(func() { serverConnectionA.writes.Unlock() }) }
	defer release()

	enteredWrite := make(chan struct{})
	manager.beforeCommandWrite = func(nodeRef core.NodeReference) {
		if nodeRef == nodeA.Node { close(enteredWrite) }
	}
	original := daemonDispatchFixture(nodeA.Node, "blocked-node-dispatch")
	originalDone := make(chan error, 1)
	go func() { originalDone <- manager.SendCommand(ctx, nodeA.Node, original) }()
	select {
	case <-enteredWrite:
	case <-ctx.Done():
		t.Fatal("blocked Node never entered the command write")
	}
	select {
	case err := <-originalDone:
		t.Fatalf("blocked Node unexpectedly finished its write: %v", err)
	default:
	}

	independentCtx, stopIndependent := context.WithTimeout(ctx, 4*time.Second)
	defer stopIndependent()
	firstHeartbeat := Heartbeat{
		Node: nodeB.Node, Online: true, Capacity: 2, Inventory: daemonInventoryFixture(nodeB.Node),
	}
	if err := connectionB.SendHeartbeat(independentCtx, firstHeartbeat, 1); err != nil {
		t.Fatalf("healthy Node heartbeat blocked by another Node's write: %v", err)
	}
	waitFor(t, independentCtx, "healthy Node heartbeat during blocked write", func() bool {
		status, err := manager.Status(independentCtx, nodeB.Node)
		return err == nil && !status.LastHeartbeatAt.IsZero() && status.Capacity == 2
	})
	ready, err := manager.CommandReady(independentCtx, nodeB.Node)
	if err != nil || !ready {
		t.Fatalf("healthy Node not command ready while other Node is blocked: ready=%v err=%v", ready, err)
	}
	first := daemonDispatchFixture(nodeB.Node, "healthy-first-dispatch")
	if err := manager.SendCommand(independentCtx, nodeB.Node, first); err != nil {
		t.Fatalf("Dispatch to independent healthy Node blocked: %v", err)
	}
	received, err := connectionB.ReceiveCommand(independentCtx)
	if err != nil || received.Metadata().CommandID != first.Metadata().CommandID {
		t.Fatalf("wrong Dispatch on independent Node: %#v err=%v", received, err)
	}

	if err := connectionB.Close(); err != nil { t.Fatal(err) }
	waitFor(t, independentCtx, "healthy Node disconnected independently", func() bool {
		status, err := manager.Status(independentCtx, nodeB.Node)
		return err == nil && !status.Online
	})
	reconnected := dialEnrolledNode(t, independentCtx, nodeB, daemonInventoryFixture(nodeB.Node), "healthy-reconnect")
	defer reconnected.Close()
	if err := reconnected.SendHeartbeat(independentCtx, firstHeartbeat, 1); err != nil {
		t.Fatalf("healthy Node reconnect heartbeat blocked: %v", err)
	}
	waitFor(t, independentCtx, "healthy Node restored after reconnect", func() bool {
		ready, err := manager.CommandReady(independentCtx, nodeB.Node)
		return err == nil && ready
	})
	second := daemonDispatchFixture(nodeB.Node, "healthy-reconnected-dispatch")
	if err := manager.SendCommand(independentCtx, nodeB.Node, second); err != nil {
		t.Fatalf("Dispatch after independent reconnect blocked: %v", err)
	}
	received, err = reconnected.ReceiveCommand(independentCtx)
	if err != nil || received.Metadata().CommandID != second.Metadata().CommandID {
		t.Fatalf("healthy Node reconnect lost command ownership: %#v err=%v", received, err)
	}

	canceledCtx, stopCanceled := context.WithTimeout(ctx, 40*time.Millisecond)
	defer stopCanceled()
	if _, err := manager.CommandReady(canceledCtx, nodeA.Node); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait for busy Node ownership did not respect cancellation: %v", err)
	}
	select {
	case err := <-originalDone:
		t.Fatalf("blocked Node A sent before writer was released: %v", err)
	default:
	}

	release()
	select {
	case err := <-originalDone:
		if err != nil { t.Fatalf("original Node write failed after release: %v", err) }
	case <-ctx.Done():
		t.Fatal("blocked Node did not complete after write was unblocked")
	}
	received, err = connectionA.ReceiveCommand(ctx)
	if err != nil || received.Metadata().CommandID != original.Metadata().CommandID {
		t.Fatalf("original Node lost its own Dispatch: %#v err=%v", received, err)
	}
}
