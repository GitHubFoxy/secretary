package ctl

import (
	"errors"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestAbandonAttemptAfterMissingHeartbeatWithoutNodeDisconnect(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.CurrentAttempt()
	if attempt == nil { t.Fatal("missing Attempt") }
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }

	nodeRef := core.NodeReference(details.Worker.NodeID)
	record, err := store.NodeRecord(ctx, nodeRef)
	if err != nil { t.Fatal(err) }
	live := core.NodeActiveAttempt{
		WorkerRef: details.Worker.WorkerRef, TurnID: details.CurrentTurn().ID, AttemptID: attempt.ID,
	}
	if err := store.UpdateNodeHeartbeat(ctx, nodeRef, record.Inventory, core.NodeHeartbeat{
		Capacity: 2, ActiveAttempts: []core.NodeActiveAttempt{live},
	}); err != nil { t.Fatal(err) }
	if err := store.UpdateNodeHeartbeat(ctx, nodeRef, record.Inventory, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	state, err := store.NodeRecord(ctx, nodeRef)
	if err != nil || !state.Online { t.Fatalf("Node must stay online: %#v %v", state, err) }
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if current.Worker.Status != core.WorkerOffline || current.CurrentAttempt().State != core.AttemptActive {
		t.Fatalf("missing Attempt remained actionable: %#v", current)
	}

	runtime := service.Runtime.(*lifecycleRuntime)
	before := runtime.count("steer")
	_, err = service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef: details.Worker.WorkerRef, Text: "steer missing native session",
		IdempotencyKey: "missing-session-steer",
	})
	if !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("stale native session accepted steering: %v", err)
	}
	if runtime.count("steer") != before {
		t.Fatal("a vanished Attempt received a steering command")
	}
	abandoned, err := service.AbandonAttempt(ctx, details.Worker.WorkerRef, attempt.ID)
	if err != nil { t.Fatal(err) }
	if abandoned.CurrentAttempt().State != core.AttemptInterrupted || len(abandoned.Results) != 1 {
		t.Fatalf("unknown Attempt was not abandoned: %#v", abandoned)
	}
	closed, err := service.CloseWorker(ctx, details.Worker.WorkerRef)
	if err != nil || closed.Worker.Status != core.WorkerClosed {
		t.Fatalf("abandoned Worker could not be closed: %#v %v", closed, err)
	}
}
