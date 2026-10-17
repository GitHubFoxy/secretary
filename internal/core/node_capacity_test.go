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


func TestIdleFollowupReservesCapacityAtomicallyAgainstOtherWorkers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	resolverEnroll(t, store, "single", resolverInventory("single", []HarnessInstance{
		resolverInstance("single/codex", "single", HarnessCodex),
	}), 1, nil)
	conversation := mustConversation(t, store)
	request := DispatchResolutionRequest{}

	// Both Workers exist and are idle before the capacity=1 race.
	workers := make([]Worker, 2)
	for i := range workers {
		worker, _, initial, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "initial", request, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := store.RecordAttemptOutcome(ctx, initial.ID, AttemptOutcomeInput{
			Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "idle",
		}); err != nil {
			t.Fatal(err)
		}
		workers[i] = worker
	}
	start := make(chan struct{})
	type outcome struct {
		index int
		attempt Phase4Attempt
		err error
	}
	events := make(chan outcome, 2)
	for i, worker := range workers {
		go func(index int, id string) {
			<-start
			_, attempt, err := store.CreateTurn(ctx, id, TurnSpec{
				Input: "new follow-up", IdempotencyKey: "followup-capacity",
				CommandKind: "dispatch", ReserveNodeCapacity: true,
			})
			events <- outcome{index: index, attempt: attempt, err: err}
		}(i, worker.ID)
	}
	close(start)
	first, second := <-events, <-events
	var accepted, denied int
	var successful outcome
	for _, entry := range []outcome{first, second} {
		switch {
		case entry.err == nil:
			successful = entry
			accepted++
		case errors.Is(entry.err, ErrSelectedNodeUnavailable):
			denied++
		default:
			t.Fatalf("unexpected concurrent follow-up error for Worker %d: %v", entry.index, entry.err)
		}
	}
	if accepted != 1 || denied != 1 {
		t.Fatalf("capacity=1 overbooked: accepted=%d denied=%d", accepted, denied)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", nil); err != nil || used != 1 {
		t.Fatalf("concurrent Follow-ups reserved %d slots, err=%v", used, err)
	}
	_, replayed, err := store.CreateTurn(ctx, workers[successful.index].ID, TurnSpec{
		Input: "new follow-up", IdempotencyKey: "followup-capacity",
		CommandKind: "dispatch", ReserveNodeCapacity: true,
	})
	if err != nil || replayed.ID != successful.attempt.ID {
		t.Fatalf("replaying a Follow-up must not consume another slot: attempt=%#v err=%v", replayed, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, successful.attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "follow-up done",
	}); err != nil {
		t.Fatal(err)
	}
	loser := 1 - successful.index
	_, next, err := store.CreateTurn(ctx, workers[loser].ID, TurnSpec{
		Input: "new follow-up", IdempotencyKey: "followup-capacity",
		CommandKind: "dispatch", ReserveNodeCapacity: true,
	})
	if err != nil || next.ID == "" {
		t.Fatalf("free slot was not reclaimed after Follow-up completion: attempt=%#v err=%v", next, err)
	}
}

func TestRevokeNodeIfIdleIncludesUnreportedCapacityClaims(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	inventory := resolverInventory("single", []HarnessInstance{
		resolverInstance("single/codex", "single", HarnessCodex),
	})
	resolverEnroll(t, store, "single", inventory, 1, nil)
	conversation := mustConversation(t, store)
	worker, _, attempt, selected, err := store.ResolveAndCreateWorker(
		ctx, conversation.ID, "reserved before heartbeat", DispatchResolutionRequest{}, "reserved-worker",
	)
	if err != nil || selected.Queued {
		t.Fatalf("automatic Spawn did not claim ready Node: worker=%#v selection=%#v err=%v", worker, selected, err)
	}
	record, err := store.NodeRecord(ctx, "single")
	if err != nil || len(record.ActiveAttempts) != 0 {
		t.Fatalf("expected heartbeat without the just-created Attempt: record=%#v err=%v", record, err)
	}
	if _, err := store.RevokeNodeIfIdle(ctx, "single", "safe-revoke"); !errors.Is(err, ErrNodeActiveAttempts) {
		t.Fatalf("safe revoke ignored unreported Attempt reservation: %v", err)
	}
	record, err = store.NodeRecord(ctx, "single")
	if err != nil || record.Revoked || !record.Online {
		t.Fatalf("failed safe revoke modified Node state: record=%#v err=%v", record, err)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", nil); err != nil || used != 1 {
		t.Fatalf("original reservation lost after rejected revoke: used=%d err=%v", used, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "finished",
	}); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.RevokeNodeIfIdle(ctx, "single", "safe-revoke")
	if err != nil || !revoked.Revoked || revoked.Online {
		t.Fatalf("Node was not safely revoked after terminal Attempt: record=%#v err=%v", revoked, err)
	}
}

