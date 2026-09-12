package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Phase4Attempt and Phase4Result deliberately use separate storage from the
// Phase 3 Attempt/Result tables. Those tables retain their foreign keys to
// legacy worker_bindings, while this schema has no runtime-session column.
func (s *Store) migratePhase4Lifecycle(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS phase4_attempts (
  id TEXT PRIMARY KEY,
  worker_id TEXT NOT NULL REFERENCES workers(id),
  turn_id TEXT NOT NULL REFERENCES turns(id),
  number INTEGER NOT NULL,
  node_id TEXT NOT NULL,
  harness_instance_id TEXT NOT NULL,
  state TEXT NOT NULL,
  correlation_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(turn_id, number)
);
CREATE UNIQUE INDEX IF NOT EXISTS phase4_attempts_one_active
  ON phase4_attempts(turn_id) WHERE state IN ('starting', 'active');
CREATE TABLE IF NOT EXISTS phase4_attempt_outcomes (
  id TEXT PRIMARY KEY,
  attempt_id TEXT NOT NULL UNIQUE REFERENCES phase4_attempts(id),
  status TEXT NOT NULL,
  classification TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  diagnostics TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS phase4_results (
  id TEXT PRIMARY KEY,
  worker_id TEXT NOT NULL REFERENCES workers(id),
  turn_id TEXT NOT NULL UNIQUE REFERENCES turns(id),
  attempt_id TEXT NOT NULL UNIQUE REFERENCES phase4_attempts(id),
  status TEXT NOT NULL,
  summary TEXT NOT NULL,
  failure_code TEXT NOT NULL DEFAULT '',
  artifact_refs TEXT NOT NULL DEFAULT '',
  correlation_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS phase4_results_one_per_attempt
  ON phase4_results(attempt_id);
CREATE TABLE IF NOT EXISTS phase4_worker_commands (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  dedupe_key TEXT NOT NULL,
  worker_id TEXT NOT NULL REFERENCES workers(id),
  attempt_id TEXT NOT NULL REFERENCES phase4_attempts(id),
  state TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(kind, worker_id, attempt_id, dedupe_key)
);
`)
	if err != nil {
		return fmt.Errorf("migrate phase 4 lifecycle: %w", err)
	}
	if err := s.migratePhase4WorkerCommands(ctx); err != nil {
		return err
	}
	if err := s.migratePhase4RetryOperations(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) migratePhase4WorkerCommands(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(phase4_worker_commands)`)
	if err != nil {
		return fmt.Errorf("inspect phase 4 Worker commands: %w", err)
	}
	hasDedupeKey := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("scan phase 4 Worker command columns: %w", err)
		}
		hasDedupeKey = hasDedupeKey || name == "dedupe_key"
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if hasDedupeKey {
		return nil
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE phase4_worker_commands RENAME TO phase4_worker_commands_v1;
CREATE TABLE phase4_worker_commands (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  dedupe_key TEXT NOT NULL,
  worker_id TEXT NOT NULL REFERENCES workers(id),
  attempt_id TEXT NOT NULL REFERENCES phase4_attempts(id),
  state TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(kind, worker_id, attempt_id, dedupe_key)
);
INSERT INTO phase4_worker_commands(id, kind, dedupe_key, worker_id, attempt_id, state, last_error, created_at, updated_at)
SELECT id, kind, 'attempt', worker_id, attempt_id, state, last_error, created_at, updated_at FROM phase4_worker_commands_v1;
DROP TABLE phase4_worker_commands_v1;`); err != nil {
			return fmt.Errorf("migrate phase 4 Worker commands: %w", err)
		}
		return nil
	})
}

func (s *Store) migratePhase4RetryOperations(ctx context.Context) error {
	var tableCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'phase4_retry_operations'`).Scan(&tableCount); err != nil {
		return fmt.Errorf("inspect phase 4 retry operations: %w", err)
	}
	if tableCount == 0 {
		if _, err := s.db.ExecContext(ctx, `CREATE TABLE phase4_retry_operations (
  idempotency_key TEXT PRIMARY KEY,
  source_attempt_id TEXT NOT NULL REFERENCES phase4_attempts(id),
  next_attempt_id TEXT NOT NULL UNIQUE REFERENCES phase4_attempts(id),
  created_at TEXT NOT NULL,
  UNIQUE(source_attempt_id)
)`); err != nil {
			return fmt.Errorf("create phase 4 retry operations: %w", err)
		}
		return nil
	}

	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(phase4_retry_operations)`)
	if err != nil {
		return fmt.Errorf("inspect phase 4 retry operation columns: %w", err)
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan phase 4 retry operation columns: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read phase 4 retry operation columns: %w", err)
	}
	rows.Close()
	if columns["source_attempt_id"] && columns["next_attempt_id"] {
		if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS phase4_retry_operations_source ON phase4_retry_operations(source_attempt_id); CREATE UNIQUE INDEX IF NOT EXISTS phase4_retry_operations_next ON phase4_retry_operations(next_attempt_id)`); err != nil {
			return fmt.Errorf("migrate phase 4 retry operation indexes: %w", err)
		}
		return nil
	}
	if !columns["turn_id"] || !columns["attempt_id"] {
		return errors.New("migrate phase 4 retry operations: unsupported schema")
	}

	if err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE phase4_retry_operations_v2 (
  idempotency_key TEXT PRIMARY KEY,
  source_attempt_id TEXT NOT NULL REFERENCES phase4_attempts(id),
  next_attempt_id TEXT NOT NULL UNIQUE REFERENCES phase4_attempts(id),
  created_at TEXT NOT NULL,
  UNIQUE(source_attempt_id)
)`); err != nil {
			return fmt.Errorf("create phase 4 retry operation migration table: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_retry_operations_v2(idempotency_key, source_attempt_id, next_attempt_id, created_at)
SELECT old.idempotency_key, source.id, next.id, old.created_at
FROM phase4_retry_operations old
JOIN phase4_attempts next ON next.id = old.attempt_id AND next.turn_id = old.turn_id
JOIN phase4_attempts source ON source.turn_id = next.turn_id AND source.number = next.number - 1`); err != nil {
			return fmt.Errorf("backfill phase 4 retry operations: %w", err)
		}
		var oldCount, newCount int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_retry_operations`).Scan(&oldCount); err != nil {
			return fmt.Errorf("count old phase 4 retry operations: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_retry_operations_v2`).Scan(&newCount); err != nil {
			return fmt.Errorf("count migrated phase 4 retry operations: %w", err)
		}
		if oldCount != newCount {
			return fmt.Errorf("backfill phase 4 retry operations: migrated %d of %d rows", newCount, oldCount)
		}
		if _, err := tx.ExecContext(ctx, `DROP TABLE phase4_retry_operations; ALTER TABLE phase4_retry_operations_v2 RENAME TO phase4_retry_operations`); err != nil {
			return fmt.Errorf("install phase 4 retry operation migration: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) CreateWorker(ctx context.Context, conversationID string, spec WorkerSpec, turnSpec TurnSpec) (Worker, Turn, Phase4Attempt, error) {
	key := strings.TrimSpace(spec.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(turnSpec.IdempotencyKey)
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	return s.createWorker(ctx, conversationID, spec, turnSpec, key)
}

func (s *Store) createWorker(ctx context.Context, conversationID string, spec WorkerSpec, turnSpec TurnSpec, idempotencyKey string) (Worker, Turn, Phase4Attempt, error) {
	if strings.TrimSpace(conversationID) == "" || strings.TrimSpace(spec.Intent) == "" || strings.TrimSpace(spec.ProjectID) == "" || strings.TrimSpace(spec.NodeID) == "" || strings.TrimSpace(spec.HarnessInstanceID) == "" {
		return Worker{}, Turn{}, Phase4Attempt{}, errors.New("core: complete Worker binding and intent are required")
	}
	if spec.WorkerRef == "" {
		spec.WorkerRef = newID("wrk")
	}
	if spec.Title == "" {
		spec.Title = spec.Intent
	}
	if strings.TrimSpace(turnSpec.Input) == "" {
		return Worker{}, Turn{}, Phase4Attempt{}, errors.New("core: Turn input is required")
	}
	created, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		worker  Worker
		turn    Turn
		attempt Phase4Attempt
	}, error) {
		if idempotencyKey != "" {
			var encoded string
			if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "worker.create", idempotencyKey).Scan(&encoded); err == nil {
				var stored workerCreationOutcome
				if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
					return struct {
						worker  Worker
						turn    Turn
						attempt Phase4Attempt
					}{}, fmt.Errorf("core: decode durable Worker outcome: %w", err)
				}
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{worker: stored.Worker, turn: stored.Turn, attempt: stored.Attempt}, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
		}
		if spec.ExpectedInventoryJSON != "" {
			result, err := tx.ExecContext(ctx, `UPDATE phase4_nodes SET online = online WHERE node_ref = ? AND online = ? AND draining = ? AND revoked = ?`, spec.NodeID, boolInt(spec.ExpectedNodeOnline), boolInt(spec.ExpectedNodeDraining), boolInt(spec.ExpectedNodeRevoked))
			if err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			} else if affected != 1 {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, ErrSelectedNodeUnavailable
			}
			result, err = tx.ExecContext(ctx, `UPDATE phase4_nodes SET inventory_json = inventory_json WHERE node_ref = ? AND inventory_json = ?`, spec.NodeID, spec.ExpectedInventoryJSON)
			if err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			} else if affected != 1 {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, fmt.Errorf("%w: selected HarnessInstance changed before binding", ErrMissingHarnessInventory)
			}
		}
		if spec.ProjectRevision > 0 {
			result, err := tx.ExecContext(ctx, `UPDATE phase4_projects SET revision = revision WHERE id = ? AND revision = ?`, spec.ProjectID, spec.ProjectRevision)
			if err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
			if affected, err := result.RowsAffected(); err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			} else if affected != 1 {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, ErrProjectRevisionConflict
			}
		}
		now := s.now()
		worker := Worker{ID: newID("wrk"), WorkerRef: spec.WorkerRef, Title: spec.Title, Intent: spec.Intent, ProjectID: spec.ProjectID, NodeID: spec.NodeID, HarnessInstanceID: spec.HarnessInstanceID, PolicySnapshot: spec.PolicySnapshot, ProjectSnapshot: spec.ProjectSnapshot, Workspace: spec.Workspace, Status: WorkerQueued, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workers(id, worker_ref, conversation_id, title, intent, project_id, node_id, harness_instance_id, policy_snapshot, project_snapshot, workspace, status, archived, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`, worker.ID, worker.WorkerRef, conversationID, worker.Title, worker.Intent, worker.ProjectID, worker.NodeID, worker.HarnessInstanceID, worker.PolicySnapshot, worker.ProjectSnapshot, worker.Workspace, worker.Status, timestamp(now), timestamp(now)); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		turn := Turn{ID: newID("trn"), WorkerID: worker.ID, Input: turnSpec.Input, NormalizedIntent: turnSpec.NormalizedIntent, ContextSnapshot: turnSpec.ContextSnapshot, State: TurnStarting, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id, worker_id, input, normalized_intent, context_snapshot, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, turn.ID, turn.WorkerID, turn.Input, turn.NormalizedIntent, turn.ContextSnapshot, turn.State, timestamp(now), timestamp(now)); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		attempt := Phase4Attempt{ID: newID("att"), WorkerID: worker.ID, TurnID: turn.ID, Number: 1, NodeID: worker.NodeID, HarnessInstanceID: worker.HarnessInstanceID, State: AttemptStarting, CorrelationID: turn.ID, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, attempt.ID, attempt.WorkerID, attempt.TurnID, attempt.Number, attempt.NodeID, attempt.HarnessInstanceID, attempt.State, attempt.CorrelationID, timestamp(now), timestamp(now)); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		turn.CurrentAttemptID = attempt.ID
		worker.CurrentTurnID = turn.ID
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET current_attempt_id = ? WHERE id = ?`, attempt.ID, turn.ID); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET current_turn_id = ? WHERE id = ?`, turn.ID, worker.ID); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		workerEvent, err := appendEventTx(ctx, tx, now, EventInput{Kind: "worker.spawned", AggregateType: "worker", AggregateID: worker.WorkerRef, Source: "server", CorrelationID: turn.ID, Payload: worker}, worker)
		if err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, workerEvent.ID, "", "conversation", "worker.spawned:"+worker.ID); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "turn.created", AggregateType: "turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID, Payload: turn}, turn); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "attempt.started", AggregateType: "attempt", AggregateID: attempt.ID, Source: "server", CorrelationID: turn.ID, Payload: attempt}, attempt); err != nil {
			return struct {
				worker  Worker
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if idempotencyKey != "" {
			encoded, err := json.Marshal(workerCreationOutcome{Worker: worker, Turn: turn, Attempt: attempt})
			if err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, fmt.Errorf("core: encode durable Worker outcome: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, "worker.create", idempotencyKey, string(encoded), timestamp(now)); err != nil {
				return struct {
					worker  Worker
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
		}
		return struct {
			worker  Worker
			turn    Turn
			attempt Phase4Attempt
		}{worker: worker, turn: turn, attempt: attempt}, nil
	})
	return created.worker, created.turn, created.attempt, err
}

// SpawnWorker is the product-named alias for the atomic Worker plus first Turn
// operation. It is intentionally not a Task API.
func (s *Store) SpawnWorker(ctx context.Context, conversationID string, spec WorkerSpec, turnSpec TurnSpec) (Worker, Turn, Phase4Attempt, error) {
	return s.CreateWorker(ctx, conversationID, spec, turnSpec)
}

// CreateTurn creates a new user direction for an existing Worker. It never
// changes the Worker's Project, Node, HarnessInstance, or policy snapshot.
func (s *Store) CreateTurn(ctx context.Context, workerID string, spec TurnSpec) (Turn, Phase4Attempt, error) {
	key := strings.TrimSpace(spec.IdempotencyKey)
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	return s.createTurn(ctx, workerID, spec, key)
}

func (s *Store) createTurn(ctx context.Context, workerID string, spec TurnSpec, idempotencyKey string) (Turn, Phase4Attempt, error) {
	if strings.TrimSpace(workerID) == "" || strings.TrimSpace(spec.Input) == "" {
		return Turn{}, Phase4Attempt{}, errors.New("core: Worker and Turn input are required")
	}
	created, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		turn    Turn
		attempt Phase4Attempt
	}, error) {
		operation := "turn.create:" + workerID
		if idempotencyKey != "" {
			var encoded string
			if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, operation, idempotencyKey).Scan(&encoded); err == nil {
				var stored turnCreationOutcome
				if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
					return struct {
						turn    Turn
						attempt Phase4Attempt
					}{}, fmt.Errorf("core: decode durable Turn outcome: %w", err)
				}
				return struct {
					turn    Turn
					attempt Phase4Attempt
				}{turn: stored.Turn, attempt: stored.Attempt}, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return struct {
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
		}
		worker, err := getWorker(ctx, tx, workerID)
		if err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if worker.Status == WorkerClosed {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, ErrInvalidTransition
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE worker_id = ? AND state IN ('queued', 'starting', 'active', 'waiting_approval', 'needs_input')`, workerID).Scan(&active); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if active != 0 {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, ErrInvalidTransition
		}
		now := s.now()
		turn := Turn{ID: newID("trn"), WorkerID: worker.ID, Input: spec.Input, NormalizedIntent: spec.NormalizedIntent, ContextSnapshot: spec.ContextSnapshot, State: TurnStarting, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO turns(id, worker_id, input, normalized_intent, context_snapshot, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, turn.ID, turn.WorkerID, turn.Input, turn.NormalizedIntent, turn.ContextSnapshot, turn.State, timestamp(now), timestamp(now)); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		attempt := Phase4Attempt{ID: newID("att"), WorkerID: worker.ID, TurnID: turn.ID, Number: 1, NodeID: worker.NodeID, HarnessInstanceID: worker.HarnessInstanceID, State: AttemptStarting, CorrelationID: turn.ID, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, attempt.ID, attempt.WorkerID, attempt.TurnID, attempt.Number, attempt.NodeID, attempt.HarnessInstanceID, attempt.State, attempt.CorrelationID, timestamp(now), timestamp(now)); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		turn.CurrentAttemptID = attempt.ID
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET current_attempt_id = ? WHERE id = ?`, attempt.ID, turn.ID); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET current_turn_id = ?, status = ?, updated_at = ? WHERE id = ?`, turn.ID, WorkerQueued, timestamp(now), worker.ID); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "turn.created", AggregateType: "turn", AggregateID: turn.ID, Source: "server", CorrelationID: turn.ID, Payload: turn}, turn); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "attempt.started", AggregateType: "attempt", AggregateID: attempt.ID, Source: "server", CorrelationID: turn.ID, Payload: attempt}, attempt); err != nil {
			return struct {
				turn    Turn
				attempt Phase4Attempt
			}{}, err
		}
		if idempotencyKey != "" {
			encoded, err := json.Marshal(turnCreationOutcome{Turn: turn, Attempt: attempt})
			if err != nil {
				return struct {
					turn    Turn
					attempt Phase4Attempt
				}{}, fmt.Errorf("core: encode durable Turn outcome: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, operation, idempotencyKey, string(encoded), timestamp(now)); err != nil {
				return struct {
					turn    Turn
					attempt Phase4Attempt
				}{}, err
			}
		}
		return struct {
			turn    Turn
			attempt Phase4Attempt
		}{turn: turn, attempt: attempt}, nil
	})
	return created.turn, created.attempt, err
}

