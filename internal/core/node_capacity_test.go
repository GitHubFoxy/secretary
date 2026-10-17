package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAutomaticDispatchReservesCapacityAndReleasesOnFinalOutcome(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	instance := resolverInstance("single/codex", "single", HarnessCodex)
	inventory := resolverInventory("single", []HarnessInstance{instance})
	resolverEnroll(t, store, "single", inventory, 1, nil)
	conversation := mustConversation(t, store)
	request := DispatchResolutionRequest{WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessCodex}}

	worker, _, attempt, selected, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "first", request, "first-capacity-claim")
	if err != nil || selected.Queued || worker.NodeID != "single" {
		t.Fatalf("first slot was not reserved: worker=%#v err=%v", worker, err)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", nil); err != nil || used != 1 {
		t.Fatalf("durable claim is missing: used=%d err=%v", used, err)
	}
	if _, err := store.ResolveDispatch(ctx, request); !errors.Is(err, ErrNoEligibleNode) {
		t.Fatalf("second automatic dispatch overcommitted capacity=1: %v", err)
	}
	if _, _, _, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "second", request, "second-capacity-claim"); !errors.Is(err, ErrNoEligibleNode) {
		t.Fatalf("second Worker was created on full Node: %v", err)
	}
	if _, _, _, _, found, err := store.ReplayWorkerCreation(ctx, "second-capacity-claim"); err != nil || found {
		t.Fatalf("rejected spawn persisted a Worker: found=%v err=%v", found, err)
	}

	// A heartbeat reporting the same Attempt must not consume an extra slot.
	heartbeat := []NodeActiveAttempt{{AttemptID: attempt.ID, WorkerRef: worker.WorkerRef}}
	if err := store.UpdateNodeHeartbeat(ctx, "single", inventory, NodeHeartbeat{Capacity: 2, ActiveAttempts: heartbeat}); err != nil {
		t.Fatal(err)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", heartbeat); err != nil || used != 1 {
		t.Fatalf("same heartbeat and claim counted twice: used=%d err=%v", used, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "finished",
	}); err != nil {
		t.Fatal(err)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", nil); err != nil || used != 0 {
		t.Fatalf("terminal attempt still reserves a slot: used=%d err=%v", used, err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "single", inventory, NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	again, _, _, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "second", request, "second-capacity-claim")
	if err != nil || again.ID == worker.ID {
		t.Fatalf("slot was not released for next Worker: worker=%#v err=%v", again, err)
	}
}

func TestAutomaticDispatchRechecksCapacityAfterStaleResolution(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	resolverEnroll(t, store, "single", resolverInventory("single", []HarnessInstance{
		resolverInstance("single/codex", "single", HarnessCodex),
	}), 1, nil)
	conversation := mustConversation(t, store)
	request := DispatchResolutionRequest{}
	selected := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	var staleErr error
	go func() {
		defer wg.Done()
		_, _, _, _, staleErr = store.ResolveAndCreateWorkerWithProfileFactory(ctx, conversation.ID, "stale", request, "",
			func(selection DispatchResolution) (string, error) {
				close(selected)
				<-release
				return "profile for " + string(selection.HarnessInstance.Kind), nil
			})
	}()
	select {
	case <-selected:
	case <-time.After(5 * time.Second):
		t.Fatal("first Spawn never reached its capacity preview")
	}
	fresh, _, _, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "fresh", request, "")
	close(release)
	wg.Wait()
	if err != nil || fresh.NodeID != "single" {
		t.Fatalf("competing Worker did not reserve the free slot: %#v err=%v", fresh, err)
	}
	if !errors.Is(staleErr, ErrSelectedNodeUnavailable) {
		t.Fatalf("stale selection persisted despite consumed capacity: %v", staleErr)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", nil); err != nil || used != 1 {
		t.Fatalf("concurrent creates committed more than one slot: used=%d err=%v", used, err)
	}
}

