package core

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type QueuedWorkerMessage struct {
	ID        string    `json:"id"`
	WorkerID  string    `json:"worker_id"`
	Sequence  int64     `json:"sequence"`
	Text      string    `json:"text"`
	State     string    `json:"state"`
	TurnID    string    `json:"turn_id,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const queuedWorkerSelect = `SELECT id,worker_id,sequence,text,state,turn_id,last_error,created_at,updated_at FROM worker_queued_messages`

func scanQueuedWorker(row interface{ Scan(...any) error }) (QueuedWorkerMessage, error) {
	var m QueuedWorkerMessage
	err := row.Scan(&m.ID, &m.WorkerID, &m.Sequence, &m.Text, &m.State, &m.TurnID, &m.LastError, newTimestampScanner(&m.CreatedAt), newTimestampScanner(&m.UpdatedAt))
	return m, err
}
func (s *Store) QueuedWorkerMessages(ctx context.Context, workerID string) ([]QueuedWorkerMessage, error) {
	rows, err := s.db.QueryContext(ctx, queuedWorkerSelect+` WHERE worker_id=? ORDER BY sequence`, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []QueuedWorkerMessage{}
	for rows.Next() {
		m, err := scanQueuedWorker(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func queueEventTx(ctx context.Context, tx *sql.Tx, m QueuedWorkerMessage) error {
	worker, err := getWorker(ctx, tx, m.WorkerID)
	if err != nil {
		return err
	}
	event, err := appendEventTx(ctx, tx, m.UpdatedAt, EventInput{Kind: "worker.message." + m.State, AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, Source: "server", CorrelationID: m.ID, Payload: m}, m)
	if err != nil {
		return err
	}
	_, _, err = enqueueDeliveryTx(ctx, tx, m.UpdatedAt, event.ID, "", "conversation", "worker-queue:"+event.ID)
	return err
}
func (s *Store) EnqueueWorkerMessage(ctx context.Context, workerID, text, key string, origins ...SecretaryOriginIdentity) (QueuedWorkerMessage, error) {
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	return withTx(s, ctx, func(tx *sql.Tx) (QueuedWorkerMessage, error) {
		m, err := scanQueuedWorker(tx.QueryRowContext(ctx, queuedWorkerSelect+` WHERE worker_id=? AND idempotency_key=?`, workerID, key))
		if err == nil {
			if len(origins) > 0 {
				var originTurn, originInput string
				if err := tx.QueryRowContext(ctx, `SELECT secretary_turn_id,input_id FROM worker_queued_messages WHERE id=?`, m.ID).Scan(&originTurn, &originInput); err != nil {
					return m, err
				}
				if originTurn != origins[0].SecretaryTurnID || originInput != origins[0].InputID {
					return m, ErrInvalidSecretaryOrigin
				}
			}
			if m.Text != text {
				return m, ErrIdempotencyConflict
			}
			return m, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return m, err
		}
		worker, err := getWorker(ctx, tx, workerID)
		if err != nil {
			return m, err
		}
		if worker.Status == WorkerClosed {
			return m, ErrInvalidTransition
		}
		origin := SecretaryOriginIdentity{}
		if len(origins) > 0 {
			origin = origins[0]
			secretary, err := authorizedSecretaryOriginTx(ctx, tx, origin.PersonID, origin.Capability, origin.SecretaryTurnID, origin.InputID)
			if err != nil {
				return m, err
			}
			var conversationID string
			if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM workers WHERE id=?`, worker.ID).Scan(&conversationID); err != nil {
				return m, err
			}
			if secretary.State != SecretaryTurnActive || secretary.ConversationID != conversationID {
				return m, ErrInvalidSecretaryOrigin
			}
		}
		var sequence int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0)+1 FROM worker_queued_messages WHERE worker_id=?`, workerID).Scan(&sequence); err != nil {
			return m, err
		}
		now := s.now()
		m = QueuedWorkerMessage{ID: newID("wqm"), WorkerID: workerID, Sequence: sequence, Text: text, State: "pending", CreatedAt: now, UpdatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO worker_queued_messages(id,worker_id,sequence,text,state,idempotency_key,secretary_turn_id,input_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, m.ID, m.WorkerID, m.Sequence, m.Text, m.State, key, origin.SecretaryTurnID, origin.InputID, timestamp(now), timestamp(now))
		if err != nil {
			return m, err
		}
		// Queued acceptance has its own durable lifecycle; no Secretary model input.
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: "worker.message.queued", AggregateType: "worker", AggregateID: worker.WorkerRef, WorkerRef: worker.WorkerRef, Source: "server", CorrelationID: m.ID, Payload: m}, m)
		if err != nil {
			return m, err
		}
		_, _, err = enqueueDeliveryTx(ctx, tx, now, event.ID, "", "conversation", "worker-queue:"+event.ID)
		return m, err
	})
}

// PromoteQueuedWorkerMessage atomically creates the Follow-up and associates it
// with its FIFO message. The durable command intent is created in the same transaction.
func (s *Store) PromoteQueuedWorkerMessage(ctx context.Context, m QueuedWorkerMessage, kind string) (Turn, Phase4Attempt, error) {
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	return s.createTurn(ctx, m.WorkerID, TurnSpec{Input: m.Text, IdempotencyKey: "queue:" + m.ID, CommandKind: kind}, "queue:"+m.ID, m.ID)
}
func (s *Store) CompleteQueuedWorkerMessage(ctx context.Context, id, state, lastError string) error {
	_, err := withTx(s, ctx, func(tx *sql.Tx) (QueuedWorkerMessage, error) {
		m, err := scanQueuedWorker(tx.QueryRowContext(ctx, queuedWorkerSelect+` WHERE id=?`, id))
		if err != nil {
			return m, err
		}
		if m.State != "delivering" {
			return m, nil
		}
		m.State = state
		m.LastError = lastError
		m.UpdatedAt = s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE worker_queued_messages SET state=?,last_error=?,updated_at=? WHERE id=?`, state, lastError, timestamp(m.UpdatedAt), id); err != nil {
			return m, err
		}
		return m, queueEventTx(ctx, tx, m)
	})
	return err
}
func cancelQueuedWorkerMessagesTx(ctx context.Context, tx *sql.Tx, workerID string, now time.Time) error {
	rows, err := tx.QueryContext(ctx, queuedWorkerSelect+` WHERE worker_id=? AND state IN ('pending','delivering','blocked') ORDER BY sequence`, workerID)
	if err != nil {
		return err
	}
	messages := []QueuedWorkerMessage{}
	for rows.Next() {
		m, err := scanQueuedWorker(rows)
		if err != nil {
			rows.Close()
			return err
		}
		messages = append(messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range messages {
		m.State = "canceled"
		m.UpdatedAt = now
		if _, err := tx.ExecContext(ctx, `UPDATE worker_queued_messages SET state='canceled',updated_at=? WHERE id=?`, timestamp(now), m.ID); err != nil {
			return err
		}
		if err := queueEventTx(ctx, tx, m); err != nil {
			return err
		}
	}
	return nil
}

// CancelQueuedWorkerMessages commits closure intent before runtime cancellation,
// so the terminal callback cannot race the pump into starting a queued Follow-up.
func (s *Store) CancelQueuedWorkerMessages(ctx context.Context, workerID string) error {
	_, err := withTx(s, ctx, func(tx *sql.Tx) (struct{}, error) {
		return struct{}{}, cancelQueuedWorkerMessagesTx(ctx, tx, workerID, s.now())
	})
	return err
}
