package core

import (
	"context"
	"database/sql"
	"errors"
)

func workerRefForID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, workerID string) (string, error) {
	var workerRef string
	err := q.QueryRowContext(ctx, `SELECT worker_ref FROM workers WHERE id = ?`, workerID).Scan(&workerRef)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return workerRef, err
}
