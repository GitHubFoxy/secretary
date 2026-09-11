package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) migrateDurableEventSchema(ctx context.Context) error {
	for _, migration := range []string{
		"events seq INTEGER",
		"events aggregate_type TEXT NOT NULL DEFAULT ''",
		"events aggregate_id TEXT NOT NULL DEFAULT ''",
		"events source TEXT NOT NULL DEFAULT ''",
		"events correlation_id TEXT NOT NULL DEFAULT ''",
		"events causation_id TEXT NOT NULL DEFAULT ''",
	} {
		parts := strings.SplitN(migration, " ", 2)
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+parts[0]+` ADD COLUMN `+parts[1]); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("migrate durable event column %s: %w", migration, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE events SET seq = (SELECT COUNT(*) FROM events prior WHERE prior.rowid <= events.rowid) WHERE seq IS NULL`); err != nil {
		return fmt.Errorf("backfill durable event sequence: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS events_seq_unique ON events(seq)`); err != nil {
		return fmt.Errorf("index durable event sequence: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO event_sequence(id, next_seq) VALUES(1, COALESCE((SELECT MAX(seq) + 1 FROM events), 1))`); err != nil {
		return fmt.Errorf("initialize durable event sequence: %w", err)
	}
	return nil
}

func appendEventTx(ctx context.Context, tx *sql.Tx, now time.Time, input EventInput, payload any) (Event, error) {
	encoded, err := mustJSON(payload)
	if err != nil {
		return Event{}, fmt.Errorf("encode event payload: %w", err)
	}
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT next_seq FROM event_sequence WHERE id = 1`).Scan(&next); errors.Is(err, sql.ErrNoRows) {
		next = 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO event_sequence(id, next_seq) VALUES(1, 2)`); err != nil {
			return Event{}, err
		}
	} else if err != nil {
		return Event{}, err
	} else if _, err := tx.ExecContext(ctx, `UPDATE event_sequence SET next_seq = ? WHERE id = 1`, next+1); err != nil {
		return Event{}, err
	}
	event := Event{ID: newID("evt"), Seq: next, Kind: input.Kind, AggregateType: input.AggregateType, AggregateID: input.AggregateID, Source: input.Source, CorrelationID: input.CorrelationID, CausationID: input.CausationID, WorkerRef: input.WorkerRef, AttemptID: input.AttemptID, Payload: json.RawMessage(encoded), CreatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, event.ID, event.Seq, event.Kind, event.AggregateType, event.AggregateID, event.Source, event.CorrelationID, event.CausationID, event.WorkerRef, event.AttemptID, string(event.Payload), timestamp(event.CreatedAt))
	return event, err
}

func (s *Store) ReplayConversation(ctx context.Context, conversationID string, afterSeq int64) (ConversationReplay, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (ConversationReplay, error) {
		var boundary int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM conversation_entries WHERE conversation_id = ?`, conversationID).Scan(&boundary); err != nil {
			return ConversationReplay{}, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, conversation_id, seq, kind, body, created_at FROM conversation_entries WHERE conversation_id = ? AND seq > ? AND seq <= ? ORDER BY seq`, conversationID, afterSeq, boundary)
		if err != nil {
			return ConversationReplay{}, err
		}
		defer rows.Close()
		entries := make([]ConversationEntry, 0)
		for rows.Next() {
			entry, err := scanEntry(rows)
			if err != nil {
				return ConversationReplay{}, err
			}
			entries = append(entries, entry)
		}
		if err := rows.Err(); err != nil {
			return ConversationReplay{}, err
		}
		return ConversationReplay{BoundarySeq: boundary, Entries: entries}, nil
	})
}

