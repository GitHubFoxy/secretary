package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// SecretaryIdentity is the durable product identity. Runtime sessions are
// replaceable and are represented only by the generation and selected policy.
type SecretaryIdentity struct {
	ID                string    `json:"id"`
	PersonID          string    `json:"person_id"`
	ConversationID    string    `json:"conversation_id"`
	RuntimeGeneration int64     `json:"runtime_generation"`
	RuntimeHarness    string    `json:"runtime_harness"`
	RuntimeModel      string    `json:"runtime_model"`
	RuntimeReasoning  string    `json:"runtime_reasoning"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SecretaryTurnState string

const (
	secretaryPromptPending  = "pending"
	secretaryPromptStarted  = "started"
	secretaryPromptAccepted = "accepted"

	SecretaryTurnQueued      SecretaryTurnState = "queued"
	SecretaryTurnActive      SecretaryTurnState = "active"
	SecretaryTurnSucceeded   SecretaryTurnState = "succeeded"
	SecretaryTurnFailed      SecretaryTurnState = "failed"
	SecretaryTurnCanceled    SecretaryTurnState = "canceled"
	SecretaryTurnInterrupted SecretaryTurnState = "interrupted"
)

func (s SecretaryTurnState) Terminal() bool {
	return s == SecretaryTurnSucceeded || s == SecretaryTurnFailed || s == SecretaryTurnCanceled || s == SecretaryTurnInterrupted
}

type SecretaryTurn struct {
	ID              string             `json:"id"`
	IdentityID      string             `json:"identity_id"`
	ConversationID  string             `json:"conversation_id"`
	Input           string             `json:"input"`
	ContextSnapshot string             `json:"-"`
	PromptState     string             `json:"-"`
	State           SecretaryTurnState `json:"state"`
	QueuePosition   int64              `json:"queue_position"`
	Error           string             `json:"error,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	StartedAt       *time.Time         `json:"started_at,omitempty"`
	FinishedAt      *time.Time         `json:"finished_at,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type SecretaryEventInput struct {
	TurnID         string
	IdempotencyKey string
	Kind           string
	Text           string
	Summary        string
	Tool           string
	Arguments      string
	Result         string
	Status         string
	Error          string
	Payload        any
}

const (
	SecretaryTurnQueuedEvent      = "secretary.turn.queued"
	SecretaryTurnStartedEvent     = "secretary.turn.started"
	SecretaryTextDeltaEvent       = "secretary.text_delta"
	SecretaryThinkingSummaryEvent = "secretary.thinking_summary"
	SecretaryToolCallEvent        = "secretary.tool_call"
	SecretaryToolResultEvent      = "secretary.tool_result"
	SecretaryTurnFinishedEvent    = "secretary.turn.finished"
)

// UserDocument is the active external Markdown snapshot.
type UserDocument struct {
	Path      string    `json:"path"`
	Revision  int64     `json:"revision"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SecretaryPolicySnapshot is the durable, non-runtime policy/profile view used
// when a Secretary turn is reconstructed. It intentionally has no session or
// credential fields.
type SecretaryPolicySnapshot struct {
	Version          string    `json:"version,omitempty"`
	Harness          string    `json:"harness,omitempty"`
	Model            string    `json:"model,omitempty"`
	Reasoning        string    `json:"reasoning,omitempty"`
	ProfileVersion   string    `json:"profile_version,omitempty"`
	ProfileName      string    `json:"profile_name,omitempty"`
	ProfileHash      string    `json:"profile_hash,omitempty"`
	ProfileContent   string    `json:"profile_content,omitempty"`
	ProfileRuntime   string    `json:"profile_runtime,omitempty"`
	ProfileModel     string    `json:"profile_model,omitempty"`
	ProfileReasoning string    `json:"profile_reasoning,omitempty"`
	ProfileDelivery  string    `json:"profile_delivery,omitempty"`
	AllowedTools     []string  `json:"allowed_tools,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// SecretaryNodeSnapshot is a safe read model. Node credentials and hashes are
// not part of Secretary context, even though they exist in NodeRecord.
type SecretaryNodeSnapshot struct {
	Node                 NodeReference            `json:"node"`
	Online               bool                     `json:"online"`
	Draining             bool                     `json:"draining"`
	Revoked              bool                     `json:"revoked"`
	EnrolledAt           time.Time                `json:"enrolled_at"`
	LastSeenAt           time.Time                `json:"last_seen_at,omitempty"`
	LastHeartbeatAt      time.Time                `json:"last_heartbeat_at,omitempty"`
	Capacity             int                      `json:"capacity"`
	ActiveAttempts       []NodeActiveAttempt      `json:"active_attempts,omitempty"`
	LastProcessedCommand string                   `json:"last_processed_command,omitempty"`
	Inventory            HarnessInventorySnapshot `json:"inventory,omitempty"`
}

// SecretaryContext is reconstructed from durable server-owned sources for one
// new Secretary turn. Native runtime sessions are deliberately absent.
type SecretaryContext struct {
	Identity            SecretaryIdentity       `json:"identity"`
	UserDocument        UserDocument            `json:"user_document"`
	ConversationSummary string                  `json:"conversation_summary,omitempty"`
	RecentEntries       []ConversationEntry     `json:"recent_entries"`
	UnseenWorkerResults []Phase4Result          `json:"unseen_worker_results"`
	OpenWorkers         []Worker                `json:"open_workers"`
	Projects            []Project               `json:"projects"`
	Nodes               []SecretaryNodeSnapshot `json:"nodes"`
	HarnessInstances    []HarnessInstance       `json:"harness_instances"`
	ActiveApprovals     []Approval              `json:"active_approvals"`
	PolicyProfile       SecretaryPolicySnapshot `json:"policy_profile"`
}

// UnmarshalJSON rejects fields outside the typed canonical context. This keeps
// runtime sessions, credentials and capabilities out of the model prompt even
// when a snapshot was tampered with after it was persisted.
func (c *SecretaryContext) UnmarshalJSON(data []byte) error {
	type plain SecretaryContext
	var decoded plain
	if err := decodeStrictJSON(data, &decoded); err != nil {
		return err
	}
	*c = SecretaryContext(decoded)
	return nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("core: canonical Secretary context snapshot has trailing data")
		}
		return err
	}
	return nil
}

// Validate rejects syntactically valid JSON that is not a canonical context.
func (c SecretaryContext) Validate() error {
	if strings.TrimSpace(c.Identity.ID) == "" || strings.TrimSpace(c.Identity.PersonID) == "" || strings.TrimSpace(c.Identity.ConversationID) == "" {
		return errors.New("core: canonical Secretary context snapshot is missing complete identity")
	}
	if c.Identity.RuntimeGeneration < 0 || c.Identity.CreatedAt.IsZero() || c.Identity.UpdatedAt.IsZero() {
		return errors.New("core: canonical Secretary context snapshot has invalid identity metadata")
	}

	user := c.UserDocument
	if strings.TrimSpace(user.Path) == "" || !filepath.IsAbs(user.Path) || filepath.Clean(user.Path) != user.Path {
		return errors.New("core: canonical Secretary context snapshot has invalid user document path")
	}
	if user.Revision < 1 || user.UpdatedAt.IsZero() {
		return errors.New("core: canonical Secretary context snapshot has invalid user document metadata")
	}
	if err := ValidateUserDocument(user.Content); err != nil {
		return fmt.Errorf("core: canonical Secretary context snapshot has invalid user document: %w", err)
	}

	policy := c.PolicyProfile
	for name, value := range map[string]string{
		"version": policy.Version, "harness": policy.Harness, "model": policy.Model,
		"reasoning": policy.Reasoning, "profile_version": policy.ProfileVersion,
		"profile_name": policy.ProfileName, "profile_hash": policy.ProfileHash,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("core: canonical Secretary context snapshot is missing policy profile %s", name)
		}
	}
	if policy.UpdatedAt.IsZero() || strings.TrimSpace(policy.ProfileContent) == "" {
		return errors.New("core: canonical Secretary context snapshot has incomplete policy profile")
	}

	if c.UnseenWorkerResults == nil || c.OpenWorkers == nil || c.Projects == nil || c.Nodes == nil || c.HarnessInstances == nil || c.ActiveApprovals == nil {
		return errors.New("core: canonical Secretary context snapshot is missing required collections")
	}
	for i, entry := range c.RecentEntries {
		if strings.TrimSpace(entry.ID) == "" || entry.ConversationID != c.Identity.ConversationID || entry.Seq < 1 || entry.CreatedAt.IsZero() {
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid recent entry %d", i)
		}
		switch entry.Kind {
		case EntryUser, EntrySecretary, EntryWorkerInput, EntryWorkerResult, EntrySystem:
		default:
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid recent entry kind %q", entry.Kind)
		}
		if i > 0 && c.RecentEntries[i-1].Seq >= entry.Seq {
			return errors.New("core: canonical Secretary context snapshot recent entries are not ordered")
		}
	}

	projects := make(map[string]struct{}, len(c.Projects))
	for i, project := range c.Projects {
		if err := project.Validate(); err != nil {
			return fmt.Errorf("core: canonical Secretary context snapshot project %d: %w", i, err)
		}
		if _, exists := projects[project.ID]; exists {
			return fmt.Errorf("core: canonical Secretary context snapshot has duplicate project %q", project.ID)
		}
		projects[project.ID] = struct{}{}
	}

	nodes := make(map[NodeReference]struct{}, len(c.Nodes))
	for i, node := range c.Nodes {
		if strings.TrimSpace(string(node.Node)) == "" || node.EnrolledAt.IsZero() || node.Capacity < 0 {
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid Node %d", i)
		}
		if _, exists := nodes[node.Node]; exists {
			return fmt.Errorf("core: canonical Secretary context snapshot has duplicate Node %q", node.Node)
		}
		nodes[node.Node] = struct{}{}
		for _, active := range node.ActiveAttempts {
			if strings.TrimSpace(active.WorkerRef) == "" || strings.TrimSpace(active.TurnID) == "" || strings.TrimSpace(active.AttemptID) == "" {
				return fmt.Errorf("core: canonical Secretary context snapshot has invalid active Attempt reference")
			}
		}
		if node.Inventory.Node != "" || node.Inventory.Instances != nil || !node.Inventory.ObservedAt.IsZero() {
			if node.Inventory.Node != node.Node {
				return fmt.Errorf("core: canonical Secretary context snapshot Node inventory mismatch for %q", node.Node)
			}
			if err := node.Inventory.Validate(); err != nil {
				return fmt.Errorf("core: canonical Secretary context snapshot Node inventory: %w", err)
			}
		}
	}

	harnesses := make(map[HarnessInstanceID]HarnessInstance, len(c.HarnessInstances))
	for i, instance := range c.HarnessInstances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("core: canonical Secretary context snapshot HarnessInstance %d: %w", i, err)
		}
		if _, exists := harnesses[instance.ID]; exists {
			return fmt.Errorf("core: canonical Secretary context snapshot has duplicate HarnessInstance %q", instance.ID)
		}
		if _, exists := nodes[instance.Node]; !exists {
			return fmt.Errorf("core: canonical Secretary context snapshot HarnessInstance %q references unknown Node %q", instance.ID, instance.Node)
		}
		harnesses[instance.ID] = instance
	}
	for _, node := range c.Nodes {
		for _, inventoryInstance := range node.Inventory.Instances {
			instance, exists := harnesses[inventoryInstance.ID]
			if !exists || instance.Node != node.Node {
				return fmt.Errorf("core: canonical Secretary context snapshot Node %q has an invalid inventory HarnessInstance reference", node.Node)
			}
		}
	}

	workers := make(map[string]Worker, len(c.OpenWorkers))
	for i, worker := range c.OpenWorkers {
		if strings.TrimSpace(worker.ID) == "" || strings.TrimSpace(worker.WorkerRef) == "" || strings.TrimSpace(worker.Intent) == "" || strings.TrimSpace(worker.ProjectID) == "" || strings.TrimSpace(worker.NodeID) == "" || strings.TrimSpace(worker.HarnessInstanceID) == "" || strings.TrimSpace(worker.PolicySnapshot) == "" || worker.CreatedAt.IsZero() || worker.UpdatedAt.IsZero() {
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid Worker %d", i)
		}
		if !validWorkerStatus(worker.Status) || worker.Archived || worker.Status == WorkerClosed {
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid Worker status %q", worker.Status)
		}
		if strings.TrimSpace(worker.ProjectSnapshot) != "" {
			var projectSnapshot ProjectSnapshot
			if err := decodeStrictJSON([]byte(worker.ProjectSnapshot), &projectSnapshot); err != nil {
				return fmt.Errorf("core: canonical Secretary context snapshot Worker %q has invalid Project snapshot: %w", worker.WorkerRef, err)
			}
			if err := projectSnapshot.Validate(); err != nil {
				return fmt.Errorf("core: canonical Secretary context snapshot Worker %q has invalid Project snapshot: %w", worker.WorkerRef, err)
			}
		}
		if len(projects) > 0 {
			if _, exists := projects[worker.ProjectID]; !exists {
				return fmt.Errorf("core: canonical Secretary context snapshot Worker %q references unknown Project %q", worker.WorkerRef, worker.ProjectID)
			}
		}
		if len(nodes) > 0 {
			if _, exists := nodes[NodeReference(worker.NodeID)]; !exists {
				return fmt.Errorf("core: canonical Secretary context snapshot Worker %q references unknown Node %q", worker.WorkerRef, worker.NodeID)
			}
		}
		if len(harnesses) > 0 {
			instance, exists := harnesses[HarnessInstanceID(worker.HarnessInstanceID)]
			if !exists || instance.Node != NodeReference(worker.NodeID) {
				return fmt.Errorf("core: canonical Secretary context snapshot Worker %q has invalid HarnessInstance reference", worker.WorkerRef)
			}
		}
		if _, exists := workers[worker.ID]; exists {
			return fmt.Errorf("core: canonical Secretary context snapshot has duplicate Worker %q", worker.ID)
		}
		workers[worker.ID] = worker
	}

	resultIDs := make(map[string]struct{}, len(c.UnseenWorkerResults))
	for i, result := range c.UnseenWorkerResults {
		if strings.TrimSpace(result.ID) == "" || strings.TrimSpace(result.WorkerID) == "" || strings.TrimSpace(result.TurnID) == "" || strings.TrimSpace(result.AttemptID) == "" || strings.TrimSpace(result.Summary) == "" || result.CreatedAt.IsZero() || !validResultStatus(result.Status) {
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid Result %d", i)
		}
		if _, exists := resultIDs[result.ID]; exists {
			return fmt.Errorf("core: canonical Secretary context snapshot has duplicate Result %q", result.ID)
		}
		resultIDs[result.ID] = struct{}{}
	}
	approvalIDs := make(map[string]struct{}, len(c.ActiveApprovals))
	for i, approval := range c.ActiveApprovals {
		if strings.TrimSpace(approval.ID) == "" || strings.TrimSpace(approval.RequestID) == "" || strings.TrimSpace(approval.WorkerID) == "" || strings.TrimSpace(approval.TurnID) == "" || strings.TrimSpace(approval.AttemptID) == "" || strings.TrimSpace(approval.NodeID) == "" || strings.TrimSpace(approval.ProjectID) == "" || strings.TrimSpace(approval.ActionSummary) == "" || approval.RequestedAt.IsZero() || !validApprovalKind(approval.Kind) || approval.State != ApprovalPending {
			return fmt.Errorf("core: canonical Secretary context snapshot has invalid Approval %d", i)
		}
		if _, exists := approvalIDs[approval.ID]; exists {
			return fmt.Errorf("core: canonical Secretary context snapshot has duplicate Approval %q", approval.ID)
		}
		approvalIDs[approval.ID] = struct{}{}
		worker, exists := workers[approval.WorkerID]
		if !exists || worker.NodeID != approval.NodeID || worker.ProjectID != approval.ProjectID {
			return fmt.Errorf("core: canonical Secretary context snapshot Approval %q has invalid Worker reference", approval.RequestID)
		}
		if _, exists := nodes[NodeReference(approval.NodeID)]; !exists {
			return fmt.Errorf("core: canonical Secretary context snapshot Approval %q references unknown Node", approval.RequestID)
		}
		if _, exists := projects[approval.ProjectID]; !exists {
			return fmt.Errorf("core: canonical Secretary context snapshot Approval %q references unknown Project", approval.RequestID)
		}
	}
	return nil
}

func validWorkerStatus(status WorkerStatus) bool {
	switch status {
	case WorkerQueued, WorkerStarting, WorkerWorking, WorkerWaitingApproval, WorkerNeedsInput, WorkerOffline, WorkerIdle, WorkerClosed:
		return true
	default:
		return false
	}
}

func validResultStatus(status ResultStatus) bool {
	switch status {
	case ResultSucceeded, ResultFailed, ResultCanceled, ResultInterrupted:
		return true
	default:
		return false
	}
}

func validApprovalKind(kind ApprovalKind) bool {
	return kind == ApprovalPermission || kind == ApprovalInput
}
