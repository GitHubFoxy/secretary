package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type secretaryTurnCompletion struct {
	turn  SecretaryTurn
	entry ConversationEntry
}

// FinishSecretaryTurnWithResponse atomically terminalizes a durable Secretary
// turn and, on success, stores the final assistant response in the canonical
// Personal Conversation. The live Secretary stream remains an event log; it is
// not a substitute for the durable conversation entry.
func (s *Store) FinishSecretaryTurnWithResponse(ctx context.Context, turnID string, state SecretaryTurnState, terminalError, response string) (SecretaryTurn, ConversationEntry, error) {
	if !state.Terminal() {
		return SecretaryTurn{}, ConversationEntry{}, errors.New("core: Secretary turn must finish in a terminal state")
	}
	response = strings.TrimSpace(response)
	if state != SecretaryTurnSucceeded && response != "" {
		return SecretaryTurn{}, ConversationEntry{}, errors.New("core: only a successful Secretary turn may persist a response")
	}

	completed, err := withTx(s, ctx, func(tx *sql.Tx) (secretaryTurnCompletion, error) {
		var turn SecretaryTurn
		if err := scanSecretaryTurn(tx.QueryRowContext(ctx, `SELECT id, identity_id, conversation_id, input, state, queue_position, error, created_at, started_at, finished_at, updated_at FROM secretary_turns WHERE id = ?`, turnID), &turn); errors.Is(err, sql.ErrNoRows) {
			return secretaryTurnCompletion{}, ErrNotFound
		} else if err != nil {
			return secretaryTurnCompletion{}, err
		}
		if turn.State != SecretaryTurnActive {
			return secretaryTurnCompletion{}, ErrInvalidTransition
		}

		now := s.now()
		turn.State, turn.Error, turn.FinishedAt, turn.UpdatedAt = state, strings.TrimSpace(terminalError), &now, now
		if _, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET state = ?, error = ?, finished_at = ?, updated_at = ? WHERE id = ?`, turn.State, turn.Error, timestamp(now), timestamp(now), turn.ID); err != nil {
			return secretaryTurnCompletion{}, err
		}

		var entry ConversationEntry
		if state == SecretaryTurnSucceeded && response != "" {
			var err error
			entry, err = appendEntry(ctx, tx, now, turn.ConversationID, EntrySecretary, response)
			if err != nil {
				return secretaryTurnCompletion{}, err
			}
		}

		payload := map[string]any{"turn_id": turn.ID, "status": string(state)}
		if turn.Error != "" {
			payload["error"] = turn.Error
		}
		if entry.ID != "" {
			payload["conversation_entry_id"] = entry.ID
		}
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTurnFinishedEvent, AggregateType: "secretary_turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID, Payload: payload}, payload)
		if err != nil {
			return secretaryTurnCompletion{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "secretary-stream:"+event.ID); err != nil {
			return secretaryTurnCompletion{}, err
		}
		return secretaryTurnCompletion{turn: turn, entry: entry}, nil
	})
	if err != nil {
		return SecretaryTurn{}, ConversationEntry{}, err
	}
	if completed.entry.ID != "" {
		s.notifyEntry(completed.entry)
	}
	return completed.turn, completed.entry, nil
}
