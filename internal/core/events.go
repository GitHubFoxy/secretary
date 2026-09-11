package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Event is an append-only normalized server event with a global sequence.
type Event struct {
	ID               string          `json:"id"`
	Seq              int64           `json:"seq"`
	Kind             string          `json:"kind"`
	AggregateType    string          `json:"aggregate_type,omitempty"`
	AggregateID      string          `json:"aggregate_id,omitempty"`
	Source           string          `json:"source,omitempty"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	CausationID      string          `json:"causation_id,omitempty"`
	WorkerRef string          `json:"worker_ref,omitempty"`
	AttemptID string          `json:"attempt_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (s *Store) RecordEvent(ctx context.Context, kind, workerRef, attemptID, runtimeSessionID string, payload any) (Event, error) {
	// runtimeSessionID remains an ignored legacy argument. It is not persisted
	// or exposed by the Phase 4 server event contract.
	return s.RecordEventWithMetadata(ctx, EventInput{Kind: kind, AggregateType: "worker", AggregateID: workerRef, Source: "server", WorkerRef: workerRef, AttemptID: attemptID, Payload: payload})
}

// RecordEventWithMetadata appends one event and allocates its sequence in the
// same transaction as the row, so sequence numbers survive restart and prune.
func (s *Store) RecordEventWithMetadata(ctx context.Context, input EventInput) (Event, error) {
	if input.Kind == "" {
		return Event{}, fmt.Errorf("core: event kind is required")
	}
	if input.Source == "" { input.Source = "server" }
	encoded, err := json.Marshal(input.Payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode event payload: %w", err)
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Event, error) {
		return appendEventTx(ctx, tx, s.now(), input, encoded)
	})
}

func (s *Store) EventsAfterSeq(ctx context.Context, afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) EventsAfter(ctx context.Context, after time.Time, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE created_at > ? ORDER BY seq LIMIT ?`, timestamp(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) EventsRecent(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events ORDER BY seq DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := scanEvents(rows)
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, err
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		var payload string
		if err := rows.Scan(&event.ID, &event.Seq, &event.Kind, &event.AggregateType, &event.AggregateID, &event.Source, &event.CorrelationID, &event.CausationID, &event.WorkerRef, &event.AttemptID, &payload, newTimestampScanner(&event.CreatedAt)); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, timestamp(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
