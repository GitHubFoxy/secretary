package core

import (
	"errors"
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

// Validate rejects syntactically valid JSON that is not a canonical context.
func (c SecretaryContext) Validate() error {
	if strings.TrimSpace(c.Identity.ID) == "" || strings.TrimSpace(c.Identity.ConversationID) == "" {
		return errors.New("core: canonical Secretary context snapshot is missing identity")
	}
	return nil
}
