package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrIdempotencyConflict = errors.New("core: idempotency key reused with different payload")

func idempotencyRequestHash(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func checkIdempotencyHash(stored, requested string) error {
	if stored != "" && requested != "" && stored != requested {
		return ErrIdempotencyConflict
	}
	return nil
}

func lookupIdempotencyTx(ctx context.Context, tx *sql.Tx, operation, key, requestHash string, destination any) (bool, error) {
	var storedHash, encoded string
	err := tx.QueryRowContext(ctx, `SELECT request_hash, outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&storedHash, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := checkIdempotencyHash(storedHash, requestHash); err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(encoded), destination); err != nil {
		return false, fmt.Errorf("core: decode durable idempotency outcome: %w", err)
	}
	return true, nil
}

// IdempotencyOutcome returns the serialized durable outcome for audit and
// recovery tooling without exposing the table directly to callers.
func (s *Store) RecordIdempotencyOutcome(ctx context.Context, operation, key string, outcome any) error {
	return s.RecordIdempotencyOutcomeWithPayload(ctx, operation, key, nil, outcome)
}

func (s *Store) RecordIdempotencyOutcomeWithPayload(ctx context.Context, operation, key string, payload, outcome any) error {
	if operation == "" || key == "" {
		return errors.New("core: idempotency operation and key are required")
	}
	requestHash := ""
	var err error
	if payload != nil {
		requestHash, err = idempotencyRequestHash(payload)
		if err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		var storedHash string
		err := tx.QueryRowContext(ctx, `SELECT request_hash FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&storedHash)
		if err == nil {
			return checkIdempotencyHash(storedHash, requestHash)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, operation, key, requestHash, string(encoded), timestamp(s.now()))
		return err
	})
}

func (s *Store) IdempotencyOutcomeForPayload(ctx context.Context, operation, key string, payload any) (json.RawMessage, bool, error) {
	requestHash, err := idempotencyRequestHash(payload)
	if err != nil {
		return nil, false, err
	}
	var storedHash, encoded string
	err = s.db.QueryRowContext(ctx, `SELECT request_hash, outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&storedHash, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := checkIdempotencyHash(storedHash, requestHash); err != nil {
		return nil, false, err
	}
	return json.RawMessage(encoded), true, nil
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