// ReplayEvents captures a server-side boundary and reads only events visible
// at that boundary. A live subscriber can continue strictly after BoundarySeq.
func (s *Store) ReplayEvents(ctx context.Context, afterSeq int64, limit int) (EventReplay, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (EventReplay, error) {
		var snapshotBoundary int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&snapshotBoundary); err != nil {
			return EventReplay{}, err
		}
		if limit <= 0 || limit > 500 {
			limit = 500
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE seq > ? AND seq <= ? ORDER BY seq LIMIT ?`, afterSeq, snapshotBoundary, limit+1)
		if err != nil {
			return EventReplay{}, err
		}
		defer rows.Close()
		events, err := scanEvents(rows)
		if err != nil {
			return EventReplay{}, err
		}
		hasMore := len(events) > limit
		if hasMore {
			events = events[:limit]
		}
		lastReturned := afterSeq
		if len(events) > 0 {
			lastReturned = events[len(events)-1].Seq
		}
		return EventReplay{SnapshotBoundarySeq: snapshotBoundary, BoundarySeq: snapshotBoundary, LastReturnedSeq: lastReturned, HasMore: hasMore, Events: events}, nil
	})
}

func (s *Store) EnqueueDelivery(ctx context.Context, eventID, entryID, target, idempotencyKey string) (Delivery, bool, error) {
	if strings.TrimSpace(eventID) == "" || strings.TrimSpace(target) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return Delivery{}, false, errors.New("core: delivery event, target, and idempotency key are required")
	}
	result, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		delivery  Delivery
		duplicate bool
	}, error) {
		delivery, duplicate, err := enqueueDeliveryTx(ctx, tx, s.now(), eventID, entryID, target, idempotencyKey)
		return struct {
			delivery  Delivery
			duplicate bool
		}{delivery: delivery, duplicate: duplicate}, err
	})
	return result.delivery, result.duplicate, err
}

func enqueueDeliveryTx(ctx context.Context, tx *sql.Tx, now time.Time, eventID, entryID, target, idempotencyKey string) (Delivery, bool, error) {
	var delivery Delivery
	if err := scanDelivery(tx.QueryRowContext(ctx, `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries WHERE idempotency_key = ?`, idempotencyKey), &delivery); err == nil {
		return delivery, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, false, err
	}
	id := newID("del")
	var nullableEntry any
	if entryID != "" {
		nullableEntry = entryID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, 0, '', ?, ?)`, id, eventID, nullableEntry, target, idempotencyKey, DeliveryPending, timestamp(now), timestamp(now)); err != nil {
		return Delivery{}, false, err
	}
	return Delivery{ID: id, EventID: eventID, EntryID: entryID, Target: target, IdempotencyKey: idempotencyKey, State: DeliveryPending, CreatedAt: now, UpdatedAt: now}, false, nil
}

func (s *Store) Delivery(ctx context.Context, id string) (Delivery, error) {
	var delivery Delivery
	err := scanDelivery(s.db.QueryRowContext(ctx, `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries WHERE id = ?`, id), &delivery)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	return delivery, err
}

func (s *Store) DeliveriesForEntry(ctx context.Context, entryID string) ([]Delivery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries WHERE entry_id = ? ORDER BY created_at, id`, entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Delivery, 0)
	for rows.Next() {
		var delivery Delivery
		if err := scanDelivery(rows, &delivery); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func (s *Store) Deliveries(ctx context.Context, state DeliveryState, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	query := `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries ORDER BY created_at, id LIMIT ?`
	args := []any{limit}
	if state != "" {
		query = `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries WHERE state = ? ORDER BY created_at, id LIMIT ?`
		args = []any{state, limit}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Delivery, 0)
	for rows.Next() {
		var delivery Delivery
		if err := scanDelivery(rows, &delivery); err != nil {
			return nil, err
		}
		result = append(result, delivery)
	}
	return result, rows.Err()
}

func scanDelivery(row interface{ Scan(...any) error }, delivery *Delivery) error {
	var entry, delivered sql.NullString
	err := row.Scan(&delivery.ID, &delivery.EventID, &entry, &delivery.Target, &delivery.IdempotencyKey, &delivery.State, &delivery.RetryCount, &delivery.LastError, newTimestampScanner(&delivery.CreatedAt), newTimestampScanner(&delivery.UpdatedAt), &delivered)
	if err != nil {
		return err
	}
	if entry.Valid {
		delivery.EntryID = entry.String
	}
	if delivered.Valid {
		value, err := parseTimestamp(delivered.String)
		if err != nil {
			return err
		}
		delivery.DeliveredAt = &value
	}
	return nil
}

func (s *Store) RecordDeliveryFailure(ctx context.Context, id string, deliveryErr error) (Delivery, error) {
	message := "delivery failed"
	if deliveryErr != nil {
		message = deliveryErr.Error()
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Delivery, error) {
		delivery, err := getDelivery(ctx, tx, id)
		if err != nil {
			return Delivery{}, err
		}
		if delivery.State == DeliveryDelivered {
			return delivery, nil
		}
		now := s.now()
		delivery.RetryCount++
		delivery.LastError = message
		delivery.UpdatedAt = now
		if delivery.RetryCount >= MaxDeliveryRetries {
			delivery.State = DeliveryFailed
		}
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET state = ?, retry_count = ?, last_error = ?, updated_at = ? WHERE id = ?`, delivery.State, delivery.RetryCount, delivery.LastError, timestamp(now), id); err != nil {
			return Delivery{}, err
		}
		if _, err := appendAuditEventTx(ctx, tx, now, "delivery.failed", delivery.EventID, map[string]any{"delivery_id": id, "retry_count": delivery.RetryCount, "error": message}); err != nil {
			return Delivery{}, err
		}
		return delivery, nil
	})
}

