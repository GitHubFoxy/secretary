package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNodeLossAndHeartbeatPreserveWaitingWorkerSemantics(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "node-offline.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	if _, err := store.EnrollNode(ctx, "remote"); err != nil { t.Fatal(err) }
	inventory := registryInventoryFixture("remote", time.Now().UTC())
	if err := store.MarkNodeConnected(ctx, "remote", inventory); err != nil { t.Fatal(err) }
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, NodeHeartbeat{Capacity: 2}); err != nil { t.Fatal(err) }
	type fixture struct { ref, turn, attempt string; want WorkerStatus }
	var fixtures []fixture
	for i, state := range []WorkerStatus{WorkerNeedsInput, WorkerWaitingApproval} {
		worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
			WorkerRef: []string{"input-worker", "approval-worker"}[i],
			Intent: "wait", ProjectID: "project", NodeID: "remote", HarnessInstanceID: "remote/fx",
		}, TurnSpec{Input: "waiting"})
		if err != nil { t.Fatal(err) }
		if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
		if state == WorkerNeedsInput {
			if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil { t.Fatal(err) }
		} else {
			if _, err := store.db.ExecContext(ctx, "UPDATE turns SET state = ? WHERE id = ?", TurnWaitingApproval, turn.ID); err != nil { t.Fatal(err) }
			if _, err := store.db.ExecContext(ctx, "UPDATE workers SET status = ? WHERE id = ?", WorkerWaitingApproval, worker.ID); err != nil { t.Fatal(err) }
		}
		fixtures = append(fixtures, fixture{worker.WorkerRef, turn.ID, attempt.ID, state})
	}
	if err := store.MarkNodeDisconnected(ctx, "remote"); err != nil { t.Fatal(err) }
	for _, f := range fixtures {
		details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, f.ref)
		if err != nil { t.Fatal(err) }
		if details.Worker.Status != WorkerOffline || details.CurrentAttempt().State != AttemptActive {
			t.Fatalf("Node disconnection did not hide actionability: %#v", details)
		}
	}
	if err := store.MarkNodeConnected(ctx, "remote", inventory); err != nil { t.Fatal(err) }
	if err := store.UpdateNodeHeartbeat(ctx, "remote", inventory, NodeHeartbeat{Capacity: 2,
		ActiveAttempts: []NodeActiveAttempt{
			{WorkerRef: fixtures[0].ref, TurnID: fixtures[0].turn, AttemptID: fixtures[0].attempt},
			{WorkerRef: fixtures[1].ref, TurnID: fixtures[1].turn, AttemptID: fixtures[1].attempt},
		},
	}); err != nil { t.Fatal(err) }
	for _, f := range fixtures {
		details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, f.ref)
		if err != nil { t.Fatal(err) }
		if details.Worker.Status != f.want {
			t.Fatalf("%s restored status=%s want=%s", f.ref, details.Worker.Status, f.want)
		}
	}
	if err := store.MarkSilentNodesOffline(ctx, time.Now().UTC().Add(time.Hour)); err != nil { t.Fatal(err) }
	for _, f := range fixtures {
		details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, f.ref)
		if err != nil { t.Fatal(err) }
		if details.Worker.Status != WorkerOffline {
			t.Fatalf("%s stayed actionable after heartbeat expiry: %s", f.ref, details.Worker.Status)
		}
	}
}
