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
	return s.FinishSecretaryTurnWithOutput(ctx, turnID, "", state, terminalError, response, nil)
}

// FinishSecretaryTurnWithOutput atomically flushes buffered assistant deltas
// and completes a turn. Addressed replies and Results linked to this exact
// turn/input identity suppress only unaddressed completion output.
func (s *Store) FinishSecretaryTurnWithOutput(ctx context.Context, turnID, inputID string, state SecretaryTurnState, terminalError, response string, textDeltas []string) (SecretaryTurn, ConversationEntry, error) {
	return s.finishSecretaryTurnWithOutput(ctx, turnID, inputID, state, terminalError, response, textDeltas, false)
}

// FinishSecretaryTurnWithAddressedReplyOnly succeeds only when exactly one
// canonical reply entry is durably linked to this turn's exact input identity.
func (s *Store) FinishSecretaryTurnWithAddressedReplyOnly(ctx context.Context, turnID, inputID string) (SecretaryTurn, error) {
	turn, _, err := s.finishSecretaryTurnWithOutput(ctx, turnID, inputID, SecretaryTurnSucceeded, "", "", nil, true)
	return turn, err
}

func (s *Store) finishSecretaryTurnWithOutput(ctx context.Context, turnID, inputID string, state SecretaryTurnState, terminalError, response string, textDeltas []string, requireAddressedReply bool) (SecretaryTurn, ConversationEntry, error) {
	if !state.Terminal() {
		return SecretaryTurn{}, ConversationEntry{}, errors.New("core: Secretary turn must finish in a terminal state")
	}
	response = strings.TrimSpace(response)
	if state != SecretaryTurnSucceeded && response != "" {
		return SecretaryTurn{}, ConversationEntry{}, errors.New("core: only a successful Secretary turn may persist a response")
	}

	completed, err := withTx(s, ctx, func(tx *sql.Tx) (secretaryTurnCompletion, error) {
		var turn SecretaryTurn
		if err := scanSecretaryTurn(tx.QueryRowContext(ctx, secretaryTurnSelect+` WHERE id = ?`, turnID), &turn); errors.Is(err, sql.ErrNoRows) {
			return secretaryTurnCompletion{}, ErrNotFound
		} else if err != nil {
			return secretaryTurnCompletion{}, err
		}
		if turn.State != SecretaryTurnActive {
			return secretaryTurnCompletion{}, ErrInvalidTransition
		}
		if inputID != "" && turn.InputID != inputID {
			return secretaryTurnCompletion{}, ErrInvalidSecretaryOrigin
		}
		if requireAddressedReply {
			if state != SecretaryTurnSucceeded || inputID == "" || turn.InputID != inputID {
				return secretaryTurnCompletion{}, ErrInvalidSecretaryOrigin
			}
			var replyCount int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)
FROM secretary_reply_entries r
JOIN conversation_entries e ON e.id = r.entry_id
WHERE r.secretary_turn_id = ? AND r.input_id = ? AND r.body = e.body AND e.conversation_id = ? AND e.kind = ?`, turnID, inputID, turn.ConversationID, EntrySecretary).Scan(&replyCount); err != nil {
				return secretaryTurnCompletion{}, err
			}
			if replyCount != 1 {
				return secretaryTurnCompletion{}, ErrAddressedReplyMissing
			}
		}
		if state == SecretaryTurnSucceeded {
			if _, err := tx.ExecContext(ctx, `UPDATE secretary_context_seen_results SET claim_state = 'accepted' WHERE turn_id = ? AND claim_state = 'claimed'`, turn.ID); err != nil {
				return secretaryTurnCompletion{}, err
			}
		} else if turn.PromptState != secretaryPromptAccepted {
			if err := releaseSecretaryTurnClaimsForStateTx(ctx, tx, turn.ID, turn.State, turn.PromptState); err != nil {
				return secretaryTurnCompletion{}, err
			}
		}

		now := s.now()
		turn.State, turn.Error, turn.FinishedAt, turn.UpdatedAt = state, strings.TrimSpace(terminalError), &now, now
		if _, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET state = ?, error = ?, finished_at = ?, updated_at = ? WHERE id = ?`, turn.State, turn.Error, timestamp(now), timestamp(now), turn.ID); err != nil {
			return secretaryTurnCompletion{}, err
		}

		var entry ConversationEntry
		publishUnaddressedOutput := state == SecretaryTurnSucceeded
		if publishUnaddressedOutput && turn.InputID != "" {
			var addressed, relatedResult int
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM secretary_reply_entries WHERE secretary_turn_id = ? AND input_id = ?), EXISTS(SELECT 1 FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?)`, turn.ID, turn.InputID, turn.ID, turn.InputID).Scan(&addressed, &relatedResult); err != nil {
				return secretaryTurnCompletion{}, err
			}
			publishUnaddressedOutput = addressed == 0 && relatedResult == 0
		}
		if publishUnaddressedOutput {
			for _, delta := range textDeltas {
				if delta == "" {
					continue
				}
				streamPayload := map[string]any{"turn_id": turn.ID, "text": delta}
				streamEvent, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTextDeltaEvent, AggregateType: "secretary_turn", AggregateID: turn.ID, Source: "secretary", CorrelationID: turn.ID, Payload: streamPayload}, streamPayload)
				if err != nil {
					return secretaryTurnCompletion{}, err
				}
				if _, _, err := enqueueDeliveryTx(ctx, tx, now, streamEvent.ID, "", "conversation", "secretary-stream:"+streamEvent.ID); err != nil {
					return secretaryTurnCompletion{}, err
				}
			}
			if response != "" {
				var err error
				entry, err = appendEntry(ctx, tx, now, turn.ConversationID, EntrySecretary, response)
				if err != nil {
					return secretaryTurnCompletion{}, err
				}
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
