package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const (
	secretaryInterruptionRequestedEvent = "secretary.interruption_requested"
	secretaryInterruptionTerminalEvent  = "secretary.interruption_terminal"
	secretaryRecoveryUnknownEvent       = "secretary.runtime_recovery_unknown"
	secretaryRecoveryConfirmedEvent     = "secretary.runtime_recovery_confirmed"
)

func (s *Store) SecretaryRecoveryRequired(ctx context.Context, identityID string) (bool, error) {
	var required bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM events request
		JOIN secretary_turns turn ON turn.id = request.aggregate_id
		WHERE turn.identity_id = ?
		  AND request.kind IN (?, ?)
		  AND NOT EXISTS (
			SELECT 1 FROM events resolved
			WHERE resolved.aggregate_id = request.aggregate_id
			  AND resolved.correlation_id = request.correlation_id
			  AND resolved.kind IN (?, ?)
		  )
	)`, identityID, secretaryInterruptionRequestedEvent, secretaryRecoveryUnknownEvent, secretaryInterruptionTerminalEvent, secretaryRecoveryConfirmedEvent).Scan(&required)
	return required, err
}

func (s *Store) ConfirmSecretaryRecovery(ctx context.Context, identityID string) (bool, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (bool, error) {
		rows, err := tx.QueryContext(ctx, `SELECT request.aggregate_id, request.correlation_id, request.kind, request.payload_json
			FROM events request
			JOIN secretary_turns turn ON turn.id = request.aggregate_id
			WHERE turn.identity_id = ?
			  AND request.kind IN (?, ?)
			  AND NOT EXISTS (
				SELECT 1 FROM events resolved
				WHERE resolved.aggregate_id = request.aggregate_id
				  AND resolved.correlation_id = request.correlation_id
				  AND resolved.kind IN (?, ?)
			  )
			ORDER BY request.seq`, identityID, secretaryInterruptionRequestedEvent, secretaryRecoveryUnknownEvent, secretaryInterruptionTerminalEvent, secretaryRecoveryConfirmedEvent)
		if err != nil {
			return false, err
		}
		type pending struct {
			turnID        string
			correlationID string
			kind          string
			payload       map[string]any
		}
		var requests []pending
		for rows.Next() {
			var item pending
			var payload string
			if err := rows.Scan(&item.turnID, &item.correlationID, &item.kind, &payload); err != nil {
				rows.Close()
				return false, err
			}
			if err := json.Unmarshal([]byte(payload), &item.payload); err != nil {
				rows.Close()
				return false, err
			}
			requests = append(requests, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return false, err
		}
		if err := rows.Close(); err != nil {
			return false, err
		}
		if len(requests) == 0 {
			return false, nil
		}
		now := s.now()
		for _, item := range requests {
			item.payload["recovery_confirmed"] = true
			item.payload["previous_session_stopped"] = true
			if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: secretaryRecoveryConfirmedEvent, AggregateType: "secretary_turn", AggregateID: item.turnID, Source: "user", CorrelationID: item.correlationID}, item.payload); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

func recordSecretaryInterruptionTerminalsTx(ctx context.Context, tx *sql.Tx, now time.Time, turnID string, state SecretaryTurnState) error {
	rows, err := tx.QueryContext(ctx, `SELECT request.correlation_id, request.payload_json
		FROM events request
		WHERE request.aggregate_type = 'secretary_turn'
		  AND request.aggregate_id = ?
		  AND request.kind = ?
		  AND NOT EXISTS (
			SELECT 1 FROM events resolved
			WHERE resolved.aggregate_id = request.aggregate_id
			  AND resolved.correlation_id = request.correlation_id
			  AND resolved.kind IN (?, ?)
		  )`, turnID, secretaryInterruptionRequestedEvent, secretaryInterruptionTerminalEvent, secretaryRecoveryConfirmedEvent)
	if err != nil {
		return err
	}
	type request struct {
		correlationID string
		payload       map[string]any
	}
	var requests []request
	for rows.Next() {
		var item request
		var payload string
		if err := rows.Scan(&item.correlationID, &payload); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(payload), &item.payload); err != nil {
			rows.Close()
			return err
		}
		requests = append(requests, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range requests {
		item.payload["terminal_status"] = string(state)
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: secretaryInterruptionTerminalEvent, AggregateType: "secretary_turn", AggregateID: turnID, Source: "server", CorrelationID: item.correlationID}, item.payload); err != nil {
			return err
		}
	}
	return nil
}

func recordSecretaryRecoveryUnknownTx(ctx context.Context, tx *sql.Tx, now time.Time, turn SecretaryTurn) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events WHERE aggregate_id = ? AND correlation_id = ? AND kind = ?)`, turn.ID, turn.ID, secretaryRecoveryUnknownEvent).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	payload := map[string]any{"turn_id": turn.ID, "input_id": turn.InputID, "status": "unknown", "error": turn.Error}
	_, err := appendEventTx(ctx, tx, now, EventInput{Kind: secretaryRecoveryUnknownEvent, AggregateType: "secretary_turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID}, payload)
	return err
}
