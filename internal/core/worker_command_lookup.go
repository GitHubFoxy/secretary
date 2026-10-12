package core

import (
	"context"
	"database/sql"
	"errors"
)

// FindWorkerCommandByWorkerAndDedupeKey returns a previously claimed command
// for a worker by kind and dedupe key across any attempt.
func (s *Store) FindWorkerCommandByWorkerAndDedupeKey(ctx context.Context, kind, dedupeKey, workerID string) (WorkerCommand, bool, error) {
	var command WorkerCommand
	err := scanWorkerCommand(s.db.QueryRowContext(ctx, `SELECT id, kind, dedupe_key, worker_id, attempt_id, state, last_error, lease_until, created_at, updated_at FROM phase4_worker_commands WHERE kind = ? AND worker_id = ? AND dedupe_key = ? ORDER BY created_at DESC LIMIT 1`, kind, workerID, dedupeKey), &command)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerCommand{}, false, nil
	}
	if err != nil {
		return WorkerCommand{}, false, err
	}
	return command, true, nil
}

// ValidateWorkerCommandOrigin verifies that the command origin matches the supplied origin.
// When requireActive is true (for pending handoff/reclaim), the Secretary turn must be active.
// When requireActive is false (for reading historical terminal outcome), matching authenticated
// origin with valid capability is accepted even if the Secretary turn has already completed.
// A command created without origin rejects any supplied origin (fabricated or otherwise),
// and an un-originated replay of an originated command is rejected.
func (s *Store) ValidateWorkerCommandOrigin(ctx context.Context, commandID string, origin *SecretaryOriginIdentity, requireActive bool) error {
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		return validateWorkerCommandOriginTx(ctx, tx, commandID, origin, requireActive)
	})
}

func validateWorkerCommandOriginTx(ctx context.Context, tx *sql.Tx, commandID string, origin *SecretaryOriginIdentity, requireActive bool) error {
	var existingTurnID, existingInputID, existingWorkerTurnID string
	err := tx.QueryRowContext(ctx, `SELECT secretary_turn_id, input_id, worker_turn_id FROM secretary_worker_command_origins WHERE command_id = ?`, commandID).Scan(&existingTurnID, &existingInputID, &existingWorkerTurnID)
	if errors.Is(err, sql.ErrNoRows) {
		if origin == nil {
			return nil
		}
		return ErrInvalidSecretaryOrigin
	}
	if err != nil {
		return err
	}
	if origin == nil {
		return ErrInvalidSecretaryOrigin
	}
	value := normalizeSecretaryOriginIdentity(*origin)
	if !validSecretaryOriginIdentity(value) {
		return ErrInvalidSecretaryOrigin
	}
	secretaryOrigin, err := authorizedSecretaryOriginTx(ctx, tx, value.PersonID, value.Capability, value.SecretaryTurnID, value.InputID)
	if err != nil {
		return ErrInvalidSecretaryOrigin
	}
	if requireActive && secretaryOrigin.State != SecretaryTurnActive {
		return ErrInvalidSecretaryOrigin
	}
	var workerConversationID, workerTurnID string
	err = tx.QueryRowContext(ctx, `SELECT w.conversation_id, a.turn_id FROM phase4_worker_commands c JOIN workers w ON w.id = c.worker_id JOIN phase4_attempts a ON a.id = c.attempt_id WHERE c.id = ?`, commandID).Scan(&workerConversationID, &workerTurnID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if workerConversationID != secretaryOrigin.ConversationID || existingTurnID != value.SecretaryTurnID || existingInputID != value.InputID || existingWorkerTurnID != workerTurnID {
		return ErrInvalidSecretaryOrigin
	}
	return nil
}

func queuedWorkerCommandOriginTx(ctx context.Context, tx *sql.Tx, commandID, secretaryTurnID, inputID, workerTurnID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1
		FROM phase4_worker_commands c
		JOIN phase4_attempts a ON a.id = c.attempt_id
		JOIN worker_queued_messages q ON q.worker_id = c.worker_id AND q.turn_id = a.turn_id
		WHERE c.id = ? AND c.kind IN ('dispatch', 'resume') AND c.dedupe_key = 'attempt'
		  AND q.state = 'delivering' AND q.secretary_turn_id = ? AND q.input_id = ? AND q.turn_id = ?
	)`, commandID, secretaryTurnID, inputID, workerTurnID).Scan(&exists)
	return exists, err
}
