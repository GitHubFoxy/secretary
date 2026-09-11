package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Event struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	WorkerRef        string          `json:"worker_ref,omitempty"`
	AttemptID        string          `json:"attempt_id,omitempty"`
	RuntimeSessionID string          `json:"runtime_session_id,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (s *Store) RecordEvent(ctx context.Context, kind, workerRef, attemptID, runtimeSessionID string, payload any) (Event, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode event payload: %w", err)
	}
	now := s.now()
	event := Event{ID: newID("evt"), Kind: kind, WorkerRef: workerRef, AttemptID: attemptID, RuntimeSessionID: runtimeSessionID, Payload: encoded, CreatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO events(id, kind, worker_ref, attempt_id, runtime_session_id, payload_json, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, event.ID, event.Kind, event.WorkerRef, event.AttemptID, event.RuntimeSessionID, string(event.Payload), timestamp(event.CreatedAt))
	return event, err
}

func (s *Store) EventsAfter(ctx context.Context, after time.Time, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, worker_ref, attempt_id, runtime_session_id, payload_json, created_at FROM events WHERE created_at > ? ORDER BY created_at, id LIMIT ?`, timestamp(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var payload string
		if err := rows.Scan(&event.ID, &event.Kind, &event.WorkerRef, &event.AttemptID, &event.RuntimeSessionID, &payload, newTimestampScanner(&event.CreatedAt)); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) EventsRecent(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, worker_ref, attempt_id, runtime_session_id, payload_json, created_at FROM events ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		var payload string
		if err := rows.Scan(&event.ID, &event.Kind, &event.WorkerRef, &event.AttemptID, &event.RuntimeSessionID, &payload, newTimestampScanner(&event.CreatedAt)); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	return events, nil
}

func (s *Store) PruneEvents(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, timestamp(before))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func scanEvent(row *sql.Row) (Event, error) { _ = row; return Event{}, nil }