func (s *Store) SetPhase4AttemptActive(ctx context.Context, attemptID string) (Phase4Attempt, error) {
	return s.transitionPhase4Attempt(ctx, attemptID, []AttemptState{AttemptStarting}, AttemptActive)
}

// SetPhase4AttemptNeedsInput records a durable safe boundary before the
// Secretary responds. The Attempt remains active and no Turn is created.
func (s *Store) SetPhase4AttemptNeedsInput(ctx context.Context, attemptID string) (Phase4Attempt, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Phase4Attempt, error) {
		attempt, err := getPhase4Attempt(ctx, tx, attemptID)
		if err != nil {
			return Phase4Attempt{}, err
		}
		if attempt.State != AttemptActive {
			return Phase4Attempt{}, ErrInvalidTransition
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ?`, TurnNeedsInput, timestamp(now), attempt.TurnID); err != nil {
			return Phase4Attempt{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerNeedsInput, timestamp(now), attempt.WorkerID); err != nil {
			return Phase4Attempt{}, err
		}
		return attempt, nil
	})
}

// ResumePhase4Attempt returns a needs_input Attempt to active state without
// allocating another Turn or Attempt.
func (s *Store) ResumePhase4Attempt(ctx context.Context, attemptID string) (Phase4Attempt, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Phase4Attempt, error) {
		attempt, err := getPhase4Attempt(ctx, tx, attemptID)
		if err != nil {
			return Phase4Attempt{}, err
		}
		turn, err := getTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return Phase4Attempt{}, err
		}
		if attempt.State != AttemptActive || turn.State != TurnNeedsInput {
			return Phase4Attempt{}, ErrInvalidTransition
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ?`, TurnActive, timestamp(now), turn.ID); err != nil {
			return Phase4Attempt{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerWorking, timestamp(now), attempt.WorkerID); err != nil {
			return Phase4Attempt{}, err
		}
		return attempt, nil
	})
}

