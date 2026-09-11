package core

import (
	"context"
	"database/sql"
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
CREATE TABLE IF NOT EXISTS phase4_retry_operations (
  idempotency_key TEXT PRIMARY KEY,
  turn_id TEXT NOT NULL REFERENCES turns(id),
  attempt_id TEXT NOT NULL UNIQUE REFERENCES phase4_attempts(id),
  created_at TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("migrate phase 4 lifecycle: %w", err)
	}
	return nil
}

func (s *Store) CreateWorker(ctx context.Context, conversationID string, spec WorkerSpec, turnSpec TurnSpec) (Worker, Turn, Phase4Attempt, error) {
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
		now := s.now()
		worker := Worker{ID: newID("wrk"), WorkerRef: spec.WorkerRef, Title: spec.Title, Intent: spec.Intent, ProjectID: spec.ProjectID, NodeID: spec.NodeID, HarnessInstanceID: spec.HarnessInstanceID, PolicySnapshot: spec.PolicySnapshot, Status: WorkerQueued, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workers(id, worker_ref, conversation_id, title, intent, project_id, node_id, harness_instance_id, policy_snapshot, status, archived, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`, worker.ID, worker.WorkerRef, conversationID, worker.Title, worker.Intent, worker.ProjectID, worker.NodeID, worker.HarnessInstanceID, worker.PolicySnapshot, worker.Status, timestamp(now), timestamp(now)); err != nil {
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
	if strings.TrimSpace(workerID) == "" || strings.TrimSpace(spec.Input) == "" {
		return Turn{}, Phase4Attempt{}, errors.New("core: Worker and Turn input are required")
	}
	created, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		turn    Turn
		attempt Phase4Attempt
	}, error) {
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
			var storedTurnID, nextID string
			if err := tx.QueryRowContext(ctx, `SELECT turn_id, attempt_id FROM phase4_retry_operations WHERE idempotency_key = ?`, input.RetryCommandID).Scan(&storedTurnID, &nextID); err != nil {
				return FinishAttemptResult{}, ErrInvalidTransition
			}
			if storedTurnID != attempt.TurnID {
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

		var operationTurnID, operationAttemptID string
		if err := tx.QueryRowContext(ctx, `SELECT turn_id, attempt_id FROM phase4_retry_operations WHERE idempotency_key = ?`, input.RetryCommandID).Scan(&operationTurnID, &operationAttemptID); err == nil {
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
		next := Phase4Attempt{ID: newID("att"), WorkerID: attempt.WorkerID, TurnID: attempt.TurnID, Number: attempt.Number + 1, NodeID: attempt.NodeID, HarnessInstanceID: attempt.HarnessInstanceID, State: AttemptStarting, CorrelationID: attempt.CorrelationID, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, next.ID, next.WorkerID, next.TurnID, next.Number, next.NodeID, next.HarnessInstanceID, next.State, next.CorrelationID, timestamp(now), timestamp(now)); err != nil {
			return FinishAttemptResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_retry_operations(idempotency_key, turn_id, attempt_id, created_at) VALUES(?, ?, ?, ?)`, input.RetryCommandID, turn.ID, next.ID, timestamp(now)); err != nil {
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
		if input.Classification == OutcomeRetryable {
			if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, current_attempt_id = NULL, updated_at = ? WHERE id = ?`, TurnActive, timestamp(now), attempt.TurnID); err != nil {
				return struct {
					outcome AttemptOutcome
					result  *Phase4Result
					dup     bool
				}{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerWorking, timestamp(now), attempt.WorkerID); err != nil {
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
			}{outcome: outcome}, nil
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
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, result_id = ?, updated_at = ? WHERE id = ?`, turnState, result.ID, timestamp(now), turn.ID); err != nil {
			return struct {
				outcome AttemptOutcome
				result  *Phase4Result
				dup     bool
			}{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, last_result_summary = ?, updated_at = ? WHERE id = ?`, WorkerIdle, result.Summary, timestamp(now), attempt.WorkerID); err != nil {
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
			var storedTurnID, storedAttemptID string
			err := tx.QueryRowContext(ctx, `SELECT turn_id, attempt_id FROM phase4_retry_operations WHERE idempotency_key = ?`, idempotencyKey).Scan(&storedTurnID, &storedAttemptID)
			if err == nil {
				if storedTurnID != turnID {
					return Phase4Attempt{}, ErrInvalidTransition
				}
				return getPhase4Attempt(ctx, tx, storedAttemptID)
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
			err := tx.QueryRowContext(ctx, `SELECT attempt_id FROM phase4_retry_operations WHERE turn_id = ? ORDER BY created_at DESC, idempotency_key DESC LIMIT 1`, turnID).Scan(&storedAttemptID)
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
			err := tx.QueryRowContext(ctx, `SELECT attempt_id FROM phase4_retry_operations WHERE attempt_id = ?`, latest.ID).Scan(&operationAttemptID)
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
	err := q.QueryRowContext(ctx, `SELECT id, worker_ref, title, intent, project_id, node_id, harness_instance_id, policy_snapshot, status, COALESCE(current_turn_id, ''), last_result_summary, created_at, updated_at, closed_at, archived FROM workers WHERE id = ?`, id).Scan(&worker.ID, &worker.WorkerRef, &worker.Title, &worker.Intent, &worker.ProjectID, &worker.NodeID, &worker.HarnessInstanceID, &worker.PolicySnapshot, &worker.Status, &worker.CurrentTurnID, &worker.LastResultSummary, newTimestampScanner(&worker.CreatedAt), newTimestampScanner(&worker.UpdatedAt), &closed, &archived)
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
