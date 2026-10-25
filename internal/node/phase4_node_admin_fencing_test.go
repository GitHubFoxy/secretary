package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestForceRevokePreemptsBlockedCommandWriteBeforeDurableCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "revoke-locked-node")
	if err != nil { t.Fatal(err) }
	connected := dialEnrolledNode(t, ctx, identity, daemonInventoryFixture(identity.Node), "revoke-locked-first")
	defer connected.Close()
	waitFor(t, ctx, "authenticated revocable Node", func() bool {
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && status.Online
	})
	manager.mu.Lock()
	serverSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	if serverSocket == nil { t.Fatal("Node socket missing") }
	serverSocket.writes.Lock()
	released := false
	defer func() {
		if !released { serverSocket.writes.Unlock() }
	}()
	started := make(chan struct{})
	manager.beforeCommandWrite = func(node core.NodeReference) {
		if node == identity.Node { close(started) }
	}
	dispatch := daemonDispatchFixture(identity.Node, "revoke-pending-dispatch")
	commandDone := make(chan error, 1)
	go func() { commandDone <- manager.SendCommand(ctx, identity.Node, dispatch) }()
	select {
	case <-started:
	case <-ctx.Done(): t.Fatal("Dispatch did not reach blocked write")
	}
	select {
	case err := <-commandDone:
		t.Fatalf("blocked Dispatch unexpectedly completed: %v", err)
	default:
	}
	revokeDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/nodes/"+string(identity.Node)+"/revoke", strings.NewReader(`{"force":true}`)).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer admin-token")
		recorder := httptest.NewRecorder()
		manager.ServeHTTP(recorder, req)
		revokeDone <- recorder
	}()
	select {
	case result := <-revokeDone:
		if result.Code != http.StatusOK {
			t.Fatalf("force revoke failed while write lock was held: status=%d body=%q", result.Code, result.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("force revoke waited for the blocked WebSocket write mutex")
	}
	record, err := store.NodeRecord(ctx, identity.Node)
	if err != nil || !record.Revoked || record.Online {
		t.Fatalf("durable revocation was not committed before write release: record=%#v err=%v", record, err)
	}
	select {
	case err := <-commandDone:
		if !errors.Is(err, ErrCommandOutcomeUnknown) {
			t.Fatalf("pending Dispatch was not marked uncertain after socket abort: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("force revoke failed to interrupt the blocked command send")
	}
	serverSocket.writes.Unlock()
	released = true
	readCtx, stopRead := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stopRead()
	unexpected, readErr := connected.ReceiveCommand(readCtx)
	if readErr == nil {
		t.Fatalf("revoked Node received Dispatch after durable commit: %#v", unexpected)
	}
	if err := manager.SendCommand(ctx, identity.Node, daemonDispatchFixture(identity.Node, "after-revoke")); !errors.Is(err, core.ErrNodeRevoked) {
		t.Fatalf("revoked identity accepted a new Dispatch: %v", err)
	}
	if _, err := store.NodeCredential(ctx, identity.Node); err != nil {
		t.Fatalf("force revoke removed durable identity credentials: %v", err)
	}
}

func TestDrainingWaitsForInFlightWriteBeforeDurableTransition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "draining-locked-node")
	if err != nil { t.Fatal(err) }
	connected := dialEnrolledNode(t, ctx, identity, daemonInventoryFixture(identity.Node), "draining-locked-first")
	defer connected.Close()
	waitFor(t, ctx, "draining Node connection", func() bool {
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && status.Online
	})
	manager.mu.Lock()
	serverSocket := manager.connections[identity.Node]
	manager.mu.Unlock()
	if serverSocket == nil { t.Fatal("Node socket missing") }
	serverSocket.writes.Lock()
	released := false
	defer func() {
		if !released { serverSocket.writes.Unlock() }
	}()
	writeStarted := make(chan struct{})
	manager.beforeCommandWrite = func(node core.NodeReference) {
		if node == identity.Node { close(writeStarted) }
	}
	dispatch := daemonDispatchFixture(identity.Node, "before-drain")
	sendDone := make(chan error, 1)
	go func() { sendDone <- manager.SendCommand(ctx, identity.Node, dispatch) }()
	select {
	case <-writeStarted:
	case <-ctx.Done(): t.Fatal("Dispatch did not reach blocked socket")
	}
	drainStarted := make(chan struct{})
	drainDone := make(chan error, 1)
	go func() {
		close(drainStarted)
		_, err := manager.setNodeDraining(ctx, identity.Node, true)
		drainDone <- err
	}()
	<-drainStarted
	waitFor(t, ctx, "drain waiter shares existing identity gate", func() bool {
		manager.nodeLifecycleMu.Lock()
		gate := manager.nodeLifecycleGates[identity.Node]
		waiters := 0
		if gate != nil { waiters = gate.refs }
		manager.nodeLifecycleMu.Unlock()
		return waiters == 2
	})
	record, err := store.NodeRecord(ctx, identity.Node)
	if err != nil || record.Draining {
		t.Fatalf("draining committed before pending Dispatch resolved: record=%#v err=%v", record, err)
	}
	serverSocket.writes.Unlock()
	released = true
	select {
	case err := <-sendDone:
		if err != nil { t.Fatalf("original Dispatch failed: %v", err) }
	case <-ctx.Done(): t.Fatal("Dispatch did not complete")
	}
	if received, err := connected.ReceiveCommand(ctx); err != nil || received.Metadata().CommandID != dispatch.Metadata().CommandID {
		t.Fatalf("original Dispatch not delivered: %#v err=%v", received, err)
	}
	select {
	case err := <-drainDone:
		if err != nil { t.Fatalf("drain could not commit after Dispatch: %v", err) }
	case <-ctx.Done(): t.Fatal("drain did not complete")
	}
	record, err = store.NodeRecord(ctx, identity.Node)
	if err != nil || !record.Draining {
		t.Fatalf("drain state did not persist: record=%#v err=%v", record, err)
	}
	if err := manager.SendCommand(ctx, identity.Node, daemonDispatchFixture(identity.Node, "after-drain")); err == nil || !strings.Contains(err.Error(), "draining") {
		t.Fatalf("new Dispatch passed committed drain transition: %v", err)
	}
}

func TestNodeLifecycleGateReleasesUnknownAndRetiredIdentityEntries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	for i := 0; i < 300; i++ {
		unknown := core.NodeReference(fmt.Sprintf("never-enrolled-%03d", i))
		if _, err := manager.CommandReady(ctx, unknown); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("unknown Node %s readiness: %v", unknown, err)
		}
		if err := manager.SendCommand(ctx, unknown, daemonDispatchFixture(unknown, "missing-dispatch")); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("unknown Node %s command routing: %v", unknown, err)
		}
	}
	manager.nodeLifecycleMu.Lock()
	count := len(manager.nodeLifecycleGates)
	manager.nodeLifecycleMu.Unlock()
	if count != 0 {
		t.Fatalf("unknown Node identities retained %d lifetime gates", count)
	}
	_, err = store.EnrollNode(ctx, "retired-node")
	if err != nil { t.Fatal(err) }
	held, err := manager.lockNodeLifecycle(ctx, "retired-node")
	if err != nil { t.Fatal(err) }
	waitCtx, stopWait := context.WithCancel(ctx)
	defer stopWait()
	waitStarted := make(chan struct{})
	waitDone := make(chan error, 1)
	go func() {
		close(waitStarted)
		unlock, err := manager.lockNodeLifecycle(waitCtx, "retired-node")
		if err == nil { unlock() }
		waitDone <- err
	}()
	<-waitStarted
	waitFor(t, ctx, "two lifetime-gate references", func() bool {
		manager.nodeLifecycleMu.Lock()
		gate := manager.nodeLifecycleGates["retired-node"]
		refs := 0
		if gate != nil { refs = gate.refs }
		manager.nodeLifecycleMu.Unlock()
		return refs == 2
	})
	stopWait()
	select {
	case err := <-waitDone:
		if !errors.Is(err, context.Canceled) { t.Fatalf("canceled gate waiter returned %v", err) }
	case <-ctx.Done(): t.Fatal("canceled waiter did not stop")
	}
	manager.nodeLifecycleMu.Lock()
	gate := manager.nodeLifecycleGates["retired-node"]
	refs := 0
	if gate != nil { refs = gate.refs }
	manager.nodeLifecycleMu.Unlock()
	if refs != 1 {
		t.Fatalf("canceled waiter deleted gate still held by another request: %d", refs)
	}
	held()
	if _, err := manager.revokeNode(ctx, "retired-node", true); err != nil {
		t.Fatalf("retired Node could not be revoked: %v", err)
	}
	if _, err := manager.CommandReady(ctx, "retired-node"); !errors.Is(err, core.ErrNodeRevoked) {
		t.Fatalf("retired Node was not rejected: %v", err)
	}
	manager.nodeLifecycleMu.Lock()
	count = len(manager.nodeLifecycleGates)
	manager.nodeLifecycleMu.Unlock()
	if count != 0 {
		t.Fatalf("retired Node retained %d lifecycle gates", count)
	}
	record, err := store.NodeRecord(ctx, "retired-node")
	if err != nil || !record.Revoked {
		t.Fatalf("retired Node lost durable revoked state: record=%#v err=%v", record, err)
	}
}
