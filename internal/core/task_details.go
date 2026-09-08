package core

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) TaskDetails(ctx context.Context, taskID string) (TaskDetails, error) {
	task, err := s.Task(ctx, taskID)
	if err != nil {
		return TaskDetails{}, err
	}
	details := TaskDetails{Task: task, Attempts: []Attempt{}, Results: []Result{}}

	var binding WorkerBinding
	err = s.db.QueryRowContext(ctx, `SELECT id, task_id, worker_ref, node_id, runtime_session_id, archived, created_at FROM worker_bindings WHERE task_id = ?`, taskID).Scan(
		&binding.ID, &binding.TaskID, &binding.WorkerRef, &binding.NodeID, &binding.RuntimeSessionID, &binding.Archived, newTimestampScanner(&binding.CreatedAt),
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
	defer rows.Close()
	for rows.Next() {
		var result Result
		if err := rows.Scan(&result.ID, &result.AttemptID, &result.Status, &result.Summary, newTimestampScanner(&result.CreatedAt)); err != nil {
			return TaskDetails{}, err
		}
		details.Results = append(details.Results, result)
	}
	return details, rows.Err()
}
