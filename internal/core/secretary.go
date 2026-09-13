package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const maxUserDocumentBytes = 1 << 20

func (s *Store) migrateSecretarySchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS secretary_identities (
  id TEXT PRIMARY KEY,
  person_id TEXT NOT NULL UNIQUE REFERENCES persons(id),
  conversation_id TEXT NOT NULL UNIQUE REFERENCES conversations(id),
  runtime_generation INTEGER NOT NULL DEFAULT 0,
  runtime_harness TEXT NOT NULL DEFAULT '',
  runtime_model TEXT NOT NULL DEFAULT '',
  runtime_reasoning TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS secretary_turns (
  id TEXT PRIMARY KEY,
  identity_id TEXT NOT NULL REFERENCES secretary_identities(id),
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  input TEXT NOT NULL,
  context_snapshot TEXT NOT NULL DEFAULT '',
  prompt_state TEXT NOT NULL DEFAULT 'pending',
  state TEXT NOT NULL,
  queue_position INTEGER NOT NULL,
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS secretary_turns_one_active
  ON secretary_turns(identity_id) WHERE state = 'active';
CREATE UNIQUE INDEX IF NOT EXISTS secretary_turns_queue_position
  ON secretary_turns(identity_id, queue_position) WHERE state = 'queued';
CREATE TABLE IF NOT EXISTS secretary_user_documents (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  path TEXT NOT NULL,
  revision INTEGER NOT NULL,
  content TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS secretary_conversation_summaries (
  conversation_id TEXT PRIMARY KEY REFERENCES conversations(id),
  summary TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS secretary_policy_snapshots (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  snapshot_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS secretary_context_seen_results (
  turn_id TEXT NOT NULL REFERENCES secretary_turns(id),
  result_id TEXT NOT NULL REFERENCES phase4_results(id),
  seen_at TEXT NOT NULL,
  claim_state TEXT NOT NULL DEFAULT 'accepted',
  PRIMARY KEY(turn_id, result_id)
);
DELETE FROM secretary_context_seen_results
WHERE rowid NOT IN (SELECT MIN(rowid) FROM secretary_context_seen_results GROUP BY result_id);
CREATE UNIQUE INDEX IF NOT EXISTS secretary_context_seen_results_one_owner
  ON secretary_context_seen_results(result_id);
`)
	if err != nil {
		return fmt.Errorf("migrate Secretary schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE secretary_turns ADD COLUMN context_snapshot TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return fmt.Errorf("migrate Secretary turn context snapshot: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE secretary_turns ADD COLUMN prompt_state TEXT NOT NULL DEFAULT 'pending'`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return fmt.Errorf("migrate Secretary turn prompt state: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE secretary_context_seen_results ADD COLUMN claim_state TEXT NOT NULL DEFAULT 'accepted'`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return fmt.Errorf("migrate Secretary result claim state: %w", err)
	}
	return nil
}

func scanSecretaryIdentity(row interface{ Scan(...any) error }, identity *SecretaryIdentity) error {
	return row.Scan(&identity.ID, &identity.PersonID, &identity.ConversationID, &identity.RuntimeGeneration, &identity.RuntimeHarness, &identity.RuntimeModel, &identity.RuntimeReasoning, newTimestampScanner(&identity.CreatedAt), newTimestampScanner(&identity.UpdatedAt))
}

func (s *Store) EnsureSecretaryIdentity(ctx context.Context, personID, conversationID string) (SecretaryIdentity, error) {
	if strings.TrimSpace(personID) == "" || strings.TrimSpace(conversationID) == "" {
		return SecretaryIdentity{}, errors.New("core: person and conversation are required")
	}
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryIdentity, error) {
		var identity SecretaryIdentity
		err := scanSecretaryIdentity(tx.QueryRowContext(ctx, `SELECT id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at FROM secretary_identities WHERE person_id = ?`, personID), &identity)
		if err == nil {
			if identity.ConversationID != conversationID {
				return SecretaryIdentity{}, errors.New("core: person already has another Secretary conversation")
			}
			return identity, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return SecretaryIdentity{}, err
		}
		now := s.now()
		identity = SecretaryIdentity{ID: newID("sec"), PersonID: personID, ConversationID: conversationID, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO secretary_identities(id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at) VALUES(?, ?, ?, 0, '', '', '', ?, ?)`, identity.ID, identity.PersonID, identity.ConversationID, timestamp(now), timestamp(now)); err != nil {
			return SecretaryIdentity{}, err
		}
		return identity, nil
	})
}

func (s *Store) SecretaryIdentity(ctx context.Context, personID string) (SecretaryIdentity, error) {
	var identity SecretaryIdentity
	err := scanSecretaryIdentity(s.db.QueryRowContext(ctx, `SELECT id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at FROM secretary_identities WHERE person_id = ?`, personID), &identity)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryIdentity{}, ErrNotFound
	}
	return identity, err
}

func (s *Store) ReplaceSecretaryRuntime(ctx context.Context, identityID, harness, model, reasoning string) (SecretaryIdentity, error) {
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(harness) == "" || strings.TrimSpace(model) == "" || strings.TrimSpace(reasoning) == "" {
		return SecretaryIdentity{}, errors.New("core: complete Secretary runtime policy is required")
	}
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryIdentity, error) {
		var identity SecretaryIdentity
		if err := scanSecretaryIdentity(tx.QueryRowContext(ctx, `SELECT id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at FROM secretary_identities WHERE id = ?`, identityID), &identity); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return SecretaryIdentity{}, ErrNotFound
			}
			return SecretaryIdentity{}, err
		}
		now := s.now()
		identity.RuntimeGeneration++
		identity.RuntimeHarness, identity.RuntimeModel, identity.RuntimeReasoning, identity.UpdatedAt = harness, model, reasoning, now
		_, err := tx.ExecContext(ctx, `UPDATE secretary_identities SET runtime_generation = ?, runtime_harness = ?, runtime_model = ?, runtime_reasoning = ?, updated_at = ? WHERE id = ?`, identity.RuntimeGeneration, identity.RuntimeHarness, identity.RuntimeModel, identity.RuntimeReasoning, timestamp(now), identity.ID)
		return identity, err
	})
}

func (s *Store) EnqueueSecretaryTurn(ctx context.Context, identityID, input string) (SecretaryTurn, error) {
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(input) == "" {
		return SecretaryTurn{}, errors.New("core: Secretary identity and input are required")
	}
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryTurn, error) {
		var conversationID string
		if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM secretary_identities WHERE id = ?`, identityID).Scan(&conversationID); errors.Is(err, sql.ErrNoRows) {
			return SecretaryTurn{}, ErrNotFound
		} else if err != nil {
			return SecretaryTurn{}, err
		}
		var position int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(queue_position), 0) + 1 FROM secretary_turns WHERE identity_id = ? AND state = 'queued'`, identityID).Scan(&position); err != nil {
			return SecretaryTurn{}, err
		}
		now := s.now()
		turn := SecretaryTurn{ID: newID("stn"), IdentityID: identityID, ConversationID: conversationID, Input: input, State: SecretaryTurnQueued, QueuePosition: position, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO secretary_turns(id, identity_id, conversation_id, input, state, queue_position, error, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, '', ?, ?)`, turn.ID, turn.IdentityID, turn.ConversationID, turn.Input, turn.State, turn.QueuePosition, timestamp(now), timestamp(now)); err != nil {
			return SecretaryTurn{}, err
		}
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTurnQueuedEvent, AggregateType: "secretary_turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID, Payload: turn}, turn)
		if err != nil {
			return SecretaryTurn{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "secretary-stream:"+event.ID); err != nil {
			return SecretaryTurn{}, err
		}
		return turn, nil
	})
}

func scanSecretaryTurn(row interface{ Scan(...any) error }, turn *SecretaryTurn) error {
	var started, finished sql.NullString
	if err := row.Scan(&turn.ID, &turn.IdentityID, &turn.ConversationID, &turn.Input, &turn.ContextSnapshot, &turn.PromptState, &turn.State, &turn.QueuePosition, &turn.Error, newTimestampScanner(&turn.CreatedAt), &started, &finished, newTimestampScanner(&turn.UpdatedAt)); err != nil {
		return err
	}
	if started.Valid {
		value, err := parseTimestamp(started.String)
		if err != nil {
			return err
		}
		turn.StartedAt = &value
	}
	if finished.Valid {
		value, err := parseTimestamp(finished.String)
		if err != nil {
			return err
		}
		turn.FinishedAt = &value
	}
	return nil
}

func (s *Store) SecretaryTurn(ctx context.Context, turnID string) (SecretaryTurn, error) {
	var turn SecretaryTurn
	err := scanSecretaryTurn(s.db.QueryRowContext(ctx, secretaryTurnSelect+` WHERE id = ?`, turnID), &turn)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryTurn{}, ErrNotFound
	}
	return turn, err
}

