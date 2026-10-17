package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestServerRestartNodeReconnectDeliversQueuedLateResultExactlyOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	_, err = local.QueueOutcome(core.AttemptOutcomeEnvelope{
		EventID: "late-after-restart", Node: "remote", HarnessInstanceID: "remote/fx",
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "actual remote completion",
	})
	if err != nil { t.Fatal(err) }
	if err := local.Close(); err != nil { t.Fatal(err) }
	if err := store.Close(); err != nil { t.Fatal(err) }

	reopened, err := core.Open(ctx, path)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	fresh, err := NewServerManager(ctx, reopened, "pair-token", "admin-token")
	if err != nil { t.Fatal(err) }
	fresh.SetEventSink(NewStoreEventSink(reopened))
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
	if err != nil || len(pending) != 1 { t.Fatalf("node outbox lost: %d %v", len(pending), err) }
	auth, err := identity.Authenticator()
	if err != nil { t.Fatal(err) }
	inventory := daemonInventoryFixture("remote")
	connection, err := DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{
		Node: identity.Node, ProtocolVersion: ProtocolVersion,
		Inventory: inventory, Nonce: "reconnect-after-restart",
		LastAcknowledgedSequence: queue.LastAcknowledgedSequence(),
	})
	if err != nil { t.Fatal(err) }
	defer connection.Close()
	if err := connection.SendPendingEvent(ctx, pending[0]); err != nil { t.Fatal(err) }
	if err := queue.AckThrough(pending[0].Sequence); err != nil { t.Fatal(err) }
	if lenMust, err := queue.PendingEvents(); err != nil || len(lenMust) != 0 {
		t.Fatalf("replayed outbox not acknowledged: %v %v", lenMust, err)
	}
	got, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if got.Worker.Status != core.WorkerIdle || got.CurrentAttempt().State != core.AttemptSucceeded ||
		len(got.Results) != 1 || got.Results[0].Summary != "actual remote completion" {
		t.Fatalf("reconnected result lost: %#v", got)
	}
	entries, err := reopened.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil { t.Fatal(err) }
	count := 0
	for _, entry := range entries { if entry.Kind == core.EntryWorkerResult { count++ } }
	if count != 1 { t.Fatalf("expected one user-visible Result, got %d", count) }
}
