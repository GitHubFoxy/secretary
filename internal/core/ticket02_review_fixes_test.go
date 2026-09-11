package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdempotentTurnCreationSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "turn-idempotency.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, initialAttempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, initialAttempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "initial done"}); err != nil {
		t.Fatal(err)
	}

	firstTurn, firstAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "follow up", IdempotencyKey: "turn-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	secondTurn, secondAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "different duplicate payload", IdempotencyKey: "turn-1"})
	if err != nil {
		t.Fatal(err)
	}
	if secondTurn.ID != firstTurn.ID || secondAttempt.ID != firstAttempt.ID {
		t.Fatalf("duplicate Turn creation returned different durable outcome: first=%#v/%#v second=%#v/%#v", firstTurn, firstAttempt, secondTurn, secondAttempt)
	}

	var turns, attempts, idempotency int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE worker_id = ?`, worker.ID).Scan(&turns); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempts WHERE worker_id = ?`, worker.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "turn.create:"+worker.ID, "turn-1").Scan(&idempotency); err != nil {
		t.Fatal(err)
	}
	if turns != 2 || attempts != 2 || idempotency != 1 {
		t.Fatalf("unexpected durable counts: turns=%d attempts=%d idempotency=%d", turns, attempts, idempotency)
	}
}

func TestRecordEventWithMetadataPreservesJSONPayload(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	payload := map[string]any{"ok": true, "count": float64(3)}
	event, err := store.RecordEventWithMetadata(ctx, EventInput{Kind: "payload.test", AggregateType: "worker", AggregateID: "worker-1", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(event.Payload, &decoded); err != nil {
		t.Fatalf("event payload is not an object: %s: %v", event.Payload, err)
	}
	if decoded["ok"] != true || decoded["count"] != float64(3) {
		t.Fatalf("event payload changed shape: %#v", decoded)
	}
}

func TestServerEventJSONDoesNotExposeRuntimeSessionID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	event, err := store.RecordEvent(ctx, "test", "worker-1", "attempt-1", "native-secret-session", map[string]string{"ok": "yes"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "runtime_session_id") || strings.Contains(string(encoded), "native-secret-session") {
		t.Fatalf("server event leaked native runtime session: %s", encoded)
	}
}
