package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidSecretaryOrigin = errors.New("core: invalid Secretary origin")
	ErrSecretaryReplyConflict = errors.New("core: reply identity already has different text")
	ErrAddressedReplyMissing  = errors.New("core: exact durable addressed reply is missing")
)

// ValidateSecretaryOrigin requires a server-authorized active Secretary turn and
// its exact server-issued input identity.
func (s *Store) ValidateSecretaryOrigin(ctx context.Context, personID, capability, secretaryTurnID, inputID string) error {
	personID, capability = strings.TrimSpace(personID), strings.TrimSpace(capability)
	secretaryTurnID, inputID = strings.TrimSpace(secretaryTurnID), strings.TrimSpace(inputID)
	if personID == "" || capability == "" || secretaryTurnID == "" || inputID == "" {
		return ErrInvalidSecretaryOrigin
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		origin, err := authorizedSecretaryOriginTx(ctx, tx, personID, capability, secretaryTurnID, inputID)
		if err != nil {
			return err
		}
		if origin.State != SecretaryTurnActive {
			return ErrInvalidSecretaryOrigin
		}
		return nil
	})
}

// LinkSecretaryWorkerTurn binds an accepted server-owned Secretary MCP action
// to the exact Worker Turn it affected. Replays return duplicate=true.
func (s *Store) LinkSecretaryWorkerTurn(ctx context.Context, personID, capability, secretaryTurnID, inputID, workerTurnID string) (duplicate bool, err error) {
	personID, capability = strings.TrimSpace(personID), strings.TrimSpace(capability)
	secretaryTurnID, inputID, workerTurnID = strings.TrimSpace(secretaryTurnID), strings.TrimSpace(inputID), strings.TrimSpace(workerTurnID)
	if personID == "" || capability == "" || secretaryTurnID == "" || inputID == "" || workerTurnID == "" {
		return false, ErrInvalidSecretaryOrigin
	}
	return withTx(s, ctx, func(tx *sql.Tx) (bool, error) {
		origin, err := authorizedSecretaryOriginTx(ctx, tx, personID, capability, secretaryTurnID, inputID)
		if err != nil {
			return false, err
		}
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM secretary_worker_origins WHERE secretary_turn_id = ? AND input_id = ? AND worker_turn_id = ?`, secretaryTurnID, inputID, workerTurnID).Scan(&exists)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		if origin.State != SecretaryTurnActive {
			return false, ErrInvalidSecretaryOrigin
		}
		var workerConversationID string
		if err := tx.QueryRowContext(ctx, `SELECT w.conversation_id FROM turns t JOIN workers w ON w.id = t.worker_id WHERE t.id = ?`, workerTurnID).Scan(&workerConversationID); errors.Is(err, sql.ErrNoRows) {
			return false, ErrInvalidSecretaryOrigin
		} else if err != nil {
			return false, err
		}
		if workerConversationID != origin.ConversationID {
			return false, ErrInvalidSecretaryOrigin
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO secretary_worker_origins(secretary_turn_id, input_id, worker_turn_id, created_at) VALUES(?, ?, ?, ?)`, secretaryTurnID, inputID, workerTurnID, timestamp(now)); err != nil {
			return false, err
		}
		if err := materializeSecretaryOriginResultsTx(ctx, tx, secretaryTurnID, inputID, workerTurnID, now); err != nil {
			return false, err
		}
		return false, nil
	})
}

func normalizeSecretaryOriginIdentity(origin SecretaryOriginIdentity) SecretaryOriginIdentity {
	origin.PersonID = strings.TrimSpace(origin.PersonID)
	origin.Capability = strings.TrimSpace(origin.Capability)
	origin.SecretaryTurnID = strings.TrimSpace(origin.SecretaryTurnID)
	origin.InputID = strings.TrimSpace(origin.InputID)
	return origin
}

func validSecretaryOriginIdentity(origin SecretaryOriginIdentity) bool {
	return origin.PersonID != "" && origin.Capability != "" && origin.SecretaryTurnID != "" && origin.InputID != ""
}

