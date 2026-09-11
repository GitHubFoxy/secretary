package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// ApplyLifecycleAction records a side-effecting lifecycle action exactly once.
// The returned outcome is the stored outcome on every duplicate delivery.
func (s *Store) ApplyLifecycleAction(ctx context.Context, action LifecycleAction) (LifecycleOutcome, bool, error) {
	key := strings.TrimSpace(action.IdempotencyKey)
	if key == "" || strings.TrimSpace(action.Kind) == "" {
		return LifecycleOutcome{}, false, errors.New("core: lifecycle action kind and idempotency key are required")
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	result, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		outcome   LifecycleOutcome
		duplicate bool
	}, error) {
		var encoded string
		err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "lifecycle:"+action.Kind, key).Scan(&encoded)
		if err == nil {
			var outcome LifecycleOutcome
			if err := json.Unmarshal([]byte(encoded), &outcome); err != nil {
				return struct {
					outcome   LifecycleOutcome
					duplicate bool
				}{}, err
			}
			return struct {
				outcome   LifecycleOutcome
				duplicate bool
			}{outcome: outcome, duplicate: true}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return struct {
				outcome   LifecycleOutcome
				duplicate bool
			}{}, err
		}
		now := s.now()
		actionID := newID("act")
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: action.Kind, AggregateType: action.AggregateType, AggregateID: action.AggregateID, Source: action.Source, CorrelationID: actionID, Payload: action.Payload}, mustJSON(action.Payload))
		if err != nil {
			return struct {
				outcome   LifecycleOutcome
				duplicate bool
			}{}, err
		}
		outcome := LifecycleOutcome{ActionID: actionID, Kind: action.Kind, State: "accepted", Event: event}
		encodedBytes, err := json.Marshal(outcome)
		if err != nil {
			return struct {
				outcome   LifecycleOutcome
				duplicate bool
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, "lifecycle:"+action.Kind, key, string(encodedBytes), timestamp(now)); err != nil {
			return struct {
				outcome   LifecycleOutcome
				duplicate bool
			}{}, err
		}
		return struct {
			outcome   LifecycleOutcome
			duplicate bool
		}{outcome: outcome}, nil
	})
	return result.outcome, result.duplicate, err
}

// RecordLifecycleAction is a descriptive alias for ApplyLifecycleAction.
func (s *Store) RecordLifecycleAction(ctx context.Context, action LifecycleAction) (LifecycleOutcome, bool, error) {
	return s.ApplyLifecycleAction(ctx, action)
}

func (s *Store) CreateWorkerWithIdempotency(ctx context.Context, conversationID, key string, spec WorkerSpec, turnSpec TurnSpec) (Worker, Turn, Phase4Attempt, error) {
	spec.IdempotencyKey = key
	return s.CreateWorker(ctx, conversationID, spec, turnSpec)
}

func (s *Store) CreateTurnWithIdempotency(ctx context.Context, workerID, key string, spec TurnSpec) (Turn, Phase4Attempt, error) {
	spec.IdempotencyKey = key
	return s.CreateTurn(ctx, workerID, spec)
}
