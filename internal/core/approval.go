package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrTrustedLocalApprovalDenied = errors.New("core: trusted local auto-approval is not allowed")
	ErrApprovalResolutionConflict = errors.New("core: approval already has a different durable resolution intent")
)

func (s *Store) Approval(ctx context.Context, requestID string) (Approval, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return Approval{}, errors.New("core: approval request_id is required")
	}
	return scanApproval(s.db.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
}

func (s *Store) Approvals(ctx context.Context) ([]Approval, error) {
	rows, err := s.db.QueryContext(ctx, approvalSelect+` ORDER BY requested_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Approval, 0)
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, approval)
	}
	return result, rows.Err()
}

// ApprovalsForConversation returns at most limit approvals whose worker
// belongs to the conversation. Approvals without a conversation-bound worker
// never cross the public Client API boundary. limit must be positive; the HTTP
// layer owns defaults, clamping and validation.
func (s *Store) ApprovalsForConversation(ctx context.Context, conversationID string, limit int) ([]Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT phase4_approvals.id, phase4_approvals.request_id, phase4_approvals.worker_id, phase4_approvals.turn_id, phase4_approvals.attempt_id, phase4_approvals.node_id, phase4_approvals.project_id, phase4_approvals.kind, phase4_approvals.action_summary, phase4_approvals.risk_category, phase4_approvals.requested_at, phase4_approvals.expires_at, phase4_approvals.state, phase4_approvals.response, phase4_approvals.resolved_by, phase4_approvals.resolved_at, phase4_approvals.audit_event_id, phase4_approvals.resolution_command_id, phase4_approvals.resolution_state, phase4_approvals.resolution_response, phase4_approvals.resolution_resolved_by FROM phase4_approvals JOIN workers ON workers.id = phase4_approvals.worker_id WHERE workers.conversation_id = ? ORDER BY phase4_approvals.requested_at, phase4_approvals.id LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Approval, 0)
	for rows.Next() {
		approval, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, approval)
	}
	return result, rows.Err()
}

const approvalSelect = `SELECT id, request_id, worker_id, turn_id, attempt_id, node_id, project_id, kind, action_summary, risk_category, requested_at, expires_at, state, response, resolved_by, resolved_at, audit_event_id, resolution_command_id, resolution_state, resolution_response, resolution_resolved_by FROM phase4_approvals`

func scanApproval(row interface{ Scan(...any) error }) (Approval, error) {
	var approval Approval
	var expiresAt, resolvedAt sql.NullString
	err := row.Scan(&approval.ID, &approval.RequestID, &approval.WorkerID, &approval.TurnID, &approval.AttemptID, &approval.NodeID, &approval.ProjectID, &approval.Kind, &approval.ActionSummary, &approval.RiskCategory, newTimestampScanner(&approval.RequestedAt), &expiresAt, &approval.State, &approval.Response, &approval.ResolvedBy, &resolvedAt, &approval.AuditEventID, &approval.ResolutionCommandID, &approval.ResolutionState, &approval.ResolutionResponse, &approval.ResolutionResolvedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, ErrNotFound
	}
	if err != nil {
		return Approval{}, err
	}
	var parseErr error
	if expiresAt.Valid {
		value, err := parseTimestamp(expiresAt.String)
		if err != nil {
			parseErr = err
		} else {
			approval.ExpiresAt = &value
		}
	}
	if resolvedAt.Valid {
		value, err := parseTimestamp(resolvedAt.String)
		if err != nil {
			parseErr = err
		} else {
			approval.ResolvedAt = &value
		}
	}
	return approval, parseErr
}

func approvalKindForActivity(kind ActivityKind) (ApprovalKind, bool) {
	switch kind {
	case ActivityPermissionRequest:
		return ApprovalPermission, true
	case ActivityUserInputRequest:
		return ApprovalInput, true
	default:
		return "", false
	}
}

func (s *Store) recordApprovalRequestTx(ctx context.Context, tx *sql.Tx, now time.Time, activity Activity, attempt Phase4Attempt, worker Worker) error {
	kind, ok := approvalKindForActivity(activity.Kind)
	if !ok || activity.Request == nil {
		return nil
	}
	requestID := strings.TrimSpace(activity.Request.RequestID)
	if requestID == "" {
		return errors.New("core: Worker request_id is required")
	}
	existing, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ?`, requestID))
	if err == nil {
		if existing.WorkerID != worker.ID || existing.TurnID != attempt.TurnID || existing.AttemptID != attempt.ID || existing.NodeID != string(attempt.NodeID) || existing.ProjectID != worker.ProjectID {
			return errors.New("core: Worker request_id is bound to another execution")
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	approval := Approval{ID: newID("apr"), RequestID: requestID, WorkerID: worker.ID, TurnID: attempt.TurnID, AttemptID: attempt.ID, NodeID: string(attempt.NodeID), ProjectID: worker.ProjectID, Kind: kind, ActionSummary: strings.TrimSpace(activity.Request.Summary), RiskCategory: strings.TrimSpace(activity.Request.RiskCategory), RequestedAt: now, ExpiresAt: activity.Request.ExpiresAt, State: ApprovalPending}
	if approval.ActionSummary == "" {
		approval.ActionSummary = "Worker request"
	}
	var expiresAt any
	if approval.ExpiresAt != nil {
		expiresAt = timestamp(approval.ExpiresAt.UTC())
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_approvals(id, request_id, worker_id, turn_id, attempt_id, node_id, project_id, kind, action_summary, risk_category, requested_at, expires_at, state, response, resolved_by, audit_event_id) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '')`, approval.ID, approval.RequestID, approval.WorkerID, approval.TurnID, approval.AttemptID, approval.NodeID, approval.ProjectID, approval.Kind, approval.ActionSummary, approval.RiskCategory, timestamp(now), expiresAt, approval.State); err != nil {
		return err
	}
	turnState := TurnWaitingApproval
	workerState := WorkerWaitingApproval
	if kind == ApprovalInput {
		turnState = TurnNeedsInput
		workerState = WorkerNeedsInput
	}
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ? AND state IN ('starting', 'active', 'waiting_approval', 'needs_input')`, turnState, timestamp(now), attempt.TurnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, workerState, timestamp(now), worker.ID); err != nil {
		return err
	}
	eventKind := "approval.requested"
	event, err := appendEventTx(ctx, tx, now, EventInput{Kind: eventKind, AggregateType: "approval", AggregateID: approval.ID, Source: "node", CorrelationID: attempt.TurnID, WorkerRef: worker.WorkerRef, AttemptID: attempt.ID, Payload: approval}, approval)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE phase4_approvals SET audit_event_id = ? WHERE id = ?`, event.ID, approval.ID); err != nil {
		return err
	}
	_, _, err = enqueueDeliveryTx(ctx, tx, now, event.ID, "", "client:owner", "approval:"+approval.RequestID)
	return err
}

// ResolveApproval preserves the direct Store API used by policy and tests.
// Client-facing response delivery should call CommitApprovalResolution instead.
func (s *Store) ResolveApproval(ctx context.Context, requestID string, state ApprovalState, resolvedBy, response string) (Approval, bool, error) {
	return s.commitApprovalResolution(ctx, requestID, state, resolvedBy, response)
}

// CommitApprovalResolution commits the durable Approval state after the Node
// has accepted the generic RespondWorker handoff. It is idempotent, so a crash
// after delivery and before this commit only replays the durable transition.
func (s *Store) CommitApprovalResolution(ctx context.Context, requestID string, state ApprovalState, resolvedBy, response string) (Approval, bool, error) {
	return s.commitApprovalResolution(ctx, requestID, state, resolvedBy, response)
}

// BeginApprovalResolution durably claims the first owner decision before the
// generic respond_worker command is handed to the Node. The response remains
// internal to Core and is never included in events or public DTOs.
func (s *Store) BeginApprovalResolution(ctx context.Context, requestID, commandID string, state ApprovalState, resolvedBy, response string) (Approval, bool, error) {
	requestID, commandID, resolvedBy = strings.TrimSpace(requestID), strings.TrimSpace(commandID), strings.TrimSpace(resolvedBy)
	if requestID == "" || commandID == "" || resolvedBy == "" || (state != ApprovalApproved && state != ApprovalDenied) {
		return Approval{}, false, errors.New("core: approval resolution identity is incomplete")
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		approval Approval
		dup      bool
	}, error) {
		approval, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		if approval.State == ApprovalResolving {
			if approval.ResolutionCommandID == commandID && approval.ResolutionState == state && approval.ResolutionResponse == response {
				return struct {
					approval Approval
					dup      bool
				}{approval: approval, dup: true}, nil
			}
			return struct {
				approval Approval
				dup      bool
			}{}, ErrApprovalResolutionConflict
		}
		if approval.State != ApprovalPending {
			return struct {
				approval Approval
				dup      bool
			}{approval: approval, dup: true}, nil
		}
		now := s.now()
		if approval.ExpiresAt != nil && !now.Before(*approval.ExpiresAt) {
			if err := transitionApprovalTx(ctx, tx, now, approval, ApprovalExpired, "", "system"); err != nil {
				return struct {
					approval Approval
					dup      bool
				}{}, err
			}
			approval, err = scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
			return struct {
				approval Approval
				dup      bool
			}{approval: approval, dup: true}, err
		}

		var commandWorkerID, commandAttemptID, commandKind string
		var commandState WorkerCommandState
		var attemptNode string
		var attemptTurnID string
		var attemptState AttemptState
		var turnState TurnState
		if err := tx.QueryRowContext(ctx, `SELECT c.worker_id, c.attempt_id, c.kind, c.state, a.node_id, a.turn_id, a.state, t.state
FROM phase4_worker_commands c JOIN phase4_attempts a ON a.id = c.attempt_id JOIN turns t ON t.id = a.turn_id
WHERE c.id = ?`, commandID).Scan(&commandWorkerID, &commandAttemptID, &commandKind, &commandState, &attemptNode, &attemptTurnID, &attemptState, &turnState); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return struct {
					approval Approval
					dup      bool
				}{}, ErrNotFound
			}
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		if commandWorkerID != approval.WorkerID || commandAttemptID != approval.AttemptID || commandKind != "respond" || attemptNode != approval.NodeID || attemptTurnID != approval.TurnID || commandState != WorkerCommandPending || attemptState.Terminal() || turnState.Terminal() {
			return struct {
				approval Approval
				dup      bool
			}{}, ErrInvalidTransition
		}
		result, err := tx.ExecContext(ctx, `UPDATE phase4_approvals SET state = ?, resolution_command_id = ?, resolution_state = ?, resolution_response = ?, resolution_resolved_by = ? WHERE id = ? AND state = ? AND resolution_command_id = ''`, ApprovalResolving, commandID, state, response, resolvedBy, approval.ID, ApprovalPending)
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		if affected != 1 {
			return struct {
				approval Approval
				dup      bool
			}{}, ErrApprovalResolutionConflict
		}
		workerRef, err := workerRefForID(ctx, tx, approval.WorkerID)
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		payload := map[string]any{"request_id": approval.RequestID, "state": ApprovalResolving, "decision": state, "resolved_by": resolvedBy, "worker_id": approval.WorkerID, "turn_id": approval.TurnID, "attempt_id": approval.AttemptID}
		event, err := appendEventTx(ctx, tx, now, EventInput{Kind: "approval.resolution_pending", AggregateType: "approval", AggregateID: approval.ID, Source: "server", CorrelationID: approval.TurnID, WorkerRef: workerRef, AttemptID: approval.AttemptID, Payload: payload}, payload)
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "client:owner", "approval.resolution-pending:"+approval.RequestID); err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		approval, err = scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
		return struct {
			approval Approval
			dup      bool
		}{approval: approval}, err
	})
	return returnValue.approval, returnValue.dup, err
}

func (s *Store) commitApprovalResolution(ctx context.Context, requestID string, state ApprovalState, resolvedBy, response string) (Approval, bool, error) {
	requestID = strings.TrimSpace(requestID)
	resolvedBy = strings.TrimSpace(resolvedBy)
	if requestID == "" || resolvedBy == "" {
		return Approval{}, false, errors.New("core: approval request_id and resolving client are required")
	}
	if state != ApprovalApproved && state != ApprovalDenied {
		return Approval{}, false, errors.New("core: approval resolution must be approved or denied")
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		approval Approval
		dup      bool
	}, error) {
		approval, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		if approval.State != ApprovalPending {
			return struct {
				approval Approval
				dup      bool
			}{approval: approval, dup: true}, nil
		}
		now := s.now()
		if approval.ExpiresAt != nil && !now.Before(*approval.ExpiresAt) {
			if err := transitionApprovalTx(ctx, tx, now, approval, ApprovalExpired, "", "system"); err != nil {
				return struct {
					approval Approval
					dup      bool
				}{}, err
			}
			approval, err = scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
			return struct {
				approval Approval
				dup      bool
			}{approval: approval, dup: true}, err
		}
		if err := transitionApprovalTx(ctx, tx, now, approval, state, response, resolvedBy); err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		approval, err = scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
		return struct {
			approval Approval
			dup      bool
		}{approval: approval}, err
	})
	return returnValue.approval, returnValue.dup, err
}

func transitionApprovalTx(ctx context.Context, tx *sql.Tx, now time.Time, approval Approval, state ApprovalState, response, resolvedBy string) error {
	if approval.State != ApprovalPending && approval.State != ApprovalResolving {
		return ErrInvalidTransition
	}
	resolvedAt := timestamp(now)
	result, err := tx.ExecContext(ctx, `UPDATE phase4_approvals SET state = ?, response = ?, resolved_by = ?, resolved_at = ? WHERE id = ? AND state = ?`, state, response, resolvedBy, resolvedAt, approval.ID, approval.State)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrInvalidTransition
	}
	eventKind := "approval." + string(state)
	if state == ApprovalRevoked {
		eventKind = "approval.revoked"
	}
	workerRef, err := workerRefForID(ctx, tx, approval.WorkerID)
	if err != nil {
		return err
	}
	payload := map[string]any{"request_id": approval.RequestID, "state": state, "resolved_by": resolvedBy, "worker_id": approval.WorkerID, "turn_id": approval.TurnID, "attempt_id": approval.AttemptID, "node_id": approval.NodeID, "project_id": approval.ProjectID}
	event, err := appendEventTx(ctx, tx, now, EventInput{Kind: eventKind, AggregateType: "approval", AggregateID: approval.ID, Source: "server", CorrelationID: approval.TurnID, WorkerRef: workerRef, AttemptID: approval.AttemptID, Payload: payload}, payload)
	if err != nil {
		return err
	}
	if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, "", "client:owner", "approval.resolution:"+approval.RequestID+":"+string(state)); err != nil {
		return err
	}
	if state == ApprovalApproved {
		turn, err := getTurn(ctx, tx, approval.TurnID)
		if err != nil {
			return err
		}
		if !turn.State.Terminal() {
			if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ? AND state NOT IN ('succeeded', 'failed', 'canceled', 'interrupted')`, TurnActive, timestamp(now), approval.TurnID); err != nil {
				return err
			}
			var currentTurnID string
			if err := tx.QueryRowContext(ctx, `SELECT current_turn_id FROM workers WHERE id = ?`, approval.WorkerID).Scan(&currentTurnID); err != nil {
				return err
			}
			if currentTurnID == approval.TurnID {
				if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, WorkerWorking, timestamp(now), approval.WorkerID); err != nil {
					return err
				}
			}
		}
	} else {
		if err := finalizeApprovalAttemptTx(ctx, tx, now, approval, state); err != nil {
			return err
		}
	}
	return nil
}

