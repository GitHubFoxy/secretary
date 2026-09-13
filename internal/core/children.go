package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const MaxChildrenPerAttempt = 4

// CreateChildTask creates a dispatchable child owned by the active parent
// Attempt. It never appends a user-visible conversation entry.
func (s *Store) CreateChildTask(ctx context.Context, parentWorkerRef, parentAttemptID, text string) (Task, error) {
	if s.legacyTasksReadOnly(ctx) {
		return Task{}, ErrLegacyTaskReadOnly
	}
	if strings.TrimSpace(parentWorkerRef) == "" {
		return Task{}, errors.New("core: parent Worker reference is required")
	}
	if strings.TrimSpace(parentAttemptID) == "" {
		return Task{}, errors.New("core: parent Attempt is required")
	}
	if strings.TrimSpace(text) == "" {
		return Task{}, errors.New("core: child task text is required")
	}
	created, err := withTx(s, ctx, func(tx *sql.Tx) (Task, error) {
		var parent Task
		if err := tx.QueryRowContext(ctx, `SELECT t.id, t.conversation_id, t.text, t.state, t.parent_task_id, t.parent_attempt_id, t.child_index, t.created_at, t.updated_at FROM tasks t JOIN worker_bindings w ON w.task_id = t.id JOIN attempts a ON a.worker_binding_id = w.id WHERE w.worker_ref = ? AND a.id = ?`, parentWorkerRef, parentAttemptID).Scan(&parent.ID, &parent.ConversationID, &parent.Text, &parent.State, &parent.ParentTaskID, &parent.ParentAttemptID, &parent.ChildIndex, newTimestampScanner(&parent.CreatedAt), newTimestampScanner(&parent.UpdatedAt)); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Task{}, ErrNotFound
			}
			return Task{}, err
		}
		var attemptState AttemptState
		if err := tx.QueryRowContext(ctx, `SELECT state FROM attempts WHERE id = ? AND worker_binding_id = (SELECT id FROM worker_bindings WHERE worker_ref = ?)`, parentAttemptID, parentWorkerRef).Scan(&attemptState); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Task{}, ErrNotFound
			}
			return Task{}, err
		}
		if parent.State != TaskOpen || (attemptState != AttemptStarting && attemptState != AttemptActive) {
			return Task{}, ErrInvalidTransition
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE parent_attempt_id = ?`, parentAttemptID).Scan(&count); err != nil {
			return Task{}, err
		}
		if count >= MaxChildrenPerAttempt {
			return Task{}, errors.New("core: parent Attempt reached child limit")
		}
		now := s.now()
		child := Task{ID: newID("tsk"), ConversationID: parent.ConversationID, Text: strings.TrimSpace(text), State: TaskDispatching, ParentTaskID: parent.ID, ParentAttemptID: parentAttemptID, ChildIndex: count + 1, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tasks(id, conversation_id, text, state, parent_task_id, parent_attempt_id, child_index, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, child.ID, child.ConversationID, child.Text, child.State, child.ParentTaskID, child.ParentAttemptID, child.ChildIndex, timestamp(now), timestamp(now)); err != nil {
			return Task{}, err
		}
		return child, nil
	})
	if err == nil {
		_, _ = s.RecordEvent(ctx, "child.created", parentWorkerRef, parentAttemptID, "", map[string]any{"task_id": created.ID, "text": created.Text, "child_index": created.ChildIndex})
	}
	return created, err
}

func (s *Store) ChildrenForAttempt(ctx context.Context, attemptID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, text, state, parent_task_id, parent_attempt_id, child_index, created_at, updated_at FROM tasks WHERE parent_attempt_id = ? ORDER BY child_index`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var children []Task
	for rows.Next() {
		var child Task
		if err := rows.Scan(&child.ID, &child.ConversationID, &child.Text, &child.State, &child.ParentTaskID, &child.ParentAttemptID, &child.ChildIndex, newTimestampScanner(&child.CreatedAt), newTimestampScanner(&child.UpdatedAt)); err != nil {
			return nil, err
		}
		children = append(children, child)
	}
	return children, rows.Err()
}