func bindSecretaryWorkerCommandOriginTx(ctx context.Context, tx *sql.Tx, origin SecretaryOriginIdentity, commandID string, now time.Time) error {
	secretaryOrigin, err := authorizedSecretaryOriginTx(ctx, tx, origin.PersonID, origin.Capability, origin.SecretaryTurnID, origin.InputID)
	if err != nil || secretaryOrigin.State != SecretaryTurnActive {
		if err != nil {
			return err
		}
		return ErrInvalidSecretaryOrigin
	}
	var state WorkerCommandState
	var attemptState AttemptState
	var workerTurnID, workerConversationID string
	err = tx.QueryRowContext(ctx, `SELECT c.state, a.turn_id, w.conversation_id, a.state
FROM phase4_worker_commands c
JOIN phase4_attempts a ON a.id = c.attempt_id
JOIN workers w ON w.id = c.worker_id
WHERE c.id = ?`, commandID).Scan(&state, &workerTurnID, &workerConversationID, &attemptState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if workerConversationID != secretaryOrigin.ConversationID {
		return ErrInvalidSecretaryOrigin
	}
	var existingTurnID, existingInputID, existingWorkerTurnID string
	err = tx.QueryRowContext(ctx, `SELECT secretary_turn_id, input_id, worker_turn_id FROM secretary_worker_command_origins WHERE command_id = ?`, commandID).Scan(&existingTurnID, &existingInputID, &existingWorkerTurnID)
	if err == nil {
		if existingTurnID != origin.SecretaryTurnID || existingInputID != origin.InputID || existingWorkerTurnID != workerTurnID {
			return ErrInvalidSecretaryOrigin
		}
		if state == WorkerCommandDelivered {
			return linkSecretaryWorkerTurnTx(ctx, tx, origin.SecretaryTurnID, origin.InputID, workerTurnID, now)
		}
		if state != WorkerCommandPending && state != WorkerCommandUncertain && state != WorkerCommandFailed {
			return ErrInvalidTransition
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if attemptState.Terminal() {
		return ErrInvalidTransition
	}
	if state == WorkerCommandDelivered {
		// A delivered legacy command without a pre-handoff intent cannot be
		// retroactively attributed to a later Secretary input.
		return ErrInvalidSecretaryOrigin
	}
	if state != WorkerCommandPending && state != WorkerCommandUncertain && state != WorkerCommandFailed {
		return ErrInvalidTransition
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO secretary_worker_command_origins(command_id, secretary_turn_id, input_id, worker_turn_id, created_at) VALUES(?, ?, ?, ?, ?)`, commandID, origin.SecretaryTurnID, origin.InputID, workerTurnID, timestamp(now))
	return err
}

func linkSecretaryWorkerTurnTx(ctx context.Context, tx *sql.Tx, secretaryTurnID, inputID, workerTurnID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO secretary_worker_origins(secretary_turn_id, input_id, worker_turn_id, created_at) VALUES(?, ?, ?, ?)`, secretaryTurnID, inputID, workerTurnID, timestamp(now)); err != nil {
		return err
	}
	return materializeSecretaryOriginResultsTx(ctx, tx, secretaryTurnID, inputID, workerTurnID, now)
}

func bindSecretaryWorkerCreationOriginTx(ctx context.Context, tx *sql.Tx, origin SecretaryOriginIdentity, conversationID, workerTurnID string, now time.Time, allowCreate bool) error {
	secretaryOrigin, err := authorizedSecretaryOriginTx(ctx, tx, origin.PersonID, origin.Capability, origin.SecretaryTurnID, origin.InputID)
	if err != nil {
		return err
	}
	if secretaryOrigin.State != SecretaryTurnActive || secretaryOrigin.ConversationID != conversationID {
		return ErrInvalidSecretaryOrigin
	}
	var workerConversationID string
	if err := tx.QueryRowContext(ctx, `SELECT w.conversation_id FROM turns t JOIN workers w ON w.id = t.worker_id WHERE t.id = ?`, workerTurnID).Scan(&workerConversationID); errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidSecretaryOrigin
	} else if err != nil {
		return err
	}
	if workerConversationID != conversationID {
		return ErrInvalidSecretaryOrigin
	}
	var existingTurnID, existingInputID string
	err = tx.QueryRowContext(ctx, `SELECT secretary_turn_id, input_id FROM secretary_worker_creation_origins WHERE worker_turn_id = ?`, workerTurnID).Scan(&existingTurnID, &existingInputID)
	if err == nil {
		if existingTurnID != origin.SecretaryTurnID || existingInputID != origin.InputID {
			return ErrInvalidSecretaryOrigin
		}
		return linkSecretaryWorkerTurnTx(ctx, tx, origin.SecretaryTurnID, origin.InputID, workerTurnID, now)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !allowCreate {
		return ErrInvalidSecretaryOrigin
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO secretary_worker_creation_origins(worker_turn_id, secretary_turn_id, input_id, created_at) VALUES(?, ?, ?, ?)`, workerTurnID, origin.SecretaryTurnID, origin.InputID, timestamp(now)); err != nil {
		return err
	}
	return linkSecretaryWorkerTurnTx(ctx, tx, origin.SecretaryTurnID, origin.InputID, workerTurnID, now)
}

// ValidateSecretaryWorkerCreationOrigin accepts only an exact replay of the
// original queued spawn; it never retroactively assigns an origin to a legacy
// or differently originated idempotency replay.
func (s *Store) ValidateSecretaryWorkerCreationOrigin(ctx context.Context, origin SecretaryOriginIdentity, workerTurnID string) error {
	found, err := s.ValidateSecretaryWorkerCreationOriginIfPresent(ctx, origin, workerTurnID)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidSecretaryOrigin
	}
	return nil
}

// ValidateSecretaryWorkerCreationOriginIfPresent returns found=false when a
// Worker Turn was not created as an addressed queued spawn; command-origin
// intents cover online dispatch and its retries.
func (s *Store) ValidateSecretaryWorkerCreationOriginIfPresent(ctx context.Context, origin SecretaryOriginIdentity, workerTurnID string) (found bool, err error) {
	origin.PersonID, origin.Capability = strings.TrimSpace(origin.PersonID), strings.TrimSpace(origin.Capability)
	origin.SecretaryTurnID, origin.InputID = strings.TrimSpace(origin.SecretaryTurnID), strings.TrimSpace(origin.InputID)
	workerTurnID = strings.TrimSpace(workerTurnID)
	if origin.PersonID == "" || origin.Capability == "" || origin.SecretaryTurnID == "" || origin.InputID == "" || workerTurnID == "" {
		return false, ErrInvalidSecretaryOrigin
	}
	result, err := withTx(s, ctx, func(tx *sql.Tx) (bool, error) {
		secretaryOrigin, err := authorizedSecretaryOriginTx(ctx, tx, origin.PersonID, origin.Capability, origin.SecretaryTurnID, origin.InputID)
		if err != nil || secretaryOrigin.State != SecretaryTurnActive {
			if err != nil {
				return false, err
			}
			return false, ErrInvalidSecretaryOrigin
		}
		var existingTurnID, existingInputID string
		err = tx.QueryRowContext(ctx, `SELECT secretary_turn_id, input_id FROM secretary_worker_creation_origins WHERE worker_turn_id = ?`, workerTurnID).Scan(&existingTurnID, &existingInputID)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if existingTurnID != origin.SecretaryTurnID || existingInputID != origin.InputID {
			return false, ErrInvalidSecretaryOrigin
		}
		var workerConversationID string
		if err := tx.QueryRowContext(ctx, `SELECT w.conversation_id FROM turns t JOIN workers w ON w.id = t.worker_id WHERE t.id = ?`, workerTurnID).Scan(&workerConversationID); err != nil {
			return false, err
		}
		if workerConversationID != secretaryOrigin.ConversationID {
			return false, ErrInvalidSecretaryOrigin
		}
		return true, nil
	})
	return result, err
}

func linkSecretaryWorkerCommandOriginsTx(ctx context.Context, tx *sql.Tx, commandID string, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT secretary_turn_id, input_id, worker_turn_id FROM secretary_worker_command_origins WHERE command_id = ? ORDER BY secretary_turn_id, input_id`, commandID)
	if err != nil {
		return err
	}
	type originLink struct{ turnID, inputID, workerTurnID string }
	var links []originLink
	for rows.Next() {
		var link originLink
		if err := rows.Scan(&link.turnID, &link.inputID, &link.workerTurnID); err != nil {
			rows.Close()
			return err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, link := range links {
		if err := linkSecretaryWorkerTurnTx(ctx, tx, link.turnID, link.inputID, link.workerTurnID, now); err != nil {
			return err
		}
	}
	return nil
}

// RecordAddressedSecretaryTextDelta publishes a stream delta only while the
// exact originating turn/input has neither a typed reply nor a linked Result.
// The check and event commit share one transaction, defining their ordering.
func (s *Store) RecordAddressedSecretaryTextDelta(ctx context.Context, turnID, inputID, text string) (Event, bool, error) {
	turnID, inputID = strings.TrimSpace(turnID), strings.TrimSpace(inputID)
	if turnID == "" || inputID == "" || text == "" {
		return Event{}, false, ErrInvalidSecretaryOrigin
	}
	result, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		event    Event
		recorded bool
	}, error) {
		var actualInputID string
		var state SecretaryTurnState
		if err := tx.QueryRowContext(ctx, `SELECT input_id, state FROM secretary_turns WHERE id = ?`, turnID).Scan(&actualInputID, &state); errors.Is(err, sql.ErrNoRows) {
			return struct {
				event    Event
				recorded bool
			}{}, ErrNotFound
		} else if err != nil {
			return struct {
				event    Event
				recorded bool
			}{}, err
		}
		if actualInputID != inputID {
			return struct {
				event    Event
				recorded bool
			}{}, ErrInvalidSecretaryOrigin
		}
		if state != SecretaryTurnActive {
			return struct {
				event    Event
				recorded bool
			}{}, ErrInvalidTransition
		}
		var addressed, relatedResult int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM secretary_reply_entries WHERE secretary_turn_id = ? AND input_id = ?), EXISTS(SELECT 1 FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?)`, turnID, inputID, turnID, inputID).Scan(&addressed, &relatedResult); err != nil {
			return struct {
				event    Event
				recorded bool
			}{}, err
		}
		if addressed != 0 || relatedResult != 0 {
			return struct {
				event    Event
				recorded bool
			}{}, nil
		}
		payload := map[string]any{"turn_id": turnID, "text": text}
		now := s.now()
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTextDeltaEvent, AggregateType: "secretary_turn", AggregateID: turnID, Source: "secretary", CorrelationID: turnID, Payload: payload}, payload)
		if err != nil {
			return struct {
				event    Event
				recorded bool
			}{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "secretary-stream:"+event.ID); err != nil {
			return struct {
				event    Event
				recorded bool
			}{}, err
		}
		return struct {
			event    Event
			recorded bool
		}{event: event, recorded: true}, nil
	})
	if err != nil {
		return Event{}, false, err
	}
	return result.event, result.recorded, nil
}

// RecordSecretaryReply atomically persists one addressed user reply per
// server-issued Secretary turn/input identity. Exact replays return the same
// Conversation entry without creating another entry or Secretary turn.
func (s *Store) RecordSecretaryReply(ctx context.Context, personID, capability, secretaryTurnID, inputID, text string) (ConversationEntry, bool, error) {
	personID, capability = strings.TrimSpace(personID), strings.TrimSpace(capability)
	secretaryTurnID, inputID = strings.TrimSpace(secretaryTurnID), strings.TrimSpace(inputID)
	text = strings.TrimSpace(text)
	if personID == "" || capability == "" || secretaryTurnID == "" || inputID == "" || text == "" {
		return ConversationEntry{}, false, ErrInvalidSecretaryOrigin
	}
	result, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		entry     ConversationEntry
		duplicate bool
	}, error) {
		origin, err := authorizedSecretaryOriginTx(ctx, tx, personID, capability, secretaryTurnID, inputID)
		if err != nil {
			return struct {
				entry     ConversationEntry
				duplicate bool
			}{}, err
		}
		var priorBody, priorEntryID string
		err = tx.QueryRowContext(ctx, `SELECT body, entry_id FROM secretary_reply_entries WHERE secretary_turn_id = ? AND input_id = ?`, secretaryTurnID, inputID).Scan(&priorBody, &priorEntryID)
		if err == nil {
			if priorBody != text {
				return struct {
					entry     ConversationEntry
					duplicate bool
				}{}, ErrSecretaryReplyConflict
			}
			entry, err := getEntry(ctx, tx, priorEntryID)
			return struct {
				entry     ConversationEntry
				duplicate bool
			}{entry: entry, duplicate: true}, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return struct {
				entry     ConversationEntry
				duplicate bool
			}{}, err
		}
		if origin.State != SecretaryTurnActive {
			return struct {
				entry     ConversationEntry
				duplicate bool
			}{}, ErrInvalidSecretaryOrigin
		}
		entry, err := appendEntry(ctx, tx, s.now(), origin.ConversationID, EntrySecretary, text)
		if err != nil {
			return struct {
				entry     ConversationEntry
				duplicate bool
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO secretary_reply_entries(secretary_turn_id, input_id, body, entry_id, created_at) VALUES(?, ?, ?, ?, ?)`, secretaryTurnID, inputID, text, entry.ID, timestamp(entry.CreatedAt)); err != nil {
			return struct {
				entry     ConversationEntry
				duplicate bool
			}{}, err
		}
		return struct {
			entry     ConversationEntry
			duplicate bool
		}{entry: entry}, nil
	})
	if err != nil {
		return ConversationEntry{}, false, err
	}
	if !result.duplicate {
		s.notifyEntry(result.entry)
	}
	return result.entry, result.duplicate, nil
}

