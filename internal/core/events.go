package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Event struct {
	ID            string          `json:"id"`
	Seq           int64           `json:"seq"`
	Kind          string          `json:"kind"`
	AggregateType string          `json:"aggregate_type,omitempty"`
	AggregateID   string          `json:"aggregate_id,omitempty"`
	Source        string          `json:"source,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	WorkerRef     string          `json:"worker_ref,omitempty"`
	AttemptID     string          `json:"attempt_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (s *Store) RecordEvent(ctx context.Context, kind, workerRef, attemptID, runtimeSessionID string, payload any) (Event, error) {
	return s.RecordEventWithMetadata(ctx, EventInput{Kind: kind, AggregateType: "worker", AggregateID: workerRef, Source: "server", WorkerRef: workerRef, AttemptID: attemptID, Payload: payload})
}

func (s *Store) RecordEventWithMetadata(ctx context.Context, input EventInput) (Event, error) {
	if input.Kind == "" {
		return Event{}, fmt.Errorf("core: event kind is required")
	}
	if input.Source == "" {
		input.Source = "server"
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Event, error) {
		return appendEventTx(ctx, tx, s.now(), input, input.Payload)
	})
}

func (s *Store) EventsForWorkerAfterSeq(ctx context.Context, workerRef string, afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE seq > ? AND (worker_ref = ? OR (aggregate_type = 'worker' AND aggregate_id = ?)) ORDER BY seq LIMIT ?`, afterSeq, workerRef, workerRef, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
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
		event.Payload = sanitizeFailureEventPayload(event.Kind, json.RawMessage(payload))
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) MaxEventSeq(ctx context.Context) (int64, error) {
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM events`).Scan(&max); err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64, nil
}

func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, timestamp(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func sanitizeFailureEventPayload(kind string, raw json.RawMessage) json.RawMessage {
	if kind != "result.accepted" && kind != "attempt.outcome_recorded" {
		return raw
	}
	var original map[string]json.RawMessage
	if json.Unmarshal(raw, &original) != nil || original == nil {
		return json.RawMessage(`{"status":"failed"}`)
	}
	var status string
	if json.Unmarshal(original["status"], &status) == nil && status == "succeeded" {
		return raw
	}
	payload := make(map[string]json.RawMessage)
	for _, key := range []string{"id", "worker_id", "turn_id", "attempt_id", "correlation_id"} {
		var value string
		if json.Unmarshal(original[key], &value) == nil {
			payload[key], _ = json.Marshal(value)
		}
	}
	if !AttemptOutcomeStatus(status).Valid() {
		status = "failed"
	}
	payload["status"], _ = json.Marshal(status)
	var classification OutcomeClassification
	if json.Unmarshal(original["classification"], &classification) == nil && classification.Valid() {
		payload["classification"], _ = json.Marshal(classification)
	}
	var createdAt time.Time
	if json.Unmarshal(original["created_at"], &createdAt) == nil {
		payload["created_at"], _ = json.Marshal(createdAt)
	}
	for _, key := range []string{"summary", "error_code", "error_message", "diagnostics", "failure_code"} {
		var value string
		if json.Unmarshal(original[key], &value) == nil {
			if key == "failure_code" {
				value = SanitizeFailureProviderCode(value)
			} else {
				value = SanitizeFailureMessage(value)
			}
			payload[key], _ = json.Marshal(value)
		}
	}
	var category FailureCategory
	if json.Unmarshal(original["failure_category"], &category) == nil {
		payload["failure_category"], _ = json.Marshal(SanitizeFailureCategory(category))
	}
	var failure WorkerFailure
	if json.Unmarshal(original["failure"], &failure) == nil && string(original["failure"]) != "null" {
		payload["failure"], _ = json.Marshal(SanitizeWorkerFailure(failure))
	}
	encoded, _ := json.Marshal(payload)
	return encoded
}
