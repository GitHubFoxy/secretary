package core

import (
	"context"
	"database/sql"
	"errors"
)

type followUpAttempt struct {
	attempt Attempt
	entry   ConversationEntry
}

// CreateFollowUpAttempt records queued Worker input before it is sent to the
// runtime. A follow-up is allowed only after the previous Attempt is terminal.
func (s *Store) CreateFollowUpAttempt(ctx context.Context, taskID, input string) (Attempt, error) {
	if s.legacyTasksReadOnly(ctx) {
		return Attempt{}, ErrLegacyTaskReadOnly
	}
	created, err := withTx(s, ctx, func(tx *sql.Tx) (followUpAttempt, error) {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return followUpAttempt{}, err
		}
		if task.State != TaskOpen {
			return followUpAttempt{}, ErrInvalidTransition
		}
		var bindingID, conversationID string
		if err := tx.QueryRowContext(ctx, `SELECT w.id, t.conversation_id FROM worker_bindings w JOIN tasks t ON t.id = w.task_id WHERE w.task_id = ? AND w.archived = 0`, taskID).Scan(&bindingID, &conversationID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return followUpAttempt{}, ErrNotFound
			}
			return followUpAttempt{}, err
		}
		var previousState AttemptState
		var number int
		err = tx.QueryRowContext(ctx, `SELECT number, state FROM attempts WHERE worker_binding_id = ? ORDER BY number DESC LIMIT 1`, bindingID).Scan(&number, &previousState)
		if err != nil {
			return followUpAttempt{}, err
		}
		if !previousState.Terminal() {
			return followUpAttempt{}, ErrInvalidTransition
		}
		now := s.now()
		attempt := Attempt{ID: newID("att"), WorkerBindingID: bindingID, Number: number + 1, State: AttemptStarting, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO attempts(id, worker_binding_id, number, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`, attempt.ID, attempt.WorkerBindingID, attempt.Number, attempt.State, timestamp(now), timestamp(now)); err != nil {
			return followUpAttempt{}, err
		}
		entry, err := appendEntry(ctx, tx, now, conversationID, EntryWorkerInput, input)
		if err != nil {
			return followUpAttempt{}, err
		}
		return followUpAttempt{attempt: attempt, entry: entry}, nil
	})
	if err == nil {
		s.notifyEntry(created.entry)
	}
	return created.attempt, err
}

func (s *Store) TaskForWorker(ctx context.Context, workerRef string) (Task, error) {
	var task Task
	err := s.db.QueryRowContext(ctx, `SELECT t.id, t.conversation_id, t.text, t.state, t.parent_task_id, t.parent_attempt_id, t.child_index, t.created_at, t.updated_at FROM tasks t JOIN worker_bindings w ON w.task_id = t.id WHERE w.worker_ref = ?`, workerRef).Scan(&task.ID, &task.ConversationID, &task.Text, &task.State, &task.ParentTaskID, &task.ParentAttemptID, &task.ChildIndex, newTimestampScanner(&task.CreatedAt), newTimestampScanner(&task.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

func (s *Store) ActiveAttemptForWorker(ctx context.Context, workerRef string) (Attempt, error) {
	var attempt Attempt
	err := s.db.QueryRowContext(ctx, `SELECT a.id, a.worker_binding_id, a.number, a.state, a.created_at, a.updated_at FROM attempts a JOIN worker_bindings w ON w.id = a.worker_binding_id WHERE w.worker_ref = ? AND a.state IN (?, ?) ORDER BY a.number DESC LIMIT 1`, workerRef, AttemptStarting, AttemptActive).Scan(&attempt.ID, &attempt.WorkerBindingID, &attempt.Number, &attempt.State, newTimestampScanner(&attempt.CreatedAt), newTimestampScanner(&attempt.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, ErrNotFound
	}
	return attempt, err
}
