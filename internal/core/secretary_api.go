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
	err := scanSecretaryTurn(s.db.QueryRowContext(ctx, `SELECT id, identity_id, conversation_id, input, state, queue_position, error, created_at, started_at, finished_at, updated_at FROM secretary_turns WHERE identity_id = ? AND state = 'active'`, identityID), &turn)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryTurn{}, ErrNotFound
	}
	return turn, err
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
