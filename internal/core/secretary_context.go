package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const secretaryTurnSelect = `SELECT id, identity_id, conversation_id, input, context_snapshot, state, queue_position, error, created_at, started_at, finished_at, updated_at FROM secretary_turns`

// SetSecretaryConversationSummary stores a replaceable server-owned summary
// projection. The full Conversation remains the durable source of history.
func (s *Store) SetSecretaryConversationSummary(ctx context.Context, conversationID, summary string) error {
	conversationID = strings.TrimSpace(conversationID)
	summary = strings.TrimSpace(summary)
	if conversationID == "" {
		return errors.New("core: conversation is required")
	}
	if len(summary) > 10000 {
		return errors.New("core: conversation summary is too long")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO secretary_conversation_summaries(conversation_id, summary, updated_at) VALUES(?, ?, ?) ON CONFLICT(conversation_id) DO UPDATE SET summary = excluded.summary, updated_at = excluded.updated_at`, conversationID, summary, timestamp(s.now()))
	return err
}

func (s *Store) SecretaryConversationSummary(ctx context.Context, conversationID string) (string, error) {
	var summary string
	err := s.db.QueryRowContext(ctx, `SELECT summary FROM secretary_conversation_summaries WHERE conversation_id = ?`, conversationID).Scan(&summary)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return summary, err
}

// SetSecretaryPolicySnapshot persists the current policy and Profile metadata.
// Runtime session details must never be put in this snapshot.
func (s *Store) SetSecretaryPolicySnapshot(ctx context.Context, snapshot SecretaryPolicySnapshot) error {
	snapshot.Version = strings.TrimSpace(snapshot.Version)
	snapshot.Harness = strings.TrimSpace(snapshot.Harness)
	snapshot.Model = strings.TrimSpace(snapshot.Model)
	snapshot.Reasoning = strings.TrimSpace(snapshot.Reasoning)
	snapshot.ProfileVersion = strings.TrimSpace(snapshot.ProfileVersion)
	snapshot.ProfileName = strings.TrimSpace(snapshot.ProfileName)
	snapshot.ProfileHash = strings.TrimSpace(snapshot.ProfileHash)
	snapshot.ProfileRuntime = strings.TrimSpace(snapshot.ProfileRuntime)
	snapshot.ProfileModel = strings.TrimSpace(snapshot.ProfileModel)
	snapshot.ProfileReasoning = strings.TrimSpace(snapshot.ProfileReasoning)
	snapshot.ProfileDelivery = strings.TrimSpace(snapshot.ProfileDelivery)
	snapshot.AllowedTools = append([]string(nil), snapshot.AllowedTools...)
	snapshot.UpdatedAt = s.now()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("core: encode Secretary policy snapshot: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO secretary_policy_snapshots(id, snapshot_json, updated_at) VALUES(1, ?, ?) ON CONFLICT(id) DO UPDATE SET snapshot_json = excluded.snapshot_json, updated_at = excluded.updated_at`, string(encoded), timestamp(snapshot.UpdatedAt))
	return err
}

func (s *Store) SecretaryPolicySnapshot(ctx context.Context) (SecretaryPolicySnapshot, error) {
	var encoded string
	if err := s.db.QueryRowContext(ctx, `SELECT snapshot_json FROM secretary_policy_snapshots WHERE id = 1`).Scan(&encoded); errors.Is(err, sql.ErrNoRows) {
		return SecretaryPolicySnapshot{}, ErrNotFound
	} else if err != nil {
		return SecretaryPolicySnapshot{}, err
	}
	var snapshot SecretaryPolicySnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return SecretaryPolicySnapshot{}, fmt.Errorf("core: decode Secretary policy snapshot: %w", err)
	}
	return snapshot, nil
}