func (s *Store) transitionPhase4Attempt(ctx context.Context, attemptID string, from []AttemptState, to AttemptState) (Phase4Attempt, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Phase4Attempt, error) {
		attempt, err := getPhase4Attempt(ctx, tx, attemptID)
		if err != nil {
			return Phase4Attempt{}, err
		}
		allowed := false
		for _, state := range from {
			allowed = allowed || attempt.State == state
		}
		if !allowed {
			return Phase4Attempt{}, ErrInvalidTransition
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE phase4_attempts SET state = ?, updated_at = ? WHERE id = ?`, to, timestamp(now), attempt.ID); err != nil {
			return Phase4Attempt{}, err
		}
		if to == AttemptActive {
			if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ?`, TurnActive, timestamp(now), attempt.TurnID); err != nil {
				return Phase4Attempt{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerWorking, timestamp(now), attempt.WorkerID); err != nil {
				return Phase4Attempt{}, err
			}
			workerRef, err := workerRefForID(ctx, tx, attempt.WorkerID)
			if err != nil {
				return Phase4Attempt{}, err
			}
			event, err := appendEventTx(ctx, tx, now, EventInput{Kind: "worker.started", AggregateType: "worker", AggregateID: workerRef, Source: "server", CorrelationID: attempt.TurnID, Payload: attempt}, attempt)
			if err != nil {
				return Phase4Attempt{}, err
			}
			if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "worker.started:"+attempt.ID); err != nil {
				return Phase4Attempt{}, err
			}
		}
		attempt.State, attempt.UpdatedAt = to, now
		return attempt, nil
	})
}

