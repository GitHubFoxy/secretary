package core

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestDurableEventsHaveMonotonicSequenceAndReplayBoundary(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "durable.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.AppendInbound(ctx, conversation.ID, "web", "m-1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	event, err := store.RecordEventWithMetadata(ctx, EventInput{Kind: "custom", AggregateType: "conversation", AggregateID: conversation.ID, Source: "test", CorrelationID: person.ID, Payload: map[string]string{"ok": "yes"}})
	if err != nil {
		t.Fatal(err)
	}
	if event.Seq <= 0 || event.AggregateID != conversation.ID || event.Source != "test" {
		t.Fatalf("event=%#v", event)
	}
	replay, err := store.ReplayConversation(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if replay.BoundarySeq != first.Seq || len(replay.Entries) != 1 {
		t.Fatalf("replay=%#v", replay)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	second, _, err := store.AppendInbound(ctx, conversation.ID, "web", "m-2", "again")
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq <= first.Seq {
		t.Fatalf("sequence did not survive restart: first=%d second=%d", first.Seq, second.Seq)
	}
	events, err := store.EventsAfterSeq(ctx, event.Seq, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Seq <= event.Seq {
		t.Fatalf("events=%#v", events)
	}
}

func TestDuplicateWorkerCreationReturnsDurableOutcome(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.IdempotencyKey = "spawn-1"
	firstWorker, firstTurn, firstAttempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "same"})
	if err != nil {
		t.Fatal(err)
	}
	secondWorker, secondTurn, secondAttempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "different"})
	if err != nil {
		t.Fatal(err)
	}
	if firstWorker.ID != secondWorker.ID || firstTurn.ID != secondTurn.ID || firstAttempt.ID != secondAttempt.ID {
		t.Fatalf("duplicate creation differs: %#v %#v %#v / %#v %#v %#v", firstWorker, firstTurn, firstAttempt, secondWorker, secondTurn, secondAttempt)
	}
	var workers, turns, attempts int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers`).Scan(&workers); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns`).Scan(&turns); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempts`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if workers != 1 || turns != 1 || attempts != 1 {
		t.Fatalf("counts workers=%d turns=%d attempts=%d", workers, turns, attempts)
	}
}

func TestIdempotentWorkerCreationSurvivesReopenAndConcurrency(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "worker-idempotency.db")
	store, err := Open(ctx, path)
	if err != nil { t.Fatal(err) }
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	spec := phase4WorkerSpec(); spec.IdempotencyKey = "spawn-reopen"
	first, firstTurn, firstAttempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "same"})
	if err != nil { t.Fatal(err) }
	if err := store.Close(); err != nil { t.Fatal(err) }
	store, err = Open(ctx, path)
	if err != nil { t.Fatal(err) }
	defer store.Close()
	var wg sync.WaitGroup
	results := make(chan string, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ { wg.Add(1); go func() { defer wg.Done(); worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "different"}); if err != nil { errs <- err; return }; results <- worker.ID+turn.ID+attempt.ID }() }
	wg.Wait(); close(results); close(errs)
	for err := range errs { if err != nil { t.Fatal(err) } }
	for result := range results { if result != first.ID+firstTurn.ID+firstAttempt.ID { t.Fatalf("duplicate result=%s", result) } }
	var workers, turns, attempts, idempotency int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers`).Scan(&workers); err != nil { t.Fatal(err) }
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns`).Scan(&turns); err != nil { t.Fatal(err) }
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempts`).Scan(&attempts); err != nil { t.Fatal(err) }
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE operation = 'worker.create'`).Scan(&idempotency); err != nil { t.Fatal(err) }
	if workers != 1 || turns != 1 || attempts != 1 || idempotency != 1 { t.Fatalf("counts workers=%d turns=%d attempts=%d idempotency=%d", workers, turns, attempts, idempotency) }
}

func TestConcurrentFinalizationReturnsOriginalOutcome(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "finish"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	const n = 8
	results := make(chan FinishAttemptResult, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.FinishAttempt(ctx, attempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}})
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var outcomeID, resultID string
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent finalization: %v", err)
		}
	}
	for result := range results {
		if result.Outcome.ID == "" || result.Result == nil {
			t.Fatalf("empty result=%#v", result)
		}
		if outcomeID == "" {
			outcomeID, resultID = result.Outcome.ID, result.Result.ID
		} else if result.Outcome.ID != outcomeID || result.Result.ID != resultID || !result.Duplicate {
			t.Fatalf("not original durable outcome: %#v", result)
		}
	}
	var outcomes, finalResults, entries int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempt_outcomes`).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_results`).Scan(&finalResults); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_entries`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if outcomes != 1 || finalResults != 1 || entries != 1 {
		t.Fatalf("duplicates outcomes=%d results=%d entries=%d", outcomes, finalResults, entries)
	}
}

