package node

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"
    "time"

    "github.com/beruseruko/secretary/internal/core"
)

func TestReplacementSocketInvalidatesWorkerUntilMatchingHeartbeat(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
    defer cancel()
    store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
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
    identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "macbook")
    if err != nil { t.Fatal(err) }
    auth, err := identity.Authenticator()
    if err != nil { t.Fatal(err) }
    inventory := daemonInventoryFixture("macbook")
    connect := func(nonce string) *ProtocolConnection {
        t.Helper()
        conn, err := DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{
            Node: identity.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: nonce,
        })
        if err != nil { t.Fatal(err) }
        waitFor(t, ctx, "registered socket", func() bool {
            status, err := manager.Status(ctx, identity.Node)
            return err == nil && status.Online && status.LastHeartbeatAt.IsZero()
        })
        return conn
    }
    original := connect("original-socket")
    defer original.Close()
    _, conversation, err := store.CreatePersonWithConversation(ctx)
    if err != nil { t.Fatal(err) }
    worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
        WorkerRef: "replacement-worker", ProjectID: "project", NodeID: "macbook",
        HarnessInstanceID: "macbook/fx", Intent: "run native operation",
    }, core.TurnSpec{Input: "run"})
    if err != nil { t.Fatal(err) }
    if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
    matching := []core.NodeActiveAttempt{{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID}}
    if err := original.SendHeartbeat(ctx, Heartbeat{
        Node: identity.Node, Online: true, Capacity: 1, Inventory: inventory, ActiveAttempts: matching,
    }, 1); err != nil { t.Fatal(err) }
    waitFor(t, ctx, "old connection heartbeat", func() bool {
        record, err := store.NodeRecord(ctx, identity.Node)
        return err == nil && !record.LastHeartbeatAt.IsZero() && len(record.ActiveAttempts) == 1
    })

    // Simulate a decoded heartbeat from the old socket that is already
    // in flight, but pauses before it reaches the Store. A replacement
    // must invalidate this handler even if its old socket hasn't called
    // unregisterConnection yet.
    manager.mu.Lock()
    oldServerSocket := manager.connections[identity.Node]
    manager.mu.Unlock()
    if oldServerSocket == nil { t.Fatal("old server socket was not registered") }
    staleHandler := &serverProtocolHandler{
        manager: manager, expected: identity.Node,
        inventory: inventory, connection: oldServerSocket,
    }
    staleReady := make(chan struct{})
    releaseStale := make(chan struct{})
    staleDone := make(chan error, 1)
    go func() {
        close(staleReady)
        <-releaseStale
        staleDone <- staleHandler.HandleNodeHeartbeat(ctx, Heartbeat{
            Node: identity.Node, Online: true, Capacity: 1, Inventory: inventory,
            ActiveAttempts: matching,
        })
    }()
    <-staleReady

    // Deliberately open a second authenticated socket BEFORE the old one has
    // disconnected. Its registration must make the existing Worker offline.
    replacement := connect("replacement-socket")
    defer replacement.Close()
    close(releaseStale)
    select {
    case err := <-staleDone:
        if !errors.Is(err, errStaleNodeConnection) {
            t.Fatalf("old socket updated liveness after replacement: %v", err)
        }
    case <-ctx.Done():
        t.Fatal("in-flight stale heartbeat was not rejected")
    }
    if err := staleHandler.HandleNodeInventory(ctx, inventory); !errors.Is(err, errStaleNodeConnection) {
        t.Fatalf("old socket updated inventory after replacement: %v", err)
    }
    details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
    if err != nil { t.Fatal(err) }
    record, err := store.NodeRecord(ctx, identity.Node)
    if err != nil { t.Fatal(err) }
    if !record.LastHeartbeatAt.IsZero() || record.Capacity != 0 || len(record.ActiveAttempts) != 0 {
        t.Fatalf("stale heartbeat contaminated fresh socket telemetry: %#v", record)
    }
    if details.Worker.Status != core.WorkerOffline || details.CurrentAttempt().State != core.AttemptActive {
        t.Fatalf("Worker remained actionable on replaced native socket: %#v", details)
    }

    if err := replacement.SendHeartbeat(ctx, Heartbeat{
        Node: identity.Node, Online: true, Capacity: 1, Inventory: inventory,
    }, 1); err != nil { t.Fatal(err) }
    waitFor(t, ctx, "heartbeat without bound Attempt", func() bool {
        record, err := store.NodeRecord(ctx, identity.Node)
        return err == nil && !record.LastHeartbeatAt.IsZero() && len(record.ActiveAttempts) == 0
    })
    details, err = store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
    if err != nil { t.Fatal(err) }
    if details.Worker.Status != core.WorkerOffline { t.Fatalf("unmatched heartbeat revived Worker: %s", details.Worker.Status) }

    if err := replacement.SendHeartbeat(ctx, Heartbeat{
        Node: identity.Node, Online: true, Capacity: 1, Inventory: inventory,
        ActiveAttempts: []core.NodeActiveAttempt{{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: "different-attempt"}},
    }, 2); err != nil { t.Fatal(err) }
    waitFor(t, ctx, "different attempt heartbeat", func() bool {
        record, err := store.NodeRecord(ctx, identity.Node)
        return err == nil && len(record.ActiveAttempts) == 1 && record.ActiveAttempts[0].AttemptID == "different-attempt"
    })
    details, err = store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
    if err != nil { t.Fatal(err) }
    if details.Worker.Status != core.WorkerOffline { t.Fatalf("foreign Attempt revived Worker: %s", details.Worker.Status) }

    if err := replacement.SendHeartbeat(ctx, Heartbeat{
        Node: identity.Node, Online: true, Capacity: 1, Inventory: inventory, ActiveAttempts: matching,
    }, 3); err != nil { t.Fatal(err) }
    waitFor(t, ctx, "matching heartbeat restores Worker", func() bool {
        details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
        return err == nil && details.Worker.Status == core.WorkerWorking
    })
}