func (s *Store) StartSecretaryTurn(ctx context.Context, turnID string) (SecretaryTurn, error) {
	turn, err := s.SecretaryTurn(ctx, turnID)
	if err != nil {
		return SecretaryTurn{}, err
	}
	var canonical SecretaryContext
	if strings.TrimSpace(turn.ContextSnapshot) == "" {
		if turn.State != SecretaryTurnQueued {
			return SecretaryTurn{}, ErrInvalidTransition
		}
		canonical, err = s.ReconstructSecretaryContext(ctx, turn.IdentityID, "", 20)
		if err != nil {
			return SecretaryTurn{}, fmt.Errorf("reconstruct Secretary context: %w", err)
		}
	}
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryTurn, error) {
		var current SecretaryTurn
		if err := scanSecretaryTurn(tx.QueryRowContext(ctx, secretaryTurnSelect+` WHERE id = ?`, turnID), &current); errors.Is(err, sql.ErrNoRows) {
			return SecretaryTurn{}, ErrNotFound
		} else if err != nil {
			return SecretaryTurn{}, err
		}
		if current.State != SecretaryTurnQueued {
			return SecretaryTurn{}, ErrInvalidTransition
		}
		eligible, err := secretaryTurnEligibleTx(ctx, tx, current)
		if err != nil {
			return SecretaryTurn{}, err
		}
		if !eligible {
			return SecretaryTurn{}, ErrInvalidTransition
		}
		if strings.TrimSpace(current.ContextSnapshot) == "" {
			results, err := unseenWorkerResultsQuery(ctx, tx, current.ConversationID)
			if err != nil {
				return SecretaryTurn{}, err
			}
			canonical.UnseenWorkerResults, err = claimSecretaryResultsTx(ctx, tx, current.ID, results, s.now())
			if err != nil {
				return SecretaryTurn{}, err
			}
			encoded, err := json.Marshal(canonical)
			if err != nil {
				return SecretaryTurn{}, err
			}
			result, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET context_snapshot = ?, updated_at = ? WHERE id = ? AND state = ? AND context_snapshot = ''`, string(encoded), timestamp(s.now()), current.ID, SecretaryTurnQueued)
			if err != nil {
				return SecretaryTurn{}, err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return SecretaryTurn{}, err
			}
			if affected != 1 {
				return SecretaryTurn{}, errors.New("core: canonical Secretary context snapshot was not published")
			}
			current.ContextSnapshot = string(encoded)
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET state = ?, started_at = ?, updated_at = ? WHERE id = ?`, SecretaryTurnActive, timestamp(now), timestamp(now), current.ID); err != nil {
			return SecretaryTurn{}, err
		}
		current.State, current.StartedAt, current.UpdatedAt = SecretaryTurnActive, &now, now
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTurnStartedEvent, AggregateType: "secretary_turn", AggregateID: current.ID, Source: "server", CorrelationID: current.ID, Payload: current}, current)
		if err != nil {
			return SecretaryTurn{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "secretary-stream:"+event.ID); err != nil {
			return SecretaryTurn{}, err
		}
		return current, nil
	})
}