func TestReplayEventsPaginationHasNoLoss(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for i := 0; i < 501; i++ { if _, err := store.RecordEvent(ctx, "activity", "worker", "attempt", "legacy-session", map[string]int{"i": i}); err != nil { t.Fatal(err) } }
	page, err := store.ReplayEvents(ctx, 0, 500)
	if err != nil { t.Fatal(err) }
	if len(page.Events) != 500 || !page.HasMore || page.LastReturnedSeq != page.Events[499].Seq || page.SnapshotBoundarySeq <= page.LastReturnedSeq { t.Fatalf("first page=%#v", page) }
	next, err := store.ReplayEvents(ctx, page.LastReturnedSeq, 500)
	if err != nil { t.Fatal(err) }
	if len(next.Events) != 1 || next.HasMore || next.Events[0].Seq != page.SnapshotBoundarySeq { t.Fatalf("second page=%#v", next) }
}

func TestRuntimeSessionIsNotExposedByServerEvents(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	event, err := store.RecordEventWithMetadata(ctx, EventInput{Kind: "test", AggregateType: "worker", AggregateID: "worker", Source: "server", Payload: map[string]string{"ok": "yes"}})
	if err != nil { t.Fatal(err) }
	encoded, err := json.Marshal(event); if err != nil { t.Fatal(err) }
	if string(encoded) == "" || string(encoded) == "null" { t.Fatalf("event=%s", encoded) }
	if string(encoded) == "runtime_session_id" || event.Payload == nil { t.Fatalf("event payload=%s", encoded) }
	var columns int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('events') WHERE name = 'runtime_session_id'`).Scan(&columns); err != nil { t.Fatal(err) }
	if columns != 1 { t.Fatalf("legacy runtime column was removed, count=%d", columns) }
}

func TestLifecycleAndNotificationDedupeReturnOriginalOutcome(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	first, duplicate, err := store.ApplyLifecycleAction(ctx, LifecycleAction{IdempotencyKey: "cancel-1", Kind: "worker.cancel_requested", AggregateType: "worker", AggregateID: "worker-1", Source: "web", Payload: map[string]string{"reason": "owner"}})
	if err != nil || duplicate {
		t.Fatalf("first action=%#v duplicate=%v err=%v", first, duplicate, err)
	}
	second, duplicate, err := store.ApplyLifecycleAction(ctx, LifecycleAction{IdempotencyKey: "cancel-1", Kind: "worker.cancel_requested", AggregateType: "worker", AggregateID: "worker-1", Source: "telegram", Payload: map[string]string{"reason": "other"}})
	if err != nil || !duplicate || second.ActionID != first.ActionID || second.Event.ID != first.Event.ID {
		t.Fatalf("duplicate action=%#v duplicate=%v err=%v", second, duplicate, err)
	}
	event, delivery, duplicate, err := store.PublishImportantNotification(ctx, "worker.finished", "worker", "worker-1", "client-1", "notification-1", map[string]string{"summary": "done"})
	if err != nil || duplicate || event.ID == "" || delivery.ID == "" {
		t.Fatalf("notification=%#v %#v duplicate=%v err=%v", event, delivery, duplicate, err)
	}
	event2, delivery2, duplicate, err := store.PublishImportantNotification(ctx, "worker.finished", "worker", "worker-1", "client-1", "notification-1", map[string]string{"summary": "changed"})
	if err != nil || !duplicate || event2.ID != event.ID || delivery2.ID != delivery.ID {
		t.Fatalf("duplicate notification=%#v %#v duplicate=%v err=%v", event2, delivery2, duplicate, err)
	}
}

func TestFinalResultHasOneVisualDeliveryAndGenericDeliveryAudit(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx); if err != nil { t.Fatal(err) }
	_, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "result"}); if err != nil { t.Fatal(err) }
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
	finished, err := store.FinishAttempt(ctx, attempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}}); if err != nil { t.Fatal(err) }
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0); if err != nil { t.Fatal(err) }
	if len(entries) != 1 || entries[0].Kind != EntryWorkerResult { t.Fatalf("entries=%#v", entries) }
	deliveries, err := store.DeliveriesForEntry(ctx, entries[0].ID); if err != nil || len(deliveries) != 1 { t.Fatalf("deliveries=%#v err=%v", deliveries, err) }
	var resultAcceptedDeliveries int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deliveries d JOIN events e ON e.id = d.event_id WHERE e.kind = 'result.accepted' AND e.aggregate_id = ?`, finished.Result.ID).Scan(&resultAcceptedDeliveries); err != nil { t.Fatal(err) }
	if resultAcceptedDeliveries != 0 { t.Fatalf("result.accepted had visual delivery: %d", resultAcceptedDeliveries) }
	if _, err := store.MarkDeliveryDelivered(ctx, deliveries[0].ID); err != nil { t.Fatal(err) }
	var auditKind string
	if err := store.db.QueryRowContext(ctx, `SELECT kind FROM events WHERE aggregate_id = ? ORDER BY seq DESC LIMIT 1`, deliveries[0].EventID).Scan(&auditKind); err != nil { t.Fatal(err) }
	if auditKind != "delivery.delivered" { t.Fatalf("audit kind=%q", auditKind) }
	if turn.ID == "" { t.Fatal("turn missing") }
}