func (s *Store) finishRetryableAttempt(ctx context.Context, attemptID string, input FinishAttemptInput) (FinishAttemptResult, error) {
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (FinishAttemptResult, error) {
		var outcome AttemptOutcome
		err := scanPhase4Outcome(tx.QueryRowContext(ctx, `SELECT id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at FROM phase4_attempt_outcomes WHERE attempt_id = ?`, attemptID), &outcome)
		if err == nil {
			if outcome.Classification != OutcomeRetryable {
				return FinishAttemptResult{}, ErrInvalidTransition
			}
			attempt, attemptErr := getPhase4Attempt(ctx, tx, attemptID)
			if attemptErr != nil {
				return FinishAttemptResult{}, attemptErr
			}
			var sourceAttemptID, nextID string
			err = tx.QueryRowContext(ctx, `SELECT source_attempt_id, next_attempt_id FROM phase4_retry_operations WHERE idempotency_key = ?`, input.RetryCommandID).Scan(&sourceAttemptID, &nextID)
			if errors.Is(err, sql.ErrNoRows) {
				err = tx.QueryRowContext(ctx, `SELECT source_attempt_id, next_attempt_id FROM phase4_retry_operations WHERE source_attempt_id = ?`, attempt.ID).Scan(&sourceAttemptID, &nextID)
			}
			if err != nil || sourceAttemptID != attempt.ID {
				return FinishAttemptResult{}, ErrInvalidTransition
			}
			next, err := getPhase4Attempt(ctx, tx, nextID)
			if err != nil {
				return FinishAttemptResult{}, err
			}
			result, err := phase4ResultForAttempt(ctx, tx, attemptID)
			return FinishAttemptResult{Outcome: outcome, Result: result, NextAttempt: &next, Duplicate: true}, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return FinishAttemptResult{}, err
		}

		var operationSourceID, operationNextID string
		if err := tx.QueryRowContext(ctx, `SELECT source_attempt_id, next_attempt_id FROM phase4_retry_operations WHERE idempotency_key = ?`, input.RetryCommandID).Scan(&operationSourceID, &operationNextID); err == nil {
			return FinishAttemptResult{}, ErrInvalidTransition
		} else if !errors.Is(err, sql.ErrNoRows) {
			return FinishAttemptResult{}, err
		}
		attempt, err := getPhase4Attempt(ctx, tx, attemptID)
		if err != nil {
			return FinishAttemptResult{}, err
		}
		if attempt.State.Terminal() {
			return FinishAttemptResult{}, ErrInvalidTransition
		}
		turn, err := getTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return FinishAttemptResult{}, err
		}
		if turn.ResultID != "" || turn.CurrentAttemptID != attempt.ID {
			return FinishAttemptResult{}, ErrInvalidTransition
		}
		now := s.now()
		outcome = AttemptOutcome{ID: newID("aou"), AttemptID: attempt.ID, Status: input.Status, Classification: input.Classification, ErrorCode: input.ErrorCode, ErrorMessage: input.ErrorMessage, Diagnostics: input.Diagnostics, CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempt_outcomes(id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, outcome.ID, outcome.AttemptID, outcome.Status, outcome.Classification, outcome.ErrorCode, outcome.ErrorMessage, outcome.Diagnostics, timestamp(now)); err != nil {
			return FinishAttemptResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE phase4_attempts SET state = ?, updated_at = ? WHERE id = ?`, AttemptState(input.Status), timestamp(now), attempt.ID); err != nil {
			return FinishAttemptResult{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: attempt.ID, Source: "server", CorrelationID: attempt.CorrelationID, Payload: input.AttemptOutcomeInput}, input.AttemptOutcomeInput); err != nil {
			return FinishAttemptResult{}, err
		}
		next := Phase4Attempt{ID: newID("att"), WorkerID: attempt.WorkerID, TurnID: attempt.TurnID, Number: attempt.Number + 1, NodeID: attempt.NodeID, HarnessInstanceID: attempt.HarnessInstanceID, State: AttemptStarting, CorrelationID: attempt.CorrelationID, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, next.ID, next.WorkerID, next.TurnID, next.Number, next.NodeID, next.HarnessInstanceID, next.State, next.CorrelationID, timestamp(now), timestamp(now)); err != nil {
			return FinishAttemptResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_retry_operations(idempotency_key, source_attempt_id, next_attempt_id, created_at) VALUES(?, ?, ?, ?)`, input.RetryCommandID, attempt.ID, next.ID, timestamp(now)); err != nil {
			return FinishAttemptResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, current_attempt_id = ?, updated_at = ? WHERE id = ?`, TurnStarting, next.ID, timestamp(now), turn.ID); err != nil {
			return FinishAttemptResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerWorking, timestamp(now), attempt.WorkerID); err != nil {
			return FinishAttemptResult{}, err
		}
		return FinishAttemptResult{Outcome: outcome, NextAttempt: &next}, nil
	})
	return returnValue, err
}

// FinishAttempt is the production terminal path for a Phase 4 Attempt. A
// retryable outcome, its terminal Attempt state, the next Attempt, the durable
// retry operation and Turn/Worker projections commit in one transaction.
func (s *Store) FinishAttempt(ctx context.Context, attemptID string, input FinishAttemptInput) (FinishAttemptResult, error) {
	if err := validateAttemptOutcomeInput(input.AttemptOutcomeInput); err != nil {
		return FinishAttemptResult{}, err
	}
	input.RetryCommandID = strings.TrimSpace(input.RetryCommandID)
	if input.Classification == OutcomeRetryable {
		if input.RetryCommandID == "" {
			return FinishAttemptResult{}, errors.New("core: retryable completion requires durable retry command id")
		}
		return s.finishRetryableAttempt(ctx, attemptID, input)
	}
	if input.RetryCommandID != "" {
		return FinishAttemptResult{}, errors.New("core: retry command id is only valid for retryable completion")
	}
	outcome, result, duplicate, err := s.RecordAttemptOutcome(ctx, attemptID, input.AttemptOutcomeInput)
	return FinishAttemptResult{Outcome: outcome, Result: result, Duplicate: duplicate}, err
}

func validateAttemptOutcomeInput(input AttemptOutcomeInput) error {
	if !input.Status.Valid() || !input.Classification.Valid() {
		return errors.New("core: invalid terminal AttemptOutcome")
	}
	if input.Status == OutcomeInterrupted && input.Classification != OutcomeFinal {
		return errors.New("core: interrupted outcome must be final")
	}
	if input.Status == OutcomeSucceeded && input.Classification != OutcomeFinal {
		return errors.New("core: succeeded outcome must be final")
	}
	if input.Status == OutcomeCanceled && input.Classification == OutcomeRetryable {
		return errors.New("core: canceled outcome cannot be retryable")
	}
	if input.Classification == OutcomeFinal && strings.TrimSpace(input.Summary) == "" {
		return errors.New("core: final outcome summary is required")
	}
	return nil
}

// RecordAttemptOutcome remains available for terminal final outcomes and
// legacy callers. It deliberately rejects retryable outcomes so production
// code cannot leave a Turn without an atomic next Attempt.
func (s *Store) RecordAttemptOutcome(ctx context.Context, attemptID string, input AttemptOutcomeInput) (AttemptOutcome, *Phase4Result, bool, error) {
	if err := validateAttemptOutcomeInput(input); err != nil {
		return AttemptOutcome{}, nil, false, err
	}
	if input.Classification == OutcomeRetryable {
		return AttemptOutcome{}, nil, false, errors.New("core: retryable outcome requires FinishAttempt with durable retry command id")
	}
	if input.Classification == OutcomeFinal && input.Status != OutcomeSucceeded {
		input.FailureCode = strings.TrimSpace(input.FailureCode)
		if input.FailureCode == "" {
			input.FailureCode = strings.TrimSpace(input.ErrorCode)
		}
		if input.FailureCode == "" {
			input.FailureCode = string(input.Status)
		}
	}
	var committedEntry ConversationEntry
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		outcome AttemptOutcome
		result  *Phase4Result
		dup     bool
	}, error) {
		var outcome AttemptOutcome
		err := scanPhase4Outcome(tx.QueryRowContext(ctx, `SELECT id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at FROM phase4_attempt_outcomes WHERE attempt_id = ?`, attemptID), &outcome)
		if err == nil {
			result, resultErr := phase4ResultForAttempt(ctx, tx, attemptID)
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{outcome: outcome, result: result, dup: true}, resultErr
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		attempt, err := getPhase4Attempt(ctx, tx, attemptID)
		if err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if attempt.State.Terminal() {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, ErrInvalidTransition
		}
		now := s.now()
		outcome = AttemptOutcome{ID: newID("aou"), AttemptID: attempt.ID, Status: input.Status, Classification: input.Classification, ErrorCode: input.ErrorCode, ErrorMessage: input.ErrorMessage, Diagnostics: input.Diagnostics, CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempt_outcomes(id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, outcome.ID, outcome.AttemptID, outcome.Status, outcome.Classification, outcome.ErrorCode, outcome.ErrorMessage, outcome.Diagnostics, timestamp(now)); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE phase4_attempts SET state = ?, updated_at = ? WHERE id = ?`, AttemptState(input.Status), timestamp(now), attempt.ID); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: attempt.ID, Source: "server", CorrelationID: attempt.CorrelationID, Payload: outcome}, outcome); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		turn, err := getTurn(ctx, tx, attempt.TurnID)
		if err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if turn.ResultID != "" {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, ErrInvalidTransition
		}
		turnState := TurnState(input.Status)
		resultStatus := ResultStatus(input.Status)
		result := &Phase4Result{ID: newID("res"), WorkerID: attempt.WorkerID, TurnID: attempt.TurnID, AttemptID: attempt.ID, Status: resultStatus, Summary: input.Summary, FailureCode: input.FailureCode, ArtifactRefs: input.ArtifactRefs, CorrelationID: attempt.CorrelationID, CreatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_results(id, worker_id, turn_id, attempt_id, status, summary, failure_code, artifact_refs, correlation_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, result.ID, result.WorkerID, result.TurnID, result.AttemptID, result.Status, result.Summary, result.FailureCode, result.ArtifactRefs, result.CorrelationID, timestamp(now)); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "result.accepted", AggregateType: "result", AggregateID: result.ID, Source: "server", CorrelationID: result.CorrelationID, CausationID: outcome.ID, Payload: result}, result); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, result_id = ?, updated_at = ? WHERE id = ?`, turnState, result.ID, timestamp(now), turn.ID); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		workerStatus := WorkerIdle
		if input.Status == OutcomeInterrupted {
			workerStatus = WorkerOffline
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, last_result_summary = ?, updated_at = ? WHERE id = ?`, workerStatus, result.Summary, timestamp(now), attempt.WorkerID); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		conversationID, err := conversationForWorker(ctx, tx, attempt.WorkerID)
		if err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		committedEntry, err = appendEntry(ctx, tx, now, conversationID, EntryWorkerResult, result.Summary)
		if err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		return struct {
			outcome AttemptOutcome
			result  *Phase4Result
			dup     bool
		}{outcome: outcome, result: result}, nil
	})
	if err == nil && !returnValue.dup && committedEntry.ID != "" {
		s.notifyEntry(committedEntry)
	}
	return returnValue.outcome, returnValue.result, returnValue.dup, err
}

