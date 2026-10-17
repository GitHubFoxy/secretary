package node

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestPhase4ResolverDoesNotTrustStaleNodeHeartbeat(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	if _, err := store.EnrollNode(ctx, "remote"); err != nil { t.Fatal(err) }
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "remote-recovery", Intent: "recover", NodeID: "remote",
		ProjectID: "project", HarnessInstanceID: "remote/claude",
	}, core.TurnSpec{Input: "run"})
	if err != nil { t.Fatal(err) }
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
	inventory := daemonInventoryFixture("remote")
	active := core.NodeHeartbeat{Capacity: 1, ActiveAttempts: []core.NodeActiveAttempt{{
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
	}}}
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, active); err != nil { t.Fatal(err) }
	manager, err := NewServerManager(ctx, store, "pairing", "admin")
	if err != nil { t.Fatal(err) }
	resolver := Phase4RecoveryResolver{Store: store, Remote: manager}
	decision, err := resolver.ResolvePhase4Attempt(ctx, attempt)
	if err != nil || decision != core.Phase4RecoveryAwaiting {
		t.Fatalf("offline stale heartbeat was trusted: %s, %v", decision, err)
	}
	// Handshake alone cannot make an old heartbeat authoritative.
	if err := store.MarkNodeConnected(ctx, "remote", inventory); err != nil { t.Fatal(err) }
	decision, err = resolver.ResolvePhase4Attempt(ctx, attempt)
	if err != nil || decision != core.Phase4RecoveryAwaiting {
		t.Fatalf("handshake revived stale execution: %s, %v", decision, err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, active); err != nil { t.Fatal(err) }
	decision, err = resolver.ResolvePhase4Attempt(ctx, attempt)
	if err != nil || decision != core.Phase4RecoveryAlive {
		t.Fatalf("fresh matching heartbeat rejected: %s, %v", decision, err)
	}
}