// SecretaryTurns returns durable turn state in creation order, including turns
// that were queued before a runtime restart.
func (s *Store) SecretaryTurns(ctx context.Context, identityID string) ([]SecretaryTurn, error) {
	rows, err := s.db.QueryContext(ctx, secretaryTurnSelect+` WHERE identity_id = ? ORDER BY created_at, id`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := make([]SecretaryTurn, 0)
	for rows.Next() {
		var turn SecretaryTurn
		if err := scanSecretaryTurn(rows, &turn); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

// ReconstructSecretaryContext reads only durable server-owned state. It is a
// pure read, so callers can inspect a context without accidentally consuming a
// Worker Result. Results are consumed when a Secretary turn starts.
func (s *Store) ReconstructSecretaryContext(ctx context.Context, identityID, userPath string, recentLimit int) (SecretaryContext, error) {
	identity, err := s.secretaryIdentityByID(ctx, identityID)
	if err != nil {
		return SecretaryContext{}, err
	}
	user, err := s.LoadUserDocument(ctx, userPath)
	if err != nil {
		return SecretaryContext{}, err
	}
	summary, err := s.SecretaryConversationSummary(ctx, identity.ConversationID)
	if err != nil {
		return SecretaryContext{}, err
	}
	entries, err := s.EntriesAfter(ctx, identity.ConversationID, 0)
	if err != nil {
		return SecretaryContext{}, err
	}
	if recentLimit <= 0 {
		recentLimit = 20
	}
	if recentLimit > 1000 {
		recentLimit = 1000
	}
	if len(entries) > recentLimit {
		entries = entries[len(entries)-recentLimit:]
	}
	results, err := s.unseenWorkerResults(ctx, identity.ConversationID)
	if err != nil {
		return SecretaryContext{}, err
	}
	workers, err := s.WorkersForConversation(ctx, identity.ConversationID)
	if err != nil {
		return SecretaryContext{}, err
	}
	openWorkers := make([]Worker, 0, len(workers))
	for _, worker := range workers {
		if !worker.Archived && worker.Status != WorkerClosed {
			openWorkers = append(openWorkers, worker)
		}
	}
	projects, err := s.Projects(ctx)
	if err != nil {
		return SecretaryContext{}, err
	}
	records, err := s.NodeRecords(ctx)
	if err != nil {
		return SecretaryContext{}, err
	}
	nodes := make([]SecretaryNodeSnapshot, 0, len(records))
	harnessInstances := make([]HarnessInstance, 0)
	for _, record := range records {
		nodes = append(nodes, SecretaryNodeSnapshot{
			Node: record.Node, Online: record.Online, Draining: record.Draining, Revoked: record.Revoked,
			EnrolledAt: record.EnrolledAt, LastSeenAt: record.LastSeenAt, LastHeartbeatAt: record.LastHeartbeatAt,
			Capacity: record.Capacity, ActiveAttempts: append([]NodeActiveAttempt(nil), record.ActiveAttempts...),
			LastProcessedCommand: record.LastProcessedCommand, Inventory: record.Inventory,
		})
		harnessInstances = append(harnessInstances, record.Inventory.Instances...)
	}
	sortHarnessInstances(harnessInstances)
	approvals, err := s.Approvals(ctx)
	if err != nil {
		return SecretaryContext{}, err
	}
	activeApprovals := make([]Approval, 0, len(approvals))
	for _, approval := range approvals {
		if approval.State == ApprovalPending {
			activeApprovals = append(activeApprovals, approval)
		}
	}
	policy, err := s.SecretaryPolicySnapshot(ctx)
	if errors.Is(err, ErrNotFound) {
		policy = SecretaryPolicySnapshot{Harness: identity.RuntimeHarness, Model: identity.RuntimeModel, Reasoning: identity.RuntimeReasoning}
	} else if err != nil {
		return SecretaryContext{}, err
	}
	if policy.Harness == "" {
		policy.Harness = identity.RuntimeHarness
	}
	if policy.Model == "" {
		policy.Model = identity.RuntimeModel
	}
	if policy.Reasoning == "" {
		policy.Reasoning = identity.RuntimeReasoning
	}
	return SecretaryContext{
		Identity: identity, UserDocument: user, ConversationSummary: summary, RecentEntries: entries,
		UnseenWorkerResults: results, OpenWorkers: openWorkers, Projects: projects, Nodes: nodes,
		HarnessInstances: harnessInstances, ActiveApprovals: activeApprovals, PolicyProfile: policy,
	}, nil
}

// ReconstructSecretaryContextForTurn materializes canonical context exactly
// once for the next eligible queued turn. Snapshot publication and durable
// Result ownership happen in one transaction.
func (s *Store) ReconstructSecretaryContextForTurn(ctx context.Context, turnID, userPath string, recentLimit int) (SecretaryContext, error) {
	turn, err := s.SecretaryTurn(ctx, turnID)
	if err != nil {
		return SecretaryContext{}, err
	}
	if strings.TrimSpace(turn.ContextSnapshot) != "" {
		return decodeSecretaryContextSnapshot(turn.ContextSnapshot)
	}
	if turn.State != SecretaryTurnQueued {
		return SecretaryContext{}, errors.New("core: canonical Secretary context snapshot is unavailable for non-queued turn")
	}
	canonical, err := s.ReconstructSecretaryContext(ctx, turn.IdentityID, userPath, recentLimit)
	if err != nil {
		return SecretaryContext{}, err
	}
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryContext, error) {
		var current SecretaryTurn
		if err := scanSecretaryTurn(tx.QueryRowContext(ctx, secretaryTurnSelect+` WHERE id = ?`, turn.ID), &current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return SecretaryContext{}, ErrNotFound
			}
			return SecretaryContext{}, err
		}
		if strings.TrimSpace(current.ContextSnapshot) != "" {
			return decodeSecretaryContextSnapshot(current.ContextSnapshot)
		}
		if current.State != SecretaryTurnQueued {
			return SecretaryContext{}, errors.New("core: canonical Secretary context snapshot is unavailable for non-queued turn")
		}
		eligible, err := secretaryTurnEligibleTx(ctx, tx, current)
		if err != nil {
			return SecretaryContext{}, err
		}
		if !eligible {
			return SecretaryContext{}, ErrInvalidTransition
		}
		results, err := unseenWorkerResultsQuery(ctx, tx, current.ConversationID)
		if err != nil {
			return SecretaryContext{}, err
		}
		canonical.UnseenWorkerResults, err = claimSecretaryResultsTx(ctx, tx, current.ID, results, s.now())
		if err != nil {
			return SecretaryContext{}, err
		}
		encoded, err := json.Marshal(canonical)
		if err != nil {
			return SecretaryContext{}, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE secretary_turns SET context_snapshot = ?, updated_at = ? WHERE id = ? AND state = ? AND context_snapshot = ''`, string(encoded), timestamp(s.now()), current.ID, SecretaryTurnQueued)
		if err != nil {
			return SecretaryContext{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return SecretaryContext{}, err
		}
		if affected == 1 {
			return canonical, nil
		}
		var snapshot string
		if err := tx.QueryRowContext(ctx, `SELECT context_snapshot FROM secretary_turns WHERE id = ?`, current.ID).Scan(&snapshot); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return SecretaryContext{}, ErrNotFound
			}
			return SecretaryContext{}, err
		}
		if strings.TrimSpace(snapshot) == "" {
			return SecretaryContext{}, errors.New("core: canonical Secretary context snapshot is unavailable for turn")
		}
		return decodeSecretaryContextSnapshot(snapshot)
	})
}

func decodeSecretaryContextSnapshot(encoded string) (SecretaryContext, error) {
	if strings.TrimSpace(encoded) == "" {
		return SecretaryContext{}, errors.New("core: canonical Secretary context snapshot is empty")
	}
	var canonical SecretaryContext
	if err := json.Unmarshal([]byte(encoded), &canonical); err != nil {
		return SecretaryContext{}, fmt.Errorf("core: decode canonical Secretary context snapshot: %w", err)
	}
	if err := canonical.Validate(); err != nil {
		return SecretaryContext{}, err
	}
	return canonical, nil
}

func (s *Store) secretaryIdentityByID(ctx context.Context, identityID string) (SecretaryIdentity, error) {
	var identity SecretaryIdentity
	err := scanSecretaryIdentity(s.db.QueryRowContext(ctx, `SELECT id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at FROM secretary_identities WHERE id = ?`, identityID), &identity)
	if errors.Is(err, sql.ErrNoRows) {
		return SecretaryIdentity{}, ErrNotFound
	}
	return identity, err
}

type contextQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Store) unseenWorkerResults(ctx context.Context, conversationID string) ([]Phase4Result, error) {
	return unseenWorkerResultsQuery(ctx, s.db, conversationID)
}

func unseenWorkerResultsQuery(ctx context.Context, queryer contextQueryer, conversationID string) ([]Phase4Result, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT r.id, r.worker_id, r.turn_id, r.attempt_id, r.status, r.summary, r.failure_code, r.artifact_refs, r.correlation_id, r.created_at
FROM phase4_results r JOIN workers w ON w.id = r.worker_id
LEFT JOIN secretary_context_seen_results seen ON seen.result_id = r.id
WHERE w.conversation_id = ? AND seen.result_id IS NULL ORDER BY r.created_at, r.id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]Phase4Result, 0)
	for rows.Next() {
		var result Phase4Result
		if err := rows.Scan(&result.ID, &result.WorkerID, &result.TurnID, &result.AttemptID, &result.Status, &result.Summary, &result.FailureCode, &result.ArtifactRefs, &result.CorrelationID, newTimestampScanner(&result.CreatedAt)); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func secretaryTurnEligibleTx(ctx context.Context, tx *sql.Tx, turn SecretaryTurn) (bool, error) {
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_turns WHERE identity_id = ? AND state = 'active'`, turn.IdentityID).Scan(&active); err != nil {
		return false, err
	}
	if active != 0 {
		return false, nil
	}
	var earlier int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_turns WHERE identity_id = ? AND state = 'queued' AND queue_position < ?`, turn.IdentityID, turn.QueuePosition).Scan(&earlier); err != nil {
		return false, err
	}
	return earlier == 0, nil
}

func claimSecretaryResultsTx(ctx context.Context, tx *sql.Tx, turnID string, results []Phase4Result, claimedAt time.Time) ([]Phase4Result, error) {
	claimed := make([]Phase4Result, 0, len(results))
	for _, result := range results {
		inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO secretary_context_seen_results(turn_id, result_id, seen_at) VALUES(?, ?, ?)`, turnID, result.ID, timestamp(claimedAt))
		if err != nil {
			return nil, err
		}
		affected, err := inserted.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 1 {
			claimed = append(claimed, result)
		}
	}
	return claimed, nil
}

// SecretaryContextPrompt is the only runtime-facing conversion. It serializes
// the canonical read model and never adds native session state.
func SecretaryContextPrompt(ctx SecretaryContext, input string) (string, error) {
	encoded, err := json.Marshal(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Canonical server-owned Secretary context:\n%s\n\nUser turn:\n%s", encoded, input), nil
}

func sortHarnessInstances(instances []HarnessInstance) {
	sort.SliceStable(instances, func(i, j int) bool {
		if instances[i].Node != instances[j].Node {
			return instances[i].Node < instances[j].Node
		}
		return instances[i].ID < instances[j].ID
	})
}