// RetryAttempt is retained as a read-only compatibility lookup. It never
// creates a new Attempt; production execution must call FinishAttempt with a
// durable RetryCommandID.
func (s *Store) RetryAttempt(ctx context.Context, turnID string, idempotencyKeys ...string) (Phase4Attempt, error) {
	if len(idempotencyKeys) > 1 {
		return Phase4Attempt{}, errors.New("core: at most one retry idempotency key is allowed")
	}
	key := ""
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
		if key == "" {
			return Phase4Attempt{}, errors.New("core: retry idempotency key is required")
		}
	}
	return s.retryAttempt(ctx, turnID, key)
}

// RetryAttemptWithKey is a compatibility lookup for retries already created
// by FinishAttempt. New production retries must use FinishAttempt, which
// durably records the command and creates the next Attempt in one transaction.
func (s *Store) RetryAttemptWithKey(ctx context.Context, turnID, idempotencyKey string) (Phase4Attempt, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return Phase4Attempt{}, errors.New("core: retry idempotency key is required")
	}
	return s.retryAttempt(ctx, turnID, idempotencyKey)
}

func (s *Store) retryAttempt(ctx context.Context, turnID, idempotencyKey string) (Phase4Attempt, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Phase4Attempt, error) {
		if idempotencyKey != "" {
			var storedSourceID, storedNextID, storedTurnID string
			err := tx.QueryRowContext(ctx, `SELECT operation.source_attempt_id, operation.next_attempt_id, source.turn_id
FROM phase4_retry_operations operation
JOIN phase4_attempts source ON source.id = operation.source_attempt_id
WHERE operation.idempotency_key = ?`, idempotencyKey).Scan(&storedSourceID, &storedNextID, &storedTurnID)
			if err == nil {
				if storedTurnID != turnID {
					return Phase4Attempt{}, ErrInvalidTransition
				}
				return getPhase4Attempt(ctx, tx, storedNextID)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return Phase4Attempt{}, err
			}
		}

		turn, err := getTurn(ctx, tx, turnID)
		if err != nil {
			return Phase4Attempt{}, err
		}
		if idempotencyKey == "" && (turn.State.Terminal() || turn.ResultID != "") {
			var storedAttemptID string
			err := tx.QueryRowContext(ctx, `SELECT operation.next_attempt_id
FROM phase4_retry_operations operation
JOIN phase4_attempts next ON next.id = operation.next_attempt_id
WHERE next.turn_id = ? ORDER BY operation.created_at DESC, operation.idempotency_key DESC LIMIT 1`, turnID).Scan(&storedAttemptID)
			if err == nil {
				return getPhase4Attempt(ctx, tx, storedAttemptID)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return Phase4Attempt{}, err
			}
		}
		if turn.State.Terminal() || turn.ResultID != "" {
			return Phase4Attempt{}, ErrInvalidTransition
		}
		var latest Phase4Attempt
		if err := scanPhase4Attempt(tx.QueryRowContext(ctx, `SELECT id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at FROM phase4_attempts WHERE turn_id = ? ORDER BY number DESC LIMIT 1`, turnID), &latest); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Phase4Attempt{}, ErrNotFound
			}
			return Phase4Attempt{}, err
		}

		if latest.State == AttemptStarting || latest.State == AttemptActive {
			if idempotencyKey != "" {
				return Phase4Attempt{}, ErrInvalidTransition
			}
			var operationAttemptID string
			err := tx.QueryRowContext(ctx, `SELECT next_attempt_id FROM phase4_retry_operations WHERE next_attempt_id = ?`, latest.ID).Scan(&operationAttemptID)
			if err == nil {
				return latest, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return Phase4Attempt{}, err
			}
			return Phase4Attempt{}, ErrInvalidTransition
		}

		var classification OutcomeClassification
		if err := tx.QueryRowContext(ctx, `SELECT classification FROM phase4_attempt_outcomes WHERE attempt_id = ?`, latest.ID).Scan(&classification); err != nil {
			return Phase4Attempt{}, ErrInvalidTransition
		}
		if classification != OutcomeRetryable || !latest.State.Terminal() {
			return Phase4Attempt{}, ErrInvalidTransition
		}
		// This compatibility method is intentionally read-only. It must not be a
		// production execution path that can create a retry without an atomic
		// outcome and next Attempt.
		return Phase4Attempt{}, ErrInvalidTransition
	})
}