// MarkDeliveryFailed records a terminal delivery failure. RetryDelivery moves
// it back to pending without changing its idempotency key.
func (s *Store) MarkDeliveryFailed(ctx context.Context, id string, deliveryErr error) (Delivery, error) {
	message := "delivery failed"
	if deliveryErr != nil {
		message = deliveryErr.Error()
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Delivery, error) {
		delivery, err := getDelivery(ctx, tx, id)
		if err != nil {
			return Delivery{}, err
		}
		if delivery.State == DeliveryDelivered {
			return delivery, nil
		}
		now := s.now()
		delivery.State, delivery.LastError, delivery.UpdatedAt = DeliveryFailed, message, now
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET state = ?, last_error = ?, updated_at = ? WHERE id = ?`, delivery.State, delivery.LastError, timestamp(now), id); err != nil {
			return Delivery{}, err
		}
		if _, err := appendAuditEventTx(ctx, tx, now, "delivery.failed", delivery.EventID, map[string]any{"delivery_id": id, "retry_count": delivery.RetryCount, "error": message}); err != nil {
			return Delivery{}, err
		}
		return delivery, nil
	})
}

func (s *Store) MarkDeliveryDelivered(ctx context.Context, id string) (Delivery, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Delivery, error) {
		delivery, err := getDelivery(ctx, tx, id)
		if err != nil {
			return Delivery{}, err
		}
		if delivery.State == DeliveryDelivered {
			return delivery, nil
		}
		now := s.now()
		delivery.State, delivery.UpdatedAt = DeliveryDelivered, now
		delivery.DeliveredAt = &now
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET state = ?, updated_at = ?, delivered_at = ? WHERE id = ?`, delivery.State, timestamp(now), timestamp(now), id); err != nil {
			return Delivery{}, err
		}
		if _, err := appendAuditEventTx(ctx, tx, now, "delivery.delivered", delivery.EventID, map[string]any{"delivery_id": id, "target": delivery.Target}); err != nil {
			return Delivery{}, err
		}
		return delivery, nil
	})
}

func (s *Store) RetryDelivery(ctx context.Context, id string) (Delivery, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Delivery, error) {
		delivery, err := getDelivery(ctx, tx, id)
		if err != nil {
			return Delivery{}, err
		}
		if delivery.State == DeliveryDelivered {
			return delivery, nil
		}
		delivery.State, delivery.UpdatedAt = DeliveryPending, s.now()
		_, err = tx.ExecContext(ctx, `UPDATE deliveries SET state = ?, updated_at = ? WHERE id = ?`, delivery.State, timestamp(delivery.UpdatedAt), id)
		return delivery, err
	})
}

func getDelivery(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Delivery, error) {
	var delivery Delivery
	err := scanDelivery(q.QueryRowContext(ctx, `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries WHERE id = ?`, id), &delivery)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	return delivery, err
}

func mustJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func (s *Store) loadIdempotency(ctx context.Context, operation, key string, destination any) (bool, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(encoded), destination); err != nil {
		return false, fmt.Errorf("core: decode durable idempotency outcome: %w", err)
	}
	return true, nil
}

func (s *Store) saveIdempotency(ctx context.Context, operation, key string, outcome any) error {
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, operation, key, string(encoded), timestamp(s.now()))
	return err
}

func appendAuditEventTx(ctx context.Context, tx *sql.Tx, now time.Time, kind, aggregateID string, payload any) (Event, error) {
	return appendEventTx(ctx, tx, now, EventInput{Kind: kind, AggregateType: "delivery", AggregateID: aggregateID, Source: "server", Payload: payload}, payload)
}
