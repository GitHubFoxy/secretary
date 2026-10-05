package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MarkWorkerCommandUncertain records that handoff may have reached the Node,
// but no authoritative outcome arrived before the caller stopped waiting. It
// keeps the command retryable with the same ID and does not classify it as a
// rejection.
func (s *Store) MarkWorkerCommandUncertain(ctx context.Context, commandID, message string) (WorkerCommand, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (WorkerCommand, error) {
		var command WorkerCommand
		if err := scanWorkerCommand(tx.QueryRowContext(ctx, `SELECT id, kind, dedupe_key, worker_id, attempt_id, state, last_error, lease_until, created_at, updated_at FROM phase4_worker_commands WHERE id = ?`, commandID), &command); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return WorkerCommand{}, ErrNotFound
			}
			return WorkerCommand{}, err
		}
		var receiptState WorkerCommandReceiptState
		err := tx.QueryRowContext(ctx, `SELECT state FROM phase4_worker_command_outcomes WHERE command_id = ?`, command.ID).Scan(&receiptState)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return WorkerCommand{}, err
		}
		if command.State == WorkerCommandDelivered || receiptState == WorkerCommandReceiptAccepted || (receiptState == WorkerCommandReceiptDenied && command.State != WorkerCommandPending && command.State != WorkerCommandUncertain) {
			return command, nil
		}
		now := s.now()
		message = strings.TrimSpace(message)
		if _, err := tx.ExecContext(ctx, `UPDATE phase4_worker_commands SET state = ?, last_error = ?, lease_until = '', updated_at = ? WHERE id = ?`, WorkerCommandUncertain, message, timestamp(now), command.ID); err != nil {
			return WorkerCommand{}, err
		}
		command.State, command.LastError, command.LeaseUntil, command.UpdatedAt = WorkerCommandUncertain, message, time.Time{}, now
		return command, nil
	})
}

// RecoverApprovalResolutionCommands releases leases left by a server process
// that stopped after persisting an Approval intent. It records uncertainty but
// never sends a command; the owner must explicitly request the same saved retry.
func (s *Store) RecoverApprovalResolutionCommands(ctx context.Context) (int64, error) {
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (int64, error) {
		result, err := tx.ExecContext(ctx, `UPDATE phase4_worker_commands
SET state = ?, last_error = 'server restarted before Approval resolution completed', lease_until = '', updated_at = ?
WHERE kind = 'respond' AND state = ? AND EXISTS (
	SELECT 1 FROM phase4_approvals p WHERE p.state = ? AND p.resolution_command_id = phase4_worker_commands.id
)`, WorkerCommandUncertain, timestamp(s.now()), WorkerCommandPending, ApprovalResolving)
		if err != nil {
			return 0, err
		}
		return result.RowsAffected()
	})
	return returnValue, err
}