func finalizeApprovalAttemptTx(ctx context.Context, tx *sql.Tx, now time.Time, approval Approval, state ApprovalState) error {
	turn, err := getTurn(ctx, tx, approval.TurnID)
	if err != nil {
		return err
	}
	if turn.State.Terminal() || turn.ResultID != "" {
		return nil
	}
	var existingOutcome AttemptOutcome
	if err := scanPhase4Outcome(tx.QueryRowContext(ctx, `SELECT id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at FROM phase4_attempt_outcomes WHERE attempt_id = ?`, approval.AttemptID), &existingOutcome); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	errorCode := "approval_" + string(state)
	summary := "Worker request " + string(state)
	outcome := AttemptOutcome{ID: newID("aou"), AttemptID: approval.AttemptID, Status: OutcomeFailed, Classification: OutcomeFinal, ErrorCode: errorCode, Diagnostics: "No machine action was rerun after Worker request resolution", CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempt_outcomes(id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, outcome.ID, outcome.AttemptID, outcome.Status, outcome.Classification, outcome.ErrorCode, outcome.ErrorMessage, outcome.Diagnostics, timestamp(now)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE phase4_attempts SET state = ?, updated_at = ? WHERE id = ? AND state NOT IN ('succeeded', 'failed', 'canceled', 'interrupted')`, AttemptFailed, timestamp(now), approval.AttemptID); err != nil {
		return err
	}
	workerRef, err := workerRefForID(ctx, tx, approval.WorkerID)
	if err != nil {
		return err
	}
	if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: approval.AttemptID, Source: "server", CorrelationID: approval.TurnID, WorkerRef: workerRef, AttemptID: approval.AttemptID, Payload: outcome}, outcome); err != nil {
		return err
	}
	result := Phase4Result{ID: newID("res"), WorkerID: approval.WorkerID, TurnID: approval.TurnID, AttemptID: approval.AttemptID, Status: ResultFailed, Summary: summary, FailureCode: errorCode, CorrelationID: approval.TurnID, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_results(id, worker_id, turn_id, attempt_id, status, summary, failure_code, artifact_refs, correlation_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, result.ID, result.WorkerID, result.TurnID, result.AttemptID, result.Status, result.Summary, result.FailureCode, result.ArtifactRefs, result.CorrelationID, timestamp(now)); err != nil {
		return err
	}
	if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "result.accepted", AggregateType: "result", AggregateID: result.ID, Source: "server", CorrelationID: result.CorrelationID, CausationID: outcome.ID, AttemptID: approval.AttemptID, Payload: result}, result); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, result_id = ?, updated_at = ? WHERE id = ?`, TurnFailed, result.ID, timestamp(now), approval.TurnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workers SET status = ?, last_result_summary = ?, updated_at = ? WHERE id = ?`, WorkerIdle, result.Summary, timestamp(now), approval.WorkerID); err != nil {
		return err
	}
	conversationID, err := conversationForWorker(ctx, tx, approval.WorkerID)
	if err != nil {
		return err
	}
	_, err = appendEntryWithIdentity(ctx, tx, now, conversationID, EntryWorkerResult, result.Summary, workerRef, result.TurnID, result.ID)
	if err != nil {
		return err
	}
	return recordSecretaryOriginResultsForWorkerTurnTx(ctx, tx, result.TurnID, now)
}

func (s *Store) ExpireApproval(ctx context.Context, requestID string, now time.Time) (Approval, bool, error) {
	return s.resolveTerminalApproval(ctx, requestID, ApprovalExpired, "", "system", now)
}

func (s *Store) ExpireApprovals(ctx context.Context, now time.Time) ([]Approval, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT request_id FROM phase4_approvals WHERE state = ? AND expires_at IS NOT NULL AND expires_at <= ? ORDER BY requested_at, id`, ApprovalPending, timestamp(now.UTC()))
	if err != nil {
		return nil, err
	}
	var requestIDs []string
	for rows.Next() {
		var requestID string
		if err := rows.Scan(&requestID); err != nil {
			rows.Close()
			return nil, err
		}
		requestIDs = append(requestIDs, requestID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	expired := make([]Approval, 0, len(requestIDs))
	for _, requestID := range requestIDs {
		approval, _, err := s.ExpireApproval(ctx, requestID, now)
		if err != nil {
			return nil, err
		}
		expired = append(expired, approval)
	}
	return expired, nil
}

func (s *Store) RevokeApproval(ctx context.Context, requestID, reason string) (Approval, bool, error) {
	return s.resolveTerminalApproval(ctx, requestID, ApprovalRevoked, reason, "system", s.now())
}

func (s *Store) resolveTerminalApproval(ctx context.Context, requestID string, state ApprovalState, response, resolvedBy string, now time.Time) (Approval, bool, error) {
	if state != ApprovalExpired && state != ApprovalRevoked {
		return Approval{}, false, fmt.Errorf("core: invalid terminal approval state %q", state)
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (struct {
		approval Approval
		dup      bool
	}, error) {
		approval, err := scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
		if err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		if approval.State != ApprovalPending {
			return struct {
				approval Approval
				dup      bool
			}{approval: approval, dup: true}, nil
		}
		if err := transitionApprovalTx(ctx, tx, now.UTC(), approval, state, response, resolvedBy); err != nil {
			return struct {
				approval Approval
				dup      bool
			}{}, err
		}
		approval, err = scanApproval(tx.QueryRowContext(ctx, approvalSelect+` WHERE request_id = ? OR id = ?`, requestID, requestID))
		return struct {
			approval Approval
			dup      bool
		}{approval: approval}, err
	})
	return returnValue.approval, returnValue.dup, err
}

func (s *Store) ApplyTrustedLocalApproval(ctx context.Context, requestID string, policy TrustedLocalApprovalPolicy) (Approval, error) {
	approval, err := s.Approval(ctx, requestID)
	if err != nil {
		return Approval{}, err
	}
	if approval.State != ApprovalPending {
		return approval, nil
	}
	if !policy.Enabled || !policy.Explicit {
		return approval, nil
	}
	if !policy.LocalNode || strings.TrimSpace(string(policy.Node)) == "" || string(policy.Node) != approval.NodeID {
		return approval, ErrTrustedLocalApprovalDenied
	}
	resolved, _, err := s.ResolveApproval(ctx, requestID, ApprovalApproved, "trusted-local-policy", "auto_approved")
	if err != nil {
		return Approval{}, err
	}
	_, err = s.RecordEventWithMetadata(ctx, EventInput{Kind: "approval.auto_approved", AggregateType: "approval", AggregateID: resolved.ID, Source: "policy", CorrelationID: resolved.TurnID, AttemptID: resolved.AttemptID, Payload: map[string]any{"request_id": requestID, "node_id": resolved.NodeID, "policy": "trusted_local_explicit"}})
	return resolved, err
}
