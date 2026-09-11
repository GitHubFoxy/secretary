package core

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) TaskDetails(ctx context.Context, taskID string) (TaskDetails, error) {
	return s.taskDetails(ctx, taskID, 0)
}

func (s *Store) taskDetails(ctx context.Context, taskID string, depth int) (TaskDetails, error) {
	if depth > 16 {
		return TaskDetails{}, errors.New("core: child tree exceeds maximum depth")
	}
	task, err := s.Task(ctx, taskID)
	if err != nil {
		return TaskDetails{}, err
	}
	details := TaskDetails{Task: task, Attempts: []Attempt{}, Results: []Result{}}

	var binding WorkerBinding
	err = s.db.QueryRowContext(ctx, `SELECT id, task_id, worker_ref, node_id, runtime_session_id, workspace, parent_binding_id, parent_attempt_id, profile_version, profile_name, profile_hash, runtime, model, reasoning, allow_tools, profile_delivery, archived, created_at FROM worker_bindings WHERE task_id = ?`, taskID).Scan(
		&binding.ID, &binding.TaskID, &binding.WorkerRef, &binding.NodeID, &binding.RuntimeSessionID, &binding.Workspace, &binding.ParentBindingID, &binding.ParentAttemptID, &binding.Profile.Version, &binding.Profile.Name, &binding.Profile.Hash, &binding.Profile.Runtime, &binding.Profile.Model, &binding.Profile.Reasoning, &binding.Profile.Tools, &binding.Profile.Delivery, &binding.Archived, newTimestampScanner(&binding.CreatedAt),
	)
	if err == nil {
		details.Binding = &binding
	} else if !errors.Is(err, sql.ErrNoRows) {
		return TaskDetails{}, err
	}

	if details.Binding != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT id, worker_binding_id, number, state, created_at, updated_at FROM attempts WHERE worker_binding_id = ? ORDER BY number`, details.Binding.ID)
		if err != nil {
			return TaskDetails{}, err
		}
		for rows.Next() {
			var attempt Attempt
			if err := rows.Scan(&attempt.ID, &attempt.WorkerBindingID, &attempt.Number, &attempt.State, newTimestampScanner(&attempt.CreatedAt), newTimestampScanner(&attempt.UpdatedAt)); err != nil {
				rows.Close()
				return TaskDetails{}, err
			}
			details.Attempts = append(details.Attempts, attempt)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return TaskDetails{}, err
		}
		rows.Close()
	}

	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.attempt_id, r.status, r.summary, r.created_at FROM results r JOIN attempts a ON a.id = r.attempt_id JOIN worker_bindings w ON w.id = a.worker_binding_id WHERE w.task_id = ? ORDER BY a.number`, taskID)
	if err != nil {
		return TaskDetails{}, err
	}
	for rows.Next() {
		var result Result
		if err := rows.Scan(&result.ID, &result.AttemptID, &result.Status, &result.Summary, newTimestampScanner(&result.CreatedAt)); err != nil {
			return TaskDetails{}, err
		}
		details.Results = append(details.Results, result)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return TaskDetails{}, err
	}
	rows.Close()
	children, err := s.db.QueryContext(ctx, `SELECT id FROM tasks WHERE parent_task_id = ? ORDER BY child_index`, taskID)
	if err != nil {
		return TaskDetails{}, err
	}
	for children.Next() {
		var childID string
		if err := children.Scan(&childID); err != nil {
			children.Close()
			return TaskDetails{}, err
		}
		child, err := s.taskDetails(ctx, childID, depth+1)
		if err != nil {
			children.Close()
			return TaskDetails{}, err
		}
		details.Children = append(details.Children, child)
	}
	if err := children.Err(); err != nil {
		children.Close()
		return TaskDetails{}, err
	}
	children.Close()
	return details, nil
}
