package core

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) SecretaryIdentityForConversation(ctx context.Context, conversationID string) (SecretaryIdentity, error) {
	var identity SecretaryIdentity
	err := scanSecretaryIdentity(s.db.QueryRowContext(ctx, `SELECT id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at FROM secretary_identities WHERE conversation_id = ?`, conversationID), &identity)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryIdentity{}, ErrNotFound
	}
	return identity, err
}

func (s *Store) RestartSecretaryRuntime(ctx context.Context, identityID, harness, model, reasoning string) (SecretaryIdentity, error) {
	return s.ReplaceSecretaryRuntime(ctx, identityID, harness, model, reasoning)
}

func (s *Store) GetSecretaryIdentity(ctx context.Context, personID string) (SecretaryIdentity, error) {
	return s.SecretaryIdentity(ctx, personID)
}

func (s *Store) RecordSecretaryEvent(ctx context.Context, input SecretaryEventInput) (Event, error) {
	return s.AppendSecretaryEvent(ctx, input)
}

func (s *Store) SecretaryTurnEvents(ctx context.Context, turnID string, afterSeq int64, limit int) ([]Event, error) {
	return s.SecretaryEvents(ctx, turnID, afterSeq, limit)
}

func (s *Store) QueueSecretaryInput(ctx context.Context, identityID, input string) (SecretaryTurn, error) {
	return s.EnqueueSecretaryTurn(ctx, identityID, input)
}

func (s *Store) ActiveSecretaryTurn(ctx context.Context, identityID string) (SecretaryTurn, error) {
	var turn SecretaryTurn
	err := scanSecretaryTurn(s.db.QueryRowContext(ctx, secretaryTurnSelect+` WHERE identity_id = ? AND state = 'active'`, identityID), &turn)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryTurn{}, ErrNotFound
	}
	return turn, err
}

func (s *Store) BeginSecretaryPrompt(ctx context.Context, turnID string) error {
	_, err := withTx(s, ctx, func(tx *sql.Tx) (struct{}, error) {
		var state SecretaryTurnState
		var promptState string
		if err := tx.QueryRowContext(ctx, `SELECT state, prompt_state FROM secretary_turns WHERE id = ?`, turnID).Scan(&state, &promptState); errors.Is(err, sql.ErrNoRows) {
			return struct{}{}, ErrNotFound
		} else if err != nil {
			return struct{}{}, err
		}
		if state != SecretaryTurnActive {
			return struct{}{}, ErrInvalidTransition
		}
		if promptState == secretaryPromptAccepted || promptState == secretaryPromptStarted {
			return struct{}{}, nil
		}
		if promptState != secretaryPromptPending {
			return struct{}{}, errors.New("core: unknown Secretary prompt state")
		}
		_, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET prompt_state = ?, updated_at = ? WHERE id = ? AND state = ? AND prompt_state = ?`, secretaryPromptStarted, timestamp(s.now()), turnID, SecretaryTurnActive, secretaryPromptPending)
		return struct{}{}, err
	})
	return err
}

func (s *Store) AcceptSecretaryPrompt(ctx context.Context, turnID string) error {
	_, err := withTx(s, ctx, func(tx *sql.Tx) (struct{}, error) {
		var state SecretaryTurnState
		var promptState string
		if err := tx.QueryRowContext(ctx, `SELECT state, prompt_state FROM secretary_turns WHERE id = ?`, turnID).Scan(&state, &promptState); errors.Is(err, sql.ErrNoRows) {
			return struct{}{}, ErrNotFound
		} else if err != nil {
			return struct{}{}, err
		}
		if promptState == secretaryPromptAccepted {
			return struct{}{}, nil
		}
		if state != SecretaryTurnActive || promptState != secretaryPromptStarted {
			return struct{}{}, ErrInvalidTransition
		}
		_, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET prompt_state = ?, updated_at = ? WHERE id = ? AND state = ? AND prompt_state = ?`, secretaryPromptAccepted, timestamp(s.now()), turnID, SecretaryTurnActive, secretaryPromptStarted)
		return struct{}{}, err
	})
	return err
}

// ReleaseSecretaryTurnClaims is used only after a proven Prompt failure. It
// does not release accepted claims and is safe to repeat.
func (s *Store) ReleaseSecretaryTurnClaims(ctx context.Context, turnID string) error {
	_, err := withTx(s, ctx, func(tx *sql.Tx) (struct{}, error) {
		if err := releaseSecretaryResultsTx(ctx, tx, turnID); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Store) RecoverSecretaryTurn(ctx context.Context, identityID, errorMessage string) error {
	turn, err := s.ActiveSecretaryTurn(ctx, identityID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.FinishSecretaryTurn(ctx, turn.ID, SecretaryTurnInterrupted, errorMessage)
	return err
}

func (s *Store) RecordSecretaryTextDelta(ctx context.Context, turnID, text string) (Event, error) {
	return s.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turnID, Kind: SecretaryTextDeltaEvent, Text: text})
}

func (s *Store) RecordSecretaryThinkingSummary(ctx context.Context, turnID, summary string) (Event, error) {
	return s.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turnID, Kind: SecretaryThinkingSummaryEvent, Summary: summary})
}

func (s *Store) RecordSecretaryToolCall(ctx context.Context, turnID, tool, arguments string) (Event, error) {
	return s.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turnID, Kind: SecretaryToolCallEvent, Tool: tool, Arguments: arguments})
}

func (s *Store) RecordSecretaryToolResult(ctx context.Context, turnID, tool, result, status, terminalError string) (Event, error) {
	return s.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turnID, Kind: SecretaryToolResultEvent, Tool: tool, Result: result, Status: status, Error: terminalError})
}
