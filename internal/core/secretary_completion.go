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

func (s *Store) FinishSecretaryTurnWithResponse(ctx context.Context, turnID string, state SecretaryTurnState, terminalError, response string) (SecretaryTurn, ConversationEntry, error) {
	return s.FinishSecretaryTurnWithOutput(ctx, turnID, "", state, terminalError, response, nil)
}

func (s *Store) FinishSecretaryTurnWithOutput(ctx context.Context, turnID, inputID string, state SecretaryTurnState, terminalError, response string, textDeltas []string) (SecretaryTurn, ConversationEntry, error) {
	return s.finishSecretaryTurnWithOutput(ctx, turnID, inputID, state, terminalError, response, textDeltas, false, nil)
}

func (s *Store) FinishSecretaryTurnWithAddressedReplyOnly(ctx context.Context, turnID, inputID string) (SecretaryTurn, error) {
	turn, _, err := s.finishSecretaryTurnWithOutput(ctx, turnID, inputID, SecretaryTurnSucceeded, "", "", nil, true, nil)
	return turn, err
}

type SecretaryCompletionEvidence struct {
	Branch          string `json:"branch"`
	TerminalClass   string `json:"terminal_class"`
	AssistantChunks uint64 `json:"assistant_chunks"`
	TerminalValid   bool   `json:"terminal_valid"`
	RPCSucceeded    bool   `json:"rpc_succeeded"`
	DrainCompleted  bool   `json:"drain_completed"`
	ResponsePresent bool   `json:"response_present"`
	ReplyCount      int    `json:"reply_count"`
	EntryPresent    bool   `json:"entry_present"`
	Committed       bool   `json:"committed"`
	Code            string `json:"code"`
}

func (s *Store) FinishSecretaryTurnWithRequiredReply(ctx context.Context, turnID, inputID string, state SecretaryTurnState, evidence SecretaryCompletionEvidence) (SecretaryTurn, error) {
	turn, _, err := s.finishSecretaryTurnWithOutput(ctx, turnID, inputID, state, "", "", nil, false, &evidence)
	return turn, err
}