func (s *Store) StartNextSecretaryTurn(ctx context.Context, identityID string) (SecretaryTurn, error) {
	var turnID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM secretary_turns WHERE identity_id = ? AND state = 'queued' ORDER BY queue_position, created_at, id LIMIT 1`, identityID).Scan(&turnID)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryTurn{}, ErrNotFound
	}
	if err != nil {
		return SecretaryTurn{}, err
	}
	return s.StartSecretaryTurn(ctx, turnID)
}

func (s *Store) FinishSecretaryTurn(ctx context.Context, turnID string, state SecretaryTurnState, terminalError string) (SecretaryTurn, error) {
	if !state.Terminal() {
		return SecretaryTurn{}, errors.New("core: Secretary turn must finish in a terminal state")
	}
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryTurn, error) {
		var turn SecretaryTurn
		if err := scanSecretaryTurn(tx.QueryRowContext(ctx, secretaryTurnSelect+` WHERE id = ?`, turnID), &turn); errors.Is(err, sql.ErrNoRows) {
			return SecretaryTurn{}, ErrNotFound
		} else if err != nil {
			return SecretaryTurn{}, err
		}
		if turn.State != SecretaryTurnActive {
			return SecretaryTurn{}, ErrInvalidTransition
		}
		if state == SecretaryTurnSucceeded {
			if _, err := tx.ExecContext(ctx, `UPDATE secretary_context_seen_results SET claim_state = 'accepted' WHERE turn_id = ? AND claim_state = 'claimed'`, turn.ID); err != nil {
				return SecretaryTurn{}, err
			}
		} else if turn.PromptState != secretaryPromptAccepted {
			if err := releaseSecretaryTurnClaimsForStateTx(ctx, tx, turn.ID, turn.State, turn.PromptState); err != nil {
				return SecretaryTurn{}, err
			}
		}
		now := s.now()
		turn.State, turn.Error, turn.FinishedAt, turn.UpdatedAt = state, strings.TrimSpace(terminalError), &now, now
		if _, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET state = ?, error = ?, finished_at = ?, updated_at = ? WHERE id = ?`, turn.State, turn.Error, timestamp(now), timestamp(now), turn.ID); err != nil {
			return SecretaryTurn{}, err
		}
		payload := map[string]any{"turn_id": turn.ID, "status": string(state)}
		if turn.Error != "" {
			payload["error"] = turn.Error
		}
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: SecretaryTurnFinishedEvent, AggregateType: "secretary_turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID, Payload: payload}, payload)
		if err != nil {
			return SecretaryTurn{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "secretary-stream:"+event.ID); err != nil {
			return SecretaryTurn{}, err
		}
		return turn, nil
	})
}