func (s *Store) InterruptPhase4Attempt(ctx context.Context, attemptID, errorCode, diagnostics string) (AttemptOutcome, *Phase4Result, bool, error) {
	errorCode = strings.TrimSpace(errorCode)
	if errorCode == "" {
		errorCode = "interrupted"
	}
	summary := strings.TrimSpace(diagnostics)
	if summary == "" {
		summary = "Attempt interrupted"
	}
	return s.RecordAttemptOutcome(ctx, attemptID, AttemptOutcomeInput{Status: OutcomeInterrupted, Classification: OutcomeFinal, ErrorCode: errorCode, Diagnostics: diagnostics, FailureCode: errorCode, Summary: summary})
}

// ClaimWorkerCommand creates one durable command identity for an immutable
// Worker Attempt. Pending means another caller owns handoff; failed preserves
// the error for an explicit recovery decision without changing command ID.
func (s *Store) ClaimWorkerCommand(ctx context.Context, kind, dedupeKey, workerID, attemptID string) (WorkerCommand, bool, error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(dedupeKey) == "" || strings.TrimSpace(workerID) == "" || strings.TrimSpace(attemptID) == "" {
		return WorkerCommand{}, false, errors.New("core: Worker command binding is required")
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	duplicate := false
	command, err := withTx(s, ctx, func(tx *sql.Tx) (WorkerCommand, error) {
		var command WorkerCommand
		query := `SELECT id, kind, dedupe_key, worker_id, attempt_id, state, last_error, created_at, updated_at FROM phase4_worker_commands WHERE kind = ? AND worker_id = ? AND attempt_id = ? AND dedupe_key = ?`
		err := scanWorkerCommand(tx.QueryRowContext(ctx, query, kind, workerID, attemptID, dedupeKey), &command)
		if err == nil {
			duplicate = true
			return command, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return WorkerCommand{}, err
		}
		now := s.now()
		command = WorkerCommand{ID: newID("cmd"), Kind: kind, DedupeKey: dedupeKey, WorkerID: workerID, AttemptID: attemptID, State: WorkerCommandPending, CreatedAt: now, UpdatedAt: now}
		result, err := tx.ExecContext(ctx, `INSERT INTO phase4_worker_commands(id, kind, dedupe_key, worker_id, attempt_id, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(kind, worker_id, attempt_id, dedupe_key) DO NOTHING`, command.ID, command.Kind, command.DedupeKey, command.WorkerID, command.AttemptID, command.State, timestamp(now), timestamp(now))
		if err != nil {
			return WorkerCommand{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return WorkerCommand{}, err
		}
		if affected == 1 {
			return command, nil
		}
		duplicate = true
		if err := scanWorkerCommand(tx.QueryRowContext(ctx, query, kind, workerID, attemptID, dedupeKey), &command); err != nil {
			return WorkerCommand{}, err
		}
		return command, nil
	})
	return command, duplicate, err
}

func (s *Store) MarkWorkerCommandDelivered(ctx context.Context, commandID string) (WorkerCommand, error) {
	return s.updateWorkerCommand(ctx, commandID, WorkerCommandDelivered, "")
}

func (s *Store) MarkWorkerCommandFailed(ctx context.Context, commandID, message string) (WorkerCommand, error) {
	return s.updateWorkerCommand(ctx, commandID, WorkerCommandFailed, strings.TrimSpace(message))
}

func (s *Store) updateWorkerCommand(ctx context.Context, commandID string, state WorkerCommandState, message string) (WorkerCommand, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (WorkerCommand, error) {
		var command WorkerCommand
		if err := scanWorkerCommand(tx.QueryRowContext(ctx, `SELECT id, kind, dedupe_key, worker_id, attempt_id, state, last_error, created_at, updated_at FROM phase4_worker_commands WHERE id = ?`, commandID), &command); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return WorkerCommand{}, ErrNotFound
			}
			return WorkerCommand{}, err
		}
		if command.State == WorkerCommandDelivered {
			return command, nil
		}
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE phase4_worker_commands SET state = ?, last_error = ?, updated_at = ? WHERE id = ?`, state, message, timestamp(now), command.ID); err != nil {
			return WorkerCommand{}, err
		}
		command.State, command.LastError, command.UpdatedAt = state, message, now
		return command, nil
	})
}

func scanWorkerCommand(row interface{ Scan(...any) error }, command *WorkerCommand) error {
	return row.Scan(&command.ID, &command.Kind, &command.DedupeKey, &command.WorkerID, &command.AttemptID, &command.State, &command.LastError, newTimestampScanner(&command.CreatedAt), newTimestampScanner(&command.UpdatedAt))
}

func (s *Store) CloseWorker(ctx context.Context, workerID string) (Worker, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Worker, error) {
		worker, err := getWorker(ctx, tx, workerID)
		if err != nil {
			return Worker{}, err
		}
		if worker.Status == WorkerClosed {
			return worker, nil
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE worker_id = ? AND state IN ('queued', 'starting', 'active', 'waiting_approval', 'needs_input')`, worker.ID).Scan(&count); err != nil {
			return Worker{}, err
		}
		if count > 0 {
			return Worker{}, ErrInvalidTransition
		}
		now := s.now()
		worker.Status, worker.Archived, worker.ClosedAt, worker.UpdatedAt = WorkerClosed, true, &now, now
		_, err = tx.ExecContext(ctx, `UPDATE workers SET status = ?, archived = 1, closed_at = ?, updated_at = ? WHERE id = ?`, worker.Status, timestamp(now), timestamp(now), worker.ID)
		return worker, err
	})
}