func (s *Store) finishSecretaryTurnWithOutput(ctx context.Context, turnID, inputID string, state SecretaryTurnState, terminalError, response string, textDeltas []string, requireAddressedReply bool, evidence *SecretaryCompletionEvidence) (SecretaryTurn, ConversationEntry, error) {
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
		var discovery SecretaryMCPDiscovery
		if evidence != nil {
			if inputID == "" || turn.InputID != inputID {
				return secretaryTurnCompletion{}, ErrInvalidSecretaryOrigin
			}
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_reply_entries r JOIN conversation_entries e ON e.id = r.entry_id WHERE r.secretary_turn_id = ? AND r.input_id = ? AND r.body = e.body AND e.conversation_id = ? AND e.kind = ?`, turnID, inputID, turn.ConversationID, EntrySecretary).Scan(&evidence.ReplyCount); err != nil {
				return secretaryTurnCompletion{}, err
			}
			evidence.EntryPresent = evidence.ReplyCount == 1
			evidence.Code = "native_terminal_invalid"
			switch evidence.Branch {
			case "assistant_final", "addressed_reply_only", "progress", "invalid":
			default:
				evidence.Branch = "invalid"
			}
			switch evidence.TerminalClass {
			case "end_turn", "max_tokens", "refusal", "canceled", "other":
			default:
				evidence.TerminalClass = "unknown"
			}
			var err error
			discovery, err = secretaryMCPDiscoveryQuery(ctx, tx, turn.ID)
			if err != nil {
				return secretaryTurnCompletion{}, err
			}
			validBranch := evidence.Branch == "assistant_final" && evidence.ResponsePresent || evidence.Branch == "addressed_reply_only"
			if state == SecretaryTurnSucceeded && discovery.Revoked {
				state = SecretaryTurnFailed
				evidence.Code = "mcp_launch_revoked"
			} else if state == SecretaryTurnSucceeded && evidence.TerminalClass == "end_turn" && evidence.TerminalValid && evidence.RPCSucceeded && evidence.DrainCompleted && validBranch {
				if evidence.ReplyCount == 1 {
					evidence.Code = "completed"
				} else {
					state = SecretaryTurnFailed
					evidence.Code = "addressed_reply_missing"
				}
			} else if state != SecretaryTurnCanceled {
				state = SecretaryTurnFailed
			}
			if state != SecretaryTurnSucceeded {
				terminalError = evidence.Code
			}
			evidence.Committed = true
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
		var acknowledgement string
		ackErr := tx.QueryRowContext(ctx, `SELECT body FROM secretary_acknowledgements WHERE secretary_turn_id = ? AND input_id = ?`, turn.ID, turn.InputID).Scan(&acknowledgement)
		if ackErr != nil && !errors.Is(ackErr, sql.ErrNoRows) {
			return secretaryTurnCompletion{}, ackErr
		}
		if acknowledgement != "" && evidence == nil && !requireAddressedReply {
			publishUnaddressedOutput = publishUnaddressedOutput && response != acknowledgement
		}
		if evidence == nil && !requireAddressedReply && state == SecretaryTurnFailed && acknowledgement != "" && turn.Error != "" {
			var existing int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_entries WHERE conversation_id = ? AND turn_id = ? AND kind = ? AND body = ?`, turn.ConversationID, turn.ID, EntrySecretary, turn.Error).Scan(&existing); err != nil {
				return secretaryTurnCompletion{}, err
			}
			if existing == 0 && turn.Error != acknowledgement {
				var err error
				entry, err = appendEntryWithIdentity(ctx, tx, now, turn.ConversationID, EntrySecretary, turn.Error, "", turn.ID, "")
				if err != nil {
					return secretaryTurnCompletion{}, err
				}
			}
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
				entry, err = appendEntryWithIdentity(ctx, tx, now, turn.ConversationID, EntrySecretary, response, "", turn.ID, "")
				if err != nil {
					return secretaryTurnCompletion{}, err
				}
			}
		}

		if evidence != nil && state == SecretaryTurnFailed && evidence.ReplyCount == 0 {
            var harness string
            if err := tx.QueryRowContext(ctx, `SELECT runtime_harness FROM secretary_identities WHERE id = ?`, turn.IdentityID).Scan(&harness); err != nil { return secretaryTurnCompletion{}, err }
            if harness == "codex" {
                var accepted int
                err := tx.QueryRowContext(ctx, `SELECT EXISTS(
                    SELECT 1 FROM secretary_worker_creation_origins o JOIN turns t ON t.id = o.worker_turn_id JOIN workers w ON w.id = t.worker_id
                    WHERE o.secretary_turn_id = ? AND o.input_id = ? AND w.conversation_id = ?
                    UNION
                    SELECT 1 FROM secretary_worker_command_origins o JOIN phase4_worker_commands c ON c.id = o.command_id
                    JOIN phase4_attempts a ON a.id = c.attempt_id JOIN turns t ON t.id = o.worker_turn_id JOIN workers w ON w.id = t.worker_id
                    WHERE o.secretary_turn_id = ? AND o.input_id = ? AND w.conversation_id = ?
                      AND c.kind = 'dispatch' AND c.state = 'delivered' AND a.turn_id = t.id
                )`, turn.ID, turn.InputID, turn.ConversationID, turn.ID, turn.InputID, turn.ConversationID).Scan(&accepted)
                if err != nil { return secretaryTurnCompletion{}, err }
                message := "Secretary не получил подтверждённый адресованный ответ. Выполнение запроса не подтверждено; проверьте доступность Secretary MCP и состояние задачи перед повторной попыткой."
                if accepted != 0 { message = "Worker создан и принят сервером, но Secretary не завершил адресованный ответ. Проверьте состояние Worker перед повторным запросом, чтобы избежать дубликатов." }
                entry, err = appendEntryWithIdentity(ctx, tx, now, turn.ConversationID, EntrySecretary, message, "", turn.ID, "")
                if err != nil { return secretaryTurnCompletion{}, err }
            }
        }
        payload := map[string]any{"turn_id": turn.ID, "status": string(state)}
		if acknowledgement != "" {
			payload["acknowledged"] = true
		}
		if turn.Error != "" {
			payload["error"] = turn.Error
		}
		if entry.ID != "" {
			payload["conversation_entry_id"] = entry.ID
		}
		if evidence != nil {
			payload["completion"] = *evidence
			payload["mcp_discovery"] = discovery
		}
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTurnFinishedEvent, AggregateType: "secretary_turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID, Payload: payload}, payload)
		if err != nil {
			return secretaryTurnCompletion{}, err
		}
		if err := recordSecretaryInterruptionTerminalsTx(ctx, tx, now, turn.ID, state); err != nil {
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