// SecretaryOriginHasResult reports only canonical Worker Results linked by an
// accepted MCP action to this exact originating turn/input pair.
func (s *Store) SecretaryOriginHasResult(ctx context.Context, secretaryTurnID, inputID string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?)`, strings.TrimSpace(secretaryTurnID), strings.TrimSpace(inputID)).Scan(&exists)
	return exists != 0, err
}

// SecretaryHasAddressedReply reports whether the server already committed the
// unique typed reply for this originating identity.
func (s *Store) SecretaryHasAddressedReply(ctx context.Context, secretaryTurnID, inputID string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM secretary_reply_entries WHERE secretary_turn_id = ? AND input_id = ?)`, strings.TrimSpace(secretaryTurnID), strings.TrimSpace(inputID)).Scan(&exists)
	return exists != 0, err
}

type authorizedSecretaryOrigin struct {
	ConversationID string
	State          SecretaryTurnState
}

func authorizedSecretaryOriginTx(ctx context.Context, tx *sql.Tx, personID, capability, secretaryTurnID, inputID string) (authorizedSecretaryOrigin, error) {
	var origin authorizedSecretaryOrigin
	err := tx.QueryRowContext(ctx, `SELECT t.conversation_id, t.state
FROM secretary_turns t
JOIN secretary_identities i ON i.id = t.identity_id
JOIN secretary_capabilities c ON c.person_id = i.person_id
WHERE t.id = ? AND t.input_id = ? AND i.person_id = ? AND c.token_hash = ? AND c.revoked_at IS NULL`, secretaryTurnID, inputID, personID, hashToken(capability)).Scan(&origin.ConversationID, &origin.State)
	if errors.Is(err, sql.ErrNoRows) {
		return authorizedSecretaryOrigin{}, ErrInvalidSecretaryOrigin
	}
	return origin, err
}