func (s *Store) Worker(ctx context.Context, id string) (Worker, error) {
	return getWorker(ctx, s.db, id)
}

func (s *Store) WorkersForConversation(ctx context.Context, conversationID string) ([]Worker, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM workers WHERE conversation_id = ? ORDER BY created_at, id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workers := []Worker{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		worker, err := s.Worker(ctx, id)
		if err != nil {
			return nil, err
		}
		workers = append(workers, worker)
	}
	return workers, rows.Err()
}

func (s *Store) WorkerDetailsForConversation(ctx context.Context, conversationID, workerRef string) (WorkerDetails, error) {
	var workerID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM workers WHERE conversation_id = ? AND worker_ref = ?`, conversationID, workerRef).Scan(&workerID)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerDetails{}, ErrNotFound
	}
	if err != nil {
		return WorkerDetails{}, err
	}
	worker, err := s.Worker(ctx, workerID)
	if err != nil {
		return WorkerDetails{}, err
	}
	details := WorkerDetails{Worker: worker, Turns: []Turn{}, Attempts: []Phase4Attempt{}, Outcomes: []AttemptOutcome{}, Results: []Phase4Result{}}
	turnRows, err := s.db.QueryContext(ctx, `SELECT id FROM turns WHERE worker_id = ? ORDER BY created_at, id`, worker.ID)
	if err != nil {
		return WorkerDetails{}, err
	}
	for turnRows.Next() {
		var id string
		if err := turnRows.Scan(&id); err != nil {
			turnRows.Close()
			return WorkerDetails{}, err
		}
		turn, err := s.Turn(ctx, id)
		if err != nil {
			turnRows.Close()
			return WorkerDetails{}, err
		}
		details.Turns = append(details.Turns, turn)
	}
	if err := turnRows.Err(); err != nil {
		turnRows.Close()
		return WorkerDetails{}, err
	}
	turnRows.Close()
	attemptRows, err := s.db.QueryContext(ctx, `SELECT id FROM phase4_attempts WHERE worker_id = ? ORDER BY created_at, id`, worker.ID)
	if err != nil {
		return WorkerDetails{}, err
	}
	for attemptRows.Next() {
		var id string
		if err := attemptRows.Scan(&id); err != nil {
			attemptRows.Close()
			return WorkerDetails{}, err
		}
		attempt, err := s.Phase4Attempt(ctx, id)
		if err != nil {
			attemptRows.Close()
			return WorkerDetails{}, err
		}
		details.Attempts = append(details.Attempts, attempt)
		if outcome, err := s.AttemptOutcome(ctx, attempt.ID); err == nil {
			details.Outcomes = append(details.Outcomes, outcome)
		} else if !errors.Is(err, ErrNotFound) {
			attemptRows.Close()
			return WorkerDetails{}, err
		}
	}
	if err := attemptRows.Err(); err != nil {
		attemptRows.Close()
		return WorkerDetails{}, err
	}
	attemptRows.Close()
	for _, turn := range details.Turns {
		if result, err := s.Phase4Result(ctx, turn.ID); err == nil {
			details.Results = append(details.Results, result)
		} else if !errors.Is(err, ErrNotFound) {
			return WorkerDetails{}, err
		}
	}
	return details, nil
}
func (s *Store) Turn(ctx context.Context, id string) (Turn, error) { return getTurn(ctx, s.db, id) }
func (s *Store) Phase4Attempt(ctx context.Context, id string) (Phase4Attempt, error) {
	return getPhase4Attempt(ctx, s.db, id)
}
func (s *Store) AttemptOutcome(ctx context.Context, attemptID string) (AttemptOutcome, error) {
	var outcome AttemptOutcome
	err := scanPhase4Outcome(s.db.QueryRowContext(ctx, `SELECT id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at FROM phase4_attempt_outcomes WHERE attempt_id = ?`, attemptID), &outcome)
	if errors.Is(err, sql.ErrNoRows) {
		return AttemptOutcome{}, ErrNotFound
	}
	return outcome, err
}
func (s *Store) Phase4Result(ctx context.Context, turnID string) (Phase4Result, error) {
	var result Phase4Result
	err := s.db.QueryRowContext(ctx, `SELECT id, worker_id, turn_id, attempt_id, status, summary, failure_code, artifact_refs, correlation_id, created_at FROM phase4_results WHERE turn_id = ?`, turnID).Scan(&result.ID, &result.WorkerID, &result.TurnID, &result.AttemptID, &result.Status, &result.Summary, &result.FailureCode, &result.ArtifactRefs, &result.CorrelationID, newTimestampScanner(&result.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Phase4Result{}, ErrNotFound
	}
	return result, err
}

func getWorker(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Worker, error) {
	var worker Worker
	var closed sql.NullString
	var archived int
	err := q.QueryRowContext(ctx, `SELECT id, worker_ref, title, intent, project_id, node_id, harness_instance_id, policy_snapshot, project_snapshot, workspace, status, COALESCE(current_turn_id, ''), last_result_summary, created_at, updated_at, closed_at, archived FROM workers WHERE id = ?`, id).Scan(&worker.ID, &worker.WorkerRef, &worker.Title, &worker.Intent, &worker.ProjectID, &worker.NodeID, &worker.HarnessInstanceID, &worker.PolicySnapshot, &worker.ProjectSnapshot, &worker.Workspace, &worker.Status, &worker.CurrentTurnID, &worker.LastResultSummary, newTimestampScanner(&worker.CreatedAt), newTimestampScanner(&worker.UpdatedAt), &closed, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return Worker{}, ErrNotFound
	}
	if err != nil {
		return Worker{}, err
	}
	worker.Archived = archived != 0
	if closed.Valid {
		t, parseErr := parseTimestamp(closed.String)
		if parseErr != nil {
			return Worker{}, parseErr
		}
		worker.ClosedAt = &t
	}
	return worker, nil
}

func getTurn(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Turn, error) {
	var turn Turn
	err := q.QueryRowContext(ctx, `SELECT id, worker_id, input, normalized_intent, context_snapshot, state, COALESCE(current_attempt_id, ''), COALESCE(result_id, ''), created_at, updated_at FROM turns WHERE id = ?`, id).Scan(&turn.ID, &turn.WorkerID, &turn.Input, &turn.NormalizedIntent, &turn.ContextSnapshot, &turn.State, &turn.CurrentAttemptID, &turn.ResultID, newTimestampScanner(&turn.CreatedAt), newTimestampScanner(&turn.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Turn{}, ErrNotFound
	}
	return turn, err
}

func getPhase4Attempt(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Phase4Attempt, error) {
	var attempt Phase4Attempt
	err := scanPhase4Attempt(q.QueryRowContext(ctx, `SELECT id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at FROM phase4_attempts WHERE id = ?`, id), &attempt)
	if errors.Is(err, sql.ErrNoRows) {
		return Phase4Attempt{}, ErrNotFound
	}
	return attempt, err
}

func scanPhase4Attempt(row interface{ Scan(...any) error }, attempt *Phase4Attempt) error {
	return row.Scan(&attempt.ID, &attempt.WorkerID, &attempt.TurnID, &attempt.Number, &attempt.NodeID, &attempt.HarnessInstanceID, &attempt.State, &attempt.CorrelationID, newTimestampScanner(&attempt.CreatedAt), newTimestampScanner(&attempt.UpdatedAt))
}
func scanPhase4Outcome(row interface{ Scan(...any) error }, outcome *AttemptOutcome) error {
	return row.Scan(&outcome.ID, &outcome.AttemptID, &outcome.Status, &outcome.Classification, &outcome.ErrorCode, &outcome.ErrorMessage, &outcome.Diagnostics, newTimestampScanner(&outcome.CreatedAt))
}
func phase4ResultForAttempt(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, attemptID string) (*Phase4Result, error) {
	var result Phase4Result
	err := q.QueryRowContext(ctx, `SELECT id, worker_id, turn_id, attempt_id, status, summary, failure_code, artifact_refs, correlation_id, created_at FROM phase4_results WHERE attempt_id = ?`, attemptID).Scan(&result.ID, &result.WorkerID, &result.TurnID, &result.AttemptID, &result.Status, &result.Summary, &result.FailureCode, &result.ArtifactRefs, &result.CorrelationID, newTimestampScanner(&result.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &result, err
}
func conversationForWorker(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workerID string) (string, error) {
	var conversationID string
	err := q.QueryRowContext(ctx, `SELECT conversation_id FROM workers WHERE id = ?`, workerID).Scan(&conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return conversationID, err
}
func parseTimestamp(text string) (t time.Time, err error) { return time.Parse(time.RFC3339Nano, text) }

// RecordNodeAttemptOutcome is the authenticated Node boundary for terminal
// Attempt events. It verifies the immutable server binding before delegating to
// the idempotent lifecycle transaction. Native runtime identifiers never cross
// this boundary.
func (s *Store) RecordNodeAttemptOutcome(ctx context.Context, envelope AttemptOutcomeEnvelope) (AttemptOutcome, *Phase4Result, bool, error) {
	if err := envelope.Validate(); err != nil {
		return AttemptOutcome{}, nil, false, err
	}
	if strings.TrimSpace(string(envelope.Node)) == "" || strings.TrimSpace(string(envelope.HarnessInstanceID)) == "" || strings.TrimSpace(envelope.WorkerRef) == "" || strings.TrimSpace(envelope.TurnID) == "" {
		return AttemptOutcome{}, nil, false, errors.New("core: Node terminal event binding is required")
	}
	attempt, err := s.Phase4Attempt(ctx, envelope.AttemptID)
	if err != nil {
		return AttemptOutcome{}, nil, false, err
	}
	worker, err := s.Worker(ctx, attempt.WorkerID)
	if err != nil {
		return AttemptOutcome{}, nil, false, err
	}
	if attempt.NodeID != string(envelope.Node) || attempt.HarnessInstanceID != string(envelope.HarnessInstanceID) || worker.WorkerRef != envelope.WorkerRef || attempt.TurnID != envelope.TurnID {
		return AttemptOutcome{}, nil, false, errors.New("core: Node terminal event does not match immutable Worker binding")
	}
	artifactRefs := ""
	if len(envelope.ArtifactRefs) > 0 {
		encoded, encodeErr := json.Marshal(envelope.ArtifactRefs)
		if encodeErr != nil {
			return AttemptOutcome{}, nil, false, encodeErr
		}
		artifactRefs = string(encoded)
	}
	returnValue, result, duplicate, err := s.RecordAttemptOutcome(ctx, envelope.AttemptID, AttemptOutcomeInput{
		Status: envelope.Status, Classification: envelope.Classification, ErrorCode: envelope.ErrorCode, ErrorMessage: envelope.ErrorMessage,
		Diagnostics: envelope.Diagnostics, Summary: envelope.Summary, FailureCode: envelope.FailureCode, ArtifactRefs: artifactRefs,
	})
	return returnValue, result, duplicate, err
}

// RecordNodeActivity validates a normalized activity against the immutable
// server Attempt binding and the currently observed capability inventory.
func (s *Store) RecordNodeActivity(ctx context.Context, instance HarnessInstance, activity Activity) (Event, error) {
	if err := activity.ValidateFor(instance); err != nil {
		return Event{}, err
	}
	return s.recordNodeActivity(ctx, activity)
}

// RecordNodeActivityReplay validates a buffered Activity using its normalized
// payload and immutable Attempt binding. Current inventory is deliberately not
// consulted because it may have changed after the event was observed.
func (s *Store) RecordNodeActivityReplay(ctx context.Context, activity Activity) (Event, error) {
	if err := activity.ValidatePayload(); err != nil {
		return Event{}, err
	}
	return s.recordNodeActivity(ctx, activity)
}

func (s *Store) recordNodeActivity(ctx context.Context, activity Activity) (Event, error) {
	attempt, err := s.Phase4Attempt(ctx, activity.Metadata.AttemptID)
	if err != nil {
		return Event{}, err
	}
	worker, err := s.Worker(ctx, attempt.WorkerID)
	if err != nil {
		return Event{}, err
	}
	if attempt.NodeID != string(activity.Metadata.Node) || attempt.HarnessInstanceID != string(activity.Metadata.HarnessInstanceID) || worker.WorkerRef != activity.Metadata.WorkerRef || attempt.TurnID != activity.Metadata.TurnID {
		return Event{}, errors.New("core: Node activity does not match immutable Worker binding")
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Event, error) {
		var encoded string
		if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "node.activity", activity.Metadata.EventID).Scan(&encoded); err == nil {
			var event Event
			if err := json.Unmarshal([]byte(encoded), &event); err != nil {
				return Event{}, err
			}
			return event, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Event{}, err
		}
		event, err := appendEventTx(ctx, tx, s.now(), EventInput{Kind: "attempt.activity", AggregateType: "attempt", AggregateID: attempt.ID, Source: "node", CorrelationID: activity.Metadata.CorrelationID, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: activity}, activity)
		if err != nil {
			return Event{}, err
		}
		encodedEvent, err := json.Marshal(event)
		if err != nil {
			return Event{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, "node.activity", activity.Metadata.EventID, string(encodedEvent), timestamp(s.now())); err != nil {
			return Event{}, err
		}
		return event, nil
	})
}