func safeSummary(summary string) (string, error) {
	summary = strings.TrimSpace(strings.Trim(summary, "`"))
	if summary == "" || len(summary) > 1000 {
		return "", errors.New("core: thinking summary must be short and non-empty")
	}
	lower := strings.ToLower(summary)
	for _, marker := range []string{"chain-of-thought", "chain of thought", "raw thought", "internal reasoning", "thought process", "<think>", "</think>"} {
		if strings.Contains(lower, marker) {
			return "", errors.New("core: raw chain-of-thought is not allowed")
		}
	}
	return summary, nil
}

func (s *Store) AppendSecretaryEvent(ctx context.Context, input SecretaryEventInput) (Event, error) {
	if strings.TrimSpace(input.TurnID) == "" {
		return Event{}, errors.New("core: Secretary turn is required")
	}
	allowed := map[string]bool{SecretaryTextDeltaEvent: true, SecretaryThinkingSummaryEvent: true, SecretaryToolCallEvent: true, SecretaryToolResultEvent: true}
	if !allowed[input.Kind] {
		return Event{}, fmt.Errorf("core: unsupported Secretary stream event %q", input.Kind)
	}
	payload := map[string]any{"turn_id": input.TurnID}
	switch input.Kind {
	case SecretaryTextDeltaEvent:
		if input.Text == "" {
			return Event{}, errors.New("core: text delta is required")
		}
		payload["text"] = input.Text
	case SecretaryThinkingSummaryEvent:
		summary, err := safeSummary(input.Summary)
		if err != nil {
			return Event{}, err
		}
		payload["summary"] = summary
	case SecretaryToolCallEvent:
		if strings.TrimSpace(input.Tool) == "" {
			return Event{}, errors.New("core: tool name is required")
		}
		payload["tool"] = input.Tool
		payload["arguments"] = input.Arguments
	case SecretaryToolResultEvent:
		if strings.TrimSpace(input.Tool) == "" {
			return Event{}, errors.New("core: tool name is required")
		}
		payload["tool"] = input.Tool
		payload["result"] = input.Result
		payload["status"] = input.Status
		if input.Error != "" {
			payload["error"] = input.Error
		}
	}
	if input.Payload != nil {
		return Event{}, errors.New("core: arbitrary Secretary payload is not allowed")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Event, error) {
		var identityID string
		var turnState SecretaryTurnState
		if err := tx.QueryRowContext(ctx, `SELECT identity_id, state FROM secretary_turns WHERE id = ?`, input.TurnID).Scan(&identityID, &turnState); errors.Is(err, sql.ErrNoRows) {
			return Event{}, ErrNotFound
		} else if err != nil {
			return Event{}, err
		}
		if turnState != SecretaryTurnActive {
			return Event{}, ErrInvalidTransition
		}
		if key != "" {
			var encoded string
			if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = 'secretary.event' AND idempotency_key = ?`, key).Scan(&encoded); err == nil {
				var event Event
				if err := json.Unmarshal([]byte(encoded), &event); err != nil {
					return Event{}, err
				}
				return event, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return Event{}, err
			}
		}
		event, err := appendEventTx(ctx, tx, s.now(), EventInput{Kind: input.Kind, AggregateType: "secretary_turn", AggregateID: input.TurnID, Source: "secretary", CorrelationID: input.TurnID, Payload: payload}, payload)
		if err != nil {
			return Event{}, err
		}
		if key != "" {
			encoded, err := json.Marshal(event)
			if err != nil {
				return Event{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES('secretary.event', ?, ?, ?)`, key, string(encoded), timestamp(s.now())); err != nil {
				return Event{}, err
			}
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, s.now(), event.ID, "", "conversation", "secretary-stream:"+event.ID); err != nil {
			return Event{}, err
		}
		return event, nil
	})
}