// ReconcileWorkerCommandReceipt commits an authenticated Node outcome against
// the immutable command→Attempt→Turn binding. Accepted receipts supersede an
// uncertain handoff; denials remain terminal until an explicit Core retry
// reopens the command. Duplicate and superseded receipts are idempotent.
func (s *Store) ReconcileWorkerCommandReceipt(ctx context.Context, node NodeReference, receipt WorkerCommandReceipt) (WorkerCommand, error) {
	if strings.TrimSpace(string(node)) == "" || strings.TrimSpace(receipt.CommandID) == "" || receipt.Kind != "respond" {
		return WorkerCommand{}, ErrInvalidTransition
	}
	switch receipt.State {
	case WorkerCommandReceiptAccepted, WorkerCommandReceiptDenied, WorkerCommandReceiptUncertain:
	default:
		return WorkerCommand{}, ErrInvalidTransition
	}

	return withTx(s, ctx, func(tx *sql.Tx) (WorkerCommand, error) {
		var command WorkerCommand
		var attemptNode, turnID string
		if err := tx.QueryRowContext(ctx, `SELECT c.id, c.kind, c.dedupe_key, c.worker_id, c.attempt_id, c.state, c.last_error, c.lease_until, c.created_at, c.updated_at, a.node_id, a.turn_id
FROM phase4_worker_commands c JOIN phase4_attempts a ON a.id = c.attempt_id WHERE c.id = ?`, receipt.CommandID).Scan(&command.ID, &command.Kind, &command.DedupeKey, &command.WorkerID, &command.AttemptID, &command.State, &command.LastError, newTimestampScanner(&command.LeaseUntil), newTimestampScanner(&command.CreatedAt), newTimestampScanner(&command.UpdatedAt), &attemptNode, &turnID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return WorkerCommand{}, ErrNotFound
			}
			return WorkerCommand{}, err
		}
		if command.Kind != receipt.Kind || attemptNode != string(node) || receipt.TurnID != turnID || receipt.AttemptID != command.AttemptID {
			return WorkerCommand{}, ErrInvalidTransition
		}

		var priorState WorkerCommandReceiptState
		var priorNode, priorKind, priorTurnID, priorAttemptID string
		err := tx.QueryRowContext(ctx, `SELECT node_id, kind, turn_id, attempt_id, state FROM phase4_worker_command_outcomes WHERE command_id = ?`, command.ID).Scan(&priorNode, &priorKind, &priorTurnID, &priorAttemptID, &priorState)
		hasPrior := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return WorkerCommand{}, err
		}
		if hasPrior {
			if priorNode != string(node) || priorKind != receipt.Kind || priorTurnID != turnID || priorAttemptID != command.AttemptID {
				return WorkerCommand{}, ErrInvalidTransition
			}
			switch priorState {
			case WorkerCommandReceiptAccepted:
				if receipt.State != WorkerCommandReceiptAccepted {
					return command, nil
				}
			case WorkerCommandReceiptDenied:
				// Only an explicit Core retry reopens the command. Its handoff may
				// already be uncertain when the corresponding receipt arrives.
				if command.State != WorkerCommandPending && command.State != WorkerCommandUncertain {
					return command, nil
				}
			case WorkerCommandReceiptUncertain:
				if receipt.State == WorkerCommandReceiptUncertain && command.State != WorkerCommandPending {
					return command, nil
				}
			}
		} else if command.State == WorkerCommandDelivered && receipt.State != WorkerCommandReceiptAccepted {
			return command, nil
		}

		now := s.now()
		message := strings.TrimSpace(receipt.ErrorMessage)
		if message == "" {
			message = strings.TrimSpace(receipt.ErrorCode)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_worker_command_outcomes(command_id, node_id, kind, turn_id, attempt_id, state, error_code, error_message, received_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(command_id) DO UPDATE SET node_id = excluded.node_id, kind = excluded.kind, turn_id = excluded.turn_id, attempt_id = excluded.attempt_id, state = excluded.state, error_code = excluded.error_code, error_message = excluded.error_message, received_at = excluded.received_at`, command.ID, node, receipt.Kind, turnID, command.AttemptID, receipt.State, strings.TrimSpace(receipt.ErrorCode), strings.TrimSpace(receipt.ErrorMessage), timestamp(now)); err != nil {
			return WorkerCommand{}, err
		}

		var nextState WorkerCommandState
		var lastError string
		switch receipt.State {
		case WorkerCommandReceiptAccepted:
			nextState = WorkerCommandDelivered
		case WorkerCommandReceiptDenied:
			nextState, lastError = WorkerCommandFailed, message
		case WorkerCommandReceiptUncertain:
			nextState, lastError = WorkerCommandUncertain, message
		}
		if _, err := tx.ExecContext(ctx, `UPDATE phase4_worker_commands SET state = ?, last_error = ?, lease_until = '', updated_at = ? WHERE id = ?`, nextState, lastError, timestamp(now), command.ID); err != nil {
			return WorkerCommand{}, err
		}
		command.State, command.LastError, command.LeaseUntil, command.UpdatedAt = nextState, lastError, time.Time{}, now
		if receipt.State == WorkerCommandReceiptAccepted {
			if err := linkSecretaryWorkerCommandOriginsTx(ctx, tx, command.ID, now); err != nil {
				return WorkerCommand{}, err
			}
			handled, err := commitApprovalResolutionIntentTx(ctx, tx, command.ID, now)
			if err != nil {
				return WorkerCommand{}, err
			}
			if !handled {
				if err := resumeAcceptedWorkerResponseTx(ctx, tx, command.AttemptID, now); err != nil {
					return WorkerCommand{}, err
				}
			}
		}
		return command, nil
	})
}

func commitApprovalResolutionIntentTx(ctx context.Context, tx *sql.Tx, commandID string, now time.Time) (bool, error) {
	approval, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE resolution_command_id = ?`, commandID))
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if approval.ResolutionState != ApprovalApproved && approval.ResolutionState != ApprovalDenied {
		return false, ErrInvalidTransition
	}
	if approval.State == approval.ResolutionState {
		return true, nil
	}
	if approval.State != ApprovalResolving {
		return false, ErrInvalidTransition
	}
	if err := transitionApprovalTx(ctx, tx, now, approval, approval.ResolutionState, approval.ResolutionResponse, approval.ResolutionResolvedBy); err != nil {
		return false, err
	}
	return true, nil
}

func resumeAcceptedWorkerResponseTx(ctx context.Context, tx *sql.Tx, attemptID string, now time.Time) error {
	attempt, err := getPhase4Attempt(ctx, tx, attemptID)
	if err != nil {
		return err
	}
	if attempt.State != AttemptActive {
		return nil
	}
	turn, err := getTurn(ctx, tx, attempt.TurnID)
	if err != nil {
		return err
	}
	if turn.State != TurnNeedsInput {
		// A final Result or another lifecycle transition already superseded the
		// response receipt. Never move a terminal Worker Turn back to active.
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ? AND state = ?`, TurnActive, timestamp(now), turn.ID, TurnNeedsInput); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerWorking, timestamp(now), attempt.WorkerID)
	return err
}