func TestRevokeNodeIfIdleUnionsHeartbeatAndClaims(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	inventory := resolverInventory("single", []HarnessInstance{
		resolverInstance("single/codex", "single", HarnessCodex),
	})
	resolverEnroll(t, store, "single", inventory, 1, nil)
	conversation := mustConversation(t, store)
	_, _, attempt, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "running", DispatchResolutionRequest{}, "")
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := []NodeActiveAttempt{{AttemptID: attempt.ID}}
	if err := store.UpdateNodeHeartbeat(ctx, "single", inventory, NodeHeartbeat{Capacity: 1, ActiveAttempts: heartbeat}); err != nil {
		t.Fatal(err)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", heartbeat); err != nil || used != 1 {
		t.Fatalf("heartbeat and durable claim counted twice: used=%d err=%v", used, err)
	}
	if _, err := store.RevokeNodeIfIdle(ctx, "single"); !errors.Is(err, ErrNodeActiveAttempts) {
		t.Fatalf("safe revoke accepted busy Node with duplicate heartbeat and claim: %v", err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done",
	}); err != nil {
		t.Fatal(err)
	}
	// The Node may still report the running Attempt until its next heartbeat.
	// Safe revocation must continue to honor that report.
	if _, err := store.RevokeNodeIfIdle(ctx, "single"); !errors.Is(err, ErrNodeActiveAttempts) {
		t.Fatalf("safe revoke ignored a still-active heartbeat: %v", err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "single", inventory, NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	if revoked, err := store.RevokeNodeIfIdle(ctx, "single"); err != nil || !revoked.Revoked {
		t.Fatalf("safe revoke failed after heartbeat and claim cleared: revoked=%#v err=%v", revoked, err)
	}
}

func TestRevokeNodeIfIdleRacesWithAutomaticSpawn(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	inventory := resolverInventory("single", []HarnessInstance{
		resolverInstance("single/codex", "single", HarnessCodex),
	})
	resolverEnroll(t, store, "single", inventory, 1, nil)
	conversation := mustConversation(t, store)
	resolved := make(chan struct{})
	allowCreate := make(chan struct{})
	type spawnResult struct {
		worker Worker
		err error
	}
	spawnDone := make(chan spawnResult, 1)
	go func() {
		worker, _, _, _, err := store.ResolveAndCreateWorkerWithProfileFactory(
			ctx, conversation.ID, "concurrent spawn", DispatchResolutionRequest{}, "",
			func(DispatchResolution) (string, error) {
				close(resolved)
				<-allowCreate
				return "", nil
			},
		)
		spawnDone <- spawnResult{worker: worker, err: err}
	}()
	select {
	case <-resolved:
	case <-time.After(5 * time.Second):
		t.Fatal("Spawn did not reach the stale availability preview")
	}
	// Revoke wins the race while Spawn has selected its Node but has not
	// committed a reservation. The subsequent Spawn must fail closed.
	revoked, err := store.RevokeNodeIfIdle(ctx, "single")
	close(allowCreate)
	got := <-spawnDone
	if err != nil || !revoked.Revoked {
		t.Fatalf("safe revoke lost idle race: record=%#v err=%v", revoked, err)
	}
	if got.err == nil || got.worker.ID != "" {
		t.Fatalf("Spawn created Worker after atomic safe revoke: %#v", got)
	}
	if used, err := nodeCapacityUsed(ctx, store.db, "single", nil); err != nil || used != 0 {
		t.Fatalf("failed concurrent Spawn persisted reservation: used=%d err=%v", used, err)
	}
}