func (s *Store) SecretaryEvents(ctx context.Context, turnID string, afterSeq int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 501 {
		limit = 501
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE aggregate_type = 'secretary_turn' AND aggregate_id = ? AND seq > ? ORDER BY seq LIMIT ?`, turnID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) ReplaySecretaryEvents(ctx context.Context, turnID string, afterSeq int64, limit int) (EventReplay, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	return withTx(s, ctx, func(tx *sql.Tx) (EventReplay, error) {
		var boundary int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events`).Scan(&boundary); err != nil {
			return EventReplay{}, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, seq, kind, aggregate_type, aggregate_id, source, correlation_id, causation_id, worker_ref, attempt_id, payload_json, created_at FROM events WHERE aggregate_type = 'secretary_turn' AND aggregate_id = ? AND seq > ? AND seq <= ? ORDER BY seq LIMIT ?`, turnID, afterSeq, boundary, limit+1)
		if err != nil {
			return EventReplay{}, err
		}
		defer rows.Close()
		events, err := scanEvents(rows)
		if err != nil {
			return EventReplay{}, err
		}
		hasMore := len(events) > limit
		if hasMore {
			events = events[:limit]
		}
		last := afterSeq
		if len(events) > 0 {
			last = events[len(events)-1].Seq
		}
		return EventReplay{SnapshotBoundarySeq: boundary, BoundarySeq: boundary, LastReturnedSeq: last, HasMore: hasMore, Events: events}, nil
	})
}

// SubscribeSecretaryEvents replays durable events and then polls the same log.
// Sequence numbers make reconnects and repeated notifications idempotent.
func (s *Store) SubscribeSecretaryEvents(ctx context.Context, turnID string, afterSeq int64) (<-chan Event, error) {
	if strings.TrimSpace(turnID) == "" {
		return nil, errors.New("core: Secretary turn is required")
	}
	channel := make(chan Event, 32)
	go func() {
		defer close(channel)
		last := afterSeq
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			events, err := s.SecretaryEvents(ctx, turnID, last, 500)
			if err == nil {
				for _, event := range events {
					if event.Seq <= last {
						continue
					}
					select {
					case channel <- event:
						last = event.Seq
					case <-ctx.Done():
						return
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return channel, nil
}

func (s *Store) ReplaySecretaryStream(ctx context.Context, turnID string, afterSeq int64, limit int) (EventReplay, error) {
	return s.ReplaySecretaryEvents(ctx, turnID, afterSeq, limit)
}

// User document operations are kept in core because its revision is part of
// the durable server snapshot, while the Markdown bytes remain external.
func ValidateUserDocument(content string) error {
	if !utf8.ValidString(content) {
		return errors.New("core: user.md must be valid UTF-8")
	}
	if strings.IndexByte(content, 0) >= 0 {
		return errors.New("core: user.md contains NUL")
	}
	if len(content) > maxUserDocumentBytes {
		return errors.New("core: user.md is too large")
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".user.md.*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s *Store) LoadUserDocument(ctx context.Context, path string) (UserDocument, error) {
	s.userDocumentMu.Lock()
	defer s.userDocumentMu.Unlock()
	var current UserDocument
	var storedPath string
	err := s.db.QueryRowContext(ctx, `SELECT path, revision, content, updated_at FROM secretary_user_documents WHERE id = 1`).Scan(&storedPath, &current.Revision, &current.Content, newTimestampScanner(&current.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		if path == "" {
			path = "user.md"
		}
		content, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			content = []byte{}
		} else if readErr != nil {
			return UserDocument{}, readErr
		}
		if err := ValidateUserDocument(string(content)); err != nil {
			return UserDocument{}, err
		}
		now := s.now()
		current = UserDocument{Path: path, Revision: 1, Content: string(content), UpdatedAt: now}
		_, err := s.db.ExecContext(ctx, `INSERT INTO secretary_user_documents(id, path, revision, content, updated_at) VALUES(1, ?, ?, ?, ?)`, path, current.Revision, current.Content, timestamp(now))
		return current, err
	}
	if err != nil {
		return UserDocument{}, err
	}
	current.Path = storedPath
	readPath := path
	if readPath == "" {
		readPath = storedPath
	}
	content, readErr := os.ReadFile(readPath)
	if readErr == nil {
		if ValidateUserDocument(string(content)) == nil && string(content) != current.Content {
			now := s.now()
			current.Content, current.Revision, current.UpdatedAt = string(content), current.Revision+1, now
			if _, err := s.db.ExecContext(ctx, `UPDATE secretary_user_documents SET path = ?, revision = ?, content = ?, updated_at = ? WHERE id = 1`, readPath, current.Revision, current.Content, timestamp(now)); err != nil {
				return UserDocument{}, err
			}
		}
	}
	return current, nil
}

var ErrUserDocumentRevisionConflict = errors.New("core: user.md revision conflict")

func (s *Store) SaveUserDocumentIfRevision(ctx context.Context, path, content string, expectedRevision int64) (UserDocument, error) {
	if err := ValidateUserDocument(content); err != nil {
		return UserDocument{}, err
	}
	s.userDocumentMu.Lock()
	defer s.userDocumentMu.Unlock()
	previous, err := s.LoadUserDocumentUnlocked(ctx, path)
	if err != nil {
		return UserDocument{}, err
	}
	if expectedRevision > 0 && previous.Revision != expectedRevision {
		return UserDocument{}, ErrUserDocumentRevisionConflict
	}
	if path == "" {
		path = previous.Path
	}
	if path == "" {
		path = "user.md"
	}
	if err := atomicWrite(path, []byte(content)); err != nil {
		return UserDocument{}, err
	}
	now := s.now()
	next := UserDocument{Path: path, Revision: previous.Revision + 1, Content: content, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO secretary_user_documents(id, path, revision, content, updated_at) VALUES(1, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET path = excluded.path, revision = excluded.revision, content = excluded.content, updated_at = excluded.updated_at`, path, next.Revision, next.Content, timestamp(now))
	if err != nil {
		_ = atomicWrite(path, []byte(previous.Content))
		return UserDocument{}, err
	}
	return next, nil
}

