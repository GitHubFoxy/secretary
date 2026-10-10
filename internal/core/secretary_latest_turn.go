package core

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) LatestSecretaryTurnID(ctx context.Context, conversationID string) (string, error) {
	var turnID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM secretary_turns WHERE conversation_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, conversationID).Scan(&turnID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return turnID, err
}