func TestUnmarshalableEventPayloadAbortsTransaction(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	before, err := store.ReplayEvents(ctx, 0, 500); if err != nil { t.Fatal(err) }
	if _, err := store.RecordEventWithMetadata(ctx, EventInput{Kind: "bad", Payload: func() {}}); err == nil { t.Fatal("unmarshalable payload was accepted") }
	after, err := store.ReplayEvents(ctx, 0, 500); if err != nil { t.Fatal(err) }
	if len(after.Events) != len(before.Events) || after.SnapshotBoundarySeq != before.SnapshotBoundarySeq { t.Fatalf("failed payload mutated durable state: before=%#v after=%#v", before, after) }
}

func TestDeliveryOutboxRetriesAndFailureAreDurable(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := store.AppendEntry(ctx, conversation.ID, EntrySecretary, "saved")
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := store.DeliveriesForEntry(ctx, entry.ID)
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("deliveries=%#v err=%v", deliveries, err)
	}
	delivery := deliveries[0]
	if delivery.State != DeliveryPending {
		t.Fatalf("delivery=%#v", delivery)
	}
	if _, err := store.RecordDeliveryFailure(ctx, delivery.ID, errors.New("offline")); err != nil {
		t.Fatal(err)
	}
	failed, err := store.Delivery(ctx, delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.RetryCount != 1 || failed.LastError != "offline" || failed.State != DeliveryPending {
		t.Fatalf("retry state=%#v", failed)
	}
	if _, err := store.MarkDeliveryDelivered(ctx, delivery.ID); err != nil {
		t.Fatal(err)
	}
	done, err := store.Delivery(ctx, delivery.ID)
	if err != nil || done.State != DeliveryDelivered {
		t.Fatalf("done=%#v err=%v", done, err)
	}
}