func (s *Store) SaveUserDocument(ctx context.Context, path, content string) (UserDocument, error) {
	return s.SaveUserDocumentIfRevision(ctx, path, content, 0)
}

func (s *Store) LoadUserDocumentUnlocked(ctx context.Context, path string) (UserDocument, error) {
	var current UserDocument
	var storedPath string
	err := s.db.QueryRowContext(ctx, `SELECT path, revision, content, updated_at FROM secretary_user_documents WHERE id = 1`).Scan(&storedPath, &current.Revision, &current.Content, newTimestampScanner(&current.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		if path == "" {
			path = "user.md"
		}
		content, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			content = []byte{}
		} else if readErr != nil {
			return UserDocument{}, readErr
		}
		if err := ValidateUserDocument(string(content)); err != nil {
			return UserDocument{}, err
		}
		now := s.now()
		current = UserDocument{Path: path, Revision: 1, Content: string(content), UpdatedAt: now}
		_, err := s.db.ExecContext(ctx, `INSERT INTO secretary_user_documents(id, path, revision, content, updated_at) VALUES(1, ?, ?, ?, ?)`, path, 1, string(content), timestamp(now))
		return current, err
	}
	if err != nil {
		return UserDocument{}, err
	}
	current.Path = storedPath
	readPath := path
	if readPath == "" {
		readPath = storedPath
	}
	if content, readErr := os.ReadFile(readPath); readErr == nil && ValidateUserDocument(string(content)) == nil && string(content) != current.Content {
		now := s.now()
		current.Content, current.Revision, current.UpdatedAt = string(content), current.Revision+1, now
		if _, err := s.db.ExecContext(ctx, `UPDATE secretary_user_documents SET path = ?, revision = ?, content = ?, updated_at = ? WHERE id = 1`, readPath, current.Revision, current.Content, timestamp(now)); err != nil {
			return UserDocument{}, err
		}
	}
	return current, nil
}
