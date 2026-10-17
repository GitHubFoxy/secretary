package node

import (
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/beruseruko/secretary/internal/core"
)

func TestServerRestartNodeReconnectDeliversQueuedLateResultExactlyOnce(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    path := filepath.Join(t.TempDir(), "secretary.db")
    store, err := core.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
    if err != nil { t.Fatal(err) }

    var mu sync.RWMutex
    active := manager
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        mu.RLock()
        current := active
        mu.RUnlock()
        if r.URL.Path == "/v1/nodes/connect" {
            current.ServeProtocolHTTP(w, r)
            return
        }
        current.ServeHTTP(w, r)
    })
    server := httptest.NewServer(handler)
    defer server.Close()
    identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "remote")
    if err != nil { t.Fatal(err) }
    _, conversation, err := store.CreatePersonWithConversation(ctx)
    if err != nil { t.Fatal(err) }
    worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
        WorkerRef: "late-reconnected", NodeID: "remote", HarnessInstanceID: "remote/fx",
        Intent: "persist output", ProjectID: "project",
    }, core.TurnSpec{Input: "make an external change"})
    if err != nil { t.Fatal(err) }
    if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }

    localPath := filepath.Join(t.TempDir(), "node.json")
    local, err := OpenLocalStore(localPath)
    if err != nil { t.Fatal(err) }
    queued, err := local.QueueOutcome(core.AttemptOutcomeEnvelope{
        EventID: "late-after-restart", Node: "remote", HarnessInstanceID: "remote/fx",
        WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
        Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "actual remote completion",
    })
    if err != nil { t.Fatal(err) }
    if err := local.Close(); err != nil { t.Fatal(err) }
    if err := store.Close(); err != nil { t.Fatal(err) }

    // Restart the Server using its persisted database, without restarting
    // or re-executing the remote native Attempt.
    reopened, err := core.Open(ctx, path)
    if err != nil { t.Fatal(err) }
    defer reopened.Close()
    fresh, err := NewServerManager(ctx, reopened, "pair-token", "admin-token")
    if err != nil { t.Fatal(err) }

    actualSink := NewStoreEventSink(reopened)
    var deliveries atomic.Int32
    firstCommitted := make(chan struct{})
    releaseFirst := make(chan struct{})
    fresh.SetEventSink(func(ctx context.Context, event NodeEvent) error {
        // Commit the real Result on the first delivery, then simulate the
        // connection dropping before Server can issue its protocol ACK.
        if err := actualSink(ctx, event); err != nil { return err }
        if deliveries.Add(1) == 1 {
            close(firstCommitted)
            select {
            case <-releaseFirst:
                return errors.New("inject disconnect after durable commit, before ACK")
            case <-ctx.Done():
                return ctx.Err()
            }
        }
        return nil
    })
    fresh.SetCommandOutcomeSink(NewStoreCommandOutcomeSink(reopened))
    mu.Lock()
    active = fresh
    mu.Unlock()
    if err := reopened.RecoverPhase4Attempts(ctx, Phase4RecoveryResolver{Store: reopened, Remote: fresh}); err != nil {
        t.Fatal(err)
    }
    waiting, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
    if err != nil { t.Fatal(err) }
    if waiting.Worker.Status != core.WorkerOffline || waiting.CurrentAttempt().State != core.AttemptActive ||
        len(waiting.Results) != 0 {
        t.Fatalf("restart prematurely finalized remote execution: %#v", waiting)
    }

    queue, err := OpenLocalStore(localPath)
    if err != nil { t.Fatal(err) }
    defer queue.Close()
    pending, err := queue.PendingEvents()
    if err != nil || len(pending) != 1 || pending[0].Sequence != queued.Sequence {
        t.Fatalf("restarted Node lost durable outbox: %#v, err=%v", pending, err)
    }
    daemon := &Daemon{
        Identity: identity, Store: queue, Runtime: &daemonCountingRuntime{},
        Inventory: daemonStaticInventory{snapshot: daemonInventoryFixture("remote")},
        Capacity: 1, HeartbeatInterval: 20 * time.Millisecond,
        OutboxPollInterval: 10 * time.Millisecond,
        ReconnectMin: 10 * time.Millisecond, ReconnectMax: 40 * time.Millisecond,
    }
    daemonCtx, stop := context.WithCancel(ctx)
    done := make(chan error, 1)
    go func() { done <- daemon.Run(daemonCtx) }()
    defer func() {
        stop()
        select {
        case err := <-done:
            if err != nil { t.Errorf("daemon stopped unexpectedly: %v", err) }
        case <-time.After(time.Second):
            t.Error("daemon failed to stop")
        }
    }()
    select {
    case <-firstCommitted:
    case <-ctx.Done():
        t.Fatal("Node did not deliver first terminal Result")
    }
    // ACK must NOT have happened yet, even though the Server committed the
    // result. The real daemon is responsible for outbox removal after ACK.
    pending, err = queue.PendingEvents()
    if err != nil || len(pending) != 1 || queue.LastAcknowledgedSequence() >= queued.Sequence {
        t.Fatalf("Node lost unacknowledged Result: pending=%#v ack=%d err=%v", pending, queue.LastAcknowledgedSequence(), err)
    }
    firstResult, err := reopened.Phase4Result(ctx, turn.ID)
    if err != nil || firstResult.Summary != "actual remote completion" {
        t.Fatalf("first delivery was not committed: %#v, %v", firstResult, err)
    }
    close(releaseFirst)

    // The daemon must reconnect, resend its persisted event, read the actual
    // authenticated Server ACK, and only then remove the event from outbox.
    waitFor(t, ctx, "duplicate Node event delivered and ACKed", func() bool {
        pending, err := queue.PendingEvents()
        return err == nil && deliveries.Load() >= 2 && len(pending) == 0 &&
            queue.LastAcknowledgedSequence() >= queued.Sequence
    })
    got, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
    if err != nil { t.Fatal(err) }
    if got.Worker.Status != core.WorkerIdle || got.CurrentAttempt().State != core.AttemptSucceeded ||
        len(got.Results) != 1 || got.Results[0].ID != firstResult.ID {
        t.Fatalf("replayed terminal Result changed canonical outcome: %#v", got)
    }
    entries, err := reopened.EntriesAfter(ctx, conversation.ID, 0)
    if err != nil { t.Fatal(err) }
    count := 0
    for _, entry := range entries {
        if entry.Kind == core.EntryWorkerResult {
            count++
            if entry.ResultID != firstResult.ID { t.Fatalf("replay changed Result identity: %#v", entry) }
        }
    }
    if count != 1 { t.Fatalf("duplicate replay emitted %d user-visible Results", count) }
}
