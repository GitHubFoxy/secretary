package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// IdempotencyOutcome returns the serialized durable outcome for audit and
// recovery tooling without exposing the table directly to callers.
func (s *Store) RecordIdempotencyOutcome(ctx context.Context, operation, key string, outcome any) error {
	if operation == "" || key == "" {
		return errors.New("core: idempotency operation and key are required")
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, operation, key, string(encoded), timestamp(s.now()))
	return err
}

func (s *Store) IdempotencyOutcome(ctx context.Context, operation, key string) (json.RawMessage, bool, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(encoded), true, nil
}
