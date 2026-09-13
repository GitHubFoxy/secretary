package core

import "context"

// WorkerCommands returns durable server-side handoff records for diagnostics.
// It intentionally exposes no Node credential or runtime session state.
func (s *Store) WorkerCommands(ctx context.Context, limit int) ([]WorkerCommand, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, dedupe_key, worker_id, attempt_id, state, last_error, lease_until, created_at, updated_at FROM phase4_worker_commands ORDER BY created_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]WorkerCommand, 0)
	for rows.Next() {
		var command WorkerCommand
		if err := scanWorkerCommand(rows, &command); err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return commands, nil
}
