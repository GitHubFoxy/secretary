package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// PublishImportantNotification durably records a server notification and its
// delivery row in one transaction. The target is a Client or channel identity.
func (s *Store) PublishImportantNotification(ctx context.Context, kind, aggregateType, aggregateID, target, idempotencyKey string, payload any) (Event, Delivery, bool, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(target) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return Event{}, Delivery{}, false, errors.New("core: notification kind, target, and idempotency key are required")
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		event     Event
		delivery  Delivery
		duplicate bool
	}, error) {
		var eventID string
		if err := tx.QueryRowContext(ctx, `SELECT event_id FROM deliveries WHERE idempotency_key = ?`, idempotencyKey).Scan(&eventID); err == nil {
			event, err := getEvent(ctx, tx, eventID)
			if err != nil {
				return struct {
					event     Event
					delivery  Delivery
					duplicate bool
				}{}, err
			}
			var delivery Delivery
			if err := scanDelivery(tx.QueryRowContext(ctx, `SELECT id, event_id, entry_id, target, idempotency_key, state, retry_count, last_error, created_at, updated_at, delivered_at FROM deliveries WHERE idempotency_key = ?`, idempotencyKey), &delivery); err != nil {
				return struct {
					event     Event
					delivery  Delivery
					duplicate bool
				}{}, err
			}
			return struct {
				event     Event
				delivery  Delivery
				duplicate bool
			}{event: event, delivery: delivery, duplicate: true}, nil
		}
		now := s.now()
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: kind, AggregateType: aggregateType, AggregateID: aggregateID, Source: "server", Payload: payload}, payload)
		if err != nil {
			return struct {
				event     Event
				delivery  Delivery
				duplicate bool
			}{}, err
		}
		delivery, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", target, idempotencyKey)
		return struct {
			event     Event
			delivery  Delivery
			duplicate bool
		}{event: event, delivery: delivery}, err
	})
	return returnValue.event, returnValue.delivery, returnValue.duplicate, err
}

func getEvent(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Event, error) {
	var event Event
	var payload string
	err := q.QueryRowContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE id = ?`, id).Scan(&event.ID, &event.Seq, &event.Kind, &event.AggregateType, &event.AggregateID, &event.Source, &event.CorrelationID, &event.CausationID, &event.WorkerRef, &event.AttemptID, &payload, newTimestampScanner(&event.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	event.Payload = []byte(payload)
	return event, err
}
