package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// A claim lives with an Attempt and stops occupying capacity as soon as that
// Attempt becomes terminal. Heartbeat Attempt IDs and server claims are unioned,
// not summed, to avoid counting the same running Worker twice.
type nodeCapacityReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func nodeCapacityUsed(ctx context.Context, reader nodeCapacityReader, node NodeReference, heartbeat []NodeActiveAttempt) (int, error) {
	rows, err := reader.QueryContext(ctx, `SELECT claim.attempt_id FROM phase4_node_capacity_claims claim
JOIN phase4_attempts attempt ON attempt.id = claim.attempt_id
WHERE claim.node_id = ? AND attempt.state IN ('starting', 'scheduled', 'active')`, node)
	if err != nil {
		return 0, err
	}
	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	count := len(ids)
	for _, attempt := range heartbeat {
		if attempt.AttemptID == "" {
			// A Node may report an attempt without a durable server identity.
			count++
			continue
		}
		if _, found := ids[attempt.AttemptID]; !found {
			ids[attempt.AttemptID] = struct{}{}
			count++
		}
	}
	return count, nil
}

// The UPDATE is a SQLite write lock before reading capacity and claims.
// Concurrent SpawnWorker transactions cannot both observe the last free slot.
func requireNodeCapacityTx(ctx context.Context, tx *sql.Tx, node NodeReference) error {
	result, err := tx.ExecContext(ctx, `UPDATE phase4_nodes SET online = online
WHERE node_ref = ? AND online = 1 AND draining = 0 AND revoked = 0`, node)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: Node %s is offline, draining or unavailable", ErrSelectedNodeUnavailable, node)
	}
	var capacity int
	var encoded string
	if err := tx.QueryRowContext(ctx, `SELECT capacity, active_attempts_json FROM phase4_nodes WHERE node_ref = ?`, node).Scan(&capacity, &encoded); err != nil {
		return err
	}
	var heartbeat []NodeActiveAttempt
	if encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &heartbeat); err != nil {
			return err
		}
	}
	used, err := nodeCapacityUsed(ctx, tx, node, heartbeat)
	if err != nil {
		return err
	}
	if used >= capacity {
		return fmt.Errorf("%w: Node %s has no free execution capacity (%d/%d slots in use)", ErrSelectedNodeUnavailable, node, used, capacity)
	}
	return nil
}

func recordNodeCapacityClaimTx(ctx context.Context, tx *sql.Tx, attemptID string, node NodeReference) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO phase4_node_capacity_claims(attempt_id, node_id) VALUES(?, ?)`, attemptID, node)
	return err
}

func workerHasCapacityClaimsTx(ctx context.Context, tx *sql.Tx, workerID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM phase4_node_capacity_claims claim
JOIN phase4_attempts attempt ON attempt.id = claim.attempt_id
WHERE attempt.worker_id = ?)`, workerID).Scan(&exists)
	return exists, err
}