func materializeSecretaryOriginResultsTx(ctx context.Context, tx *sql.Tx, secretaryTurnID, inputID, workerTurnID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO secretary_origin_results(secretary_turn_id, input_id, worker_turn_id, result_id, created_at)
SELECT o.secretary_turn_id, o.input_id, o.worker_turn_id, r.id, ?
FROM secretary_worker_origins o
JOIN phase4_results r ON r.turn_id = o.worker_turn_id
JOIN conversation_entries e ON e.result_id = r.id AND e.kind = ?
WHERE o.secretary_turn_id = ? AND o.input_id = ? AND o.worker_turn_id = ?`, timestamp(now), EntryWorkerResult, secretaryTurnID, inputID, workerTurnID)
	return err
}

func recordSecretaryOriginResultsForWorkerTurnTx(ctx context.Context, tx *sql.Tx, workerTurnID string, now time.Time) error {
	// Join explicit non-failed command intents to a Result only for the exact
	// immutable Worker Turn they target, even if the delivery receipt has not
	// committed yet. Earlier Turns/Results and inferred ACP/text identities do
	// not participate in this relation.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO secretary_worker_origins(secretary_turn_id, input_id, worker_turn_id, created_at)
SELECT o.secretary_turn_id, o.input_id, o.worker_turn_id, ?
FROM secretary_worker_command_origins o
JOIN phase4_worker_commands c ON c.id = o.command_id
WHERE o.worker_turn_id = ? AND c.state IN (?, ?, ?)`, timestamp(now), workerTurnID, WorkerCommandPending, WorkerCommandUncertain, WorkerCommandDelivered); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO secretary_origin_results(secretary_turn_id, input_id, worker_turn_id, result_id, created_at)
SELECT o.secretary_turn_id, o.input_id, o.worker_turn_id, r.id, ?
FROM secretary_worker_origins o
JOIN phase4_results r ON r.turn_id = o.worker_turn_id
JOIN conversation_entries e ON e.result_id = r.id AND e.kind = ?
WHERE o.worker_turn_id = ?`, timestamp(now), EntryWorkerResult, workerTurnID)
	return err
}
