package core

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type WorkerStatus string

const (
	WorkerQueued          WorkerStatus = "queued"
	WorkerStarting        WorkerStatus = "starting"
	WorkerWorking         WorkerStatus = "working"
	WorkerWaitingApproval WorkerStatus = "waiting_approval"
	WorkerNeedsInput      WorkerStatus = "needs_input"
	WorkerLimited         WorkerStatus = "limited"
	WorkerOffline         WorkerStatus = "offline"
	WorkerIdle            WorkerStatus = "idle"
	WorkerClosed          WorkerStatus = "closed"
)

type TurnState string

const (
	TurnQueued          TurnState = "queued"
	TurnStarting        TurnState = "starting"
	TurnActive          TurnState = "active"
	TurnWaitingApproval TurnState = "waiting_approval"
	TurnNeedsInput      TurnState = "needs_input"
	TurnSuspended       TurnState = "suspended"
	TurnSucceeded       TurnState = "succeeded"
	TurnFailed          TurnState = "failed"
	TurnCanceled        TurnState = "canceled"
	TurnInterrupted     TurnState = "interrupted"
)

func (s TurnState) Terminal() bool {
	return s == TurnSucceeded || s == TurnFailed || s == TurnCanceled || s == TurnInterrupted
}

func (s TurnState) Active() bool {
	return s == TurnQueued || s == TurnStarting || s == TurnActive || s == TurnWaitingApproval || s == TurnNeedsInput || s == TurnSuspended
}

type AttemptOutcomeStatus string

const (
	OutcomeSucceeded   AttemptOutcomeStatus = "succeeded"
	OutcomeFailed      AttemptOutcomeStatus = "failed"
	OutcomeCanceled    AttemptOutcomeStatus = "canceled"
	OutcomeInterrupted AttemptOutcomeStatus = "interrupted"
)

func (s AttemptOutcomeStatus) Valid() bool {
	return s == OutcomeSucceeded || s == OutcomeFailed || s == OutcomeCanceled || s == OutcomeInterrupted
}

type OutcomeClassification string

const (
	OutcomeRetryable OutcomeClassification = "retryable"
	OutcomeFinal     OutcomeClassification = "final"
)

func (c OutcomeClassification) Valid() bool {
	return c == OutcomeRetryable || c == OutcomeFinal
}

type FailureCategory string

const (
	FailureCategoryUsageLimit     FailureCategory = "usage_limit"
	FailureCategoryAuthRequired   FailureCategory = "auth_required"
	FailureCategoryBillingCredits FailureCategory = "billing_credits"
	FailureCategoryRateLimit      FailureCategory = "rate_limit"
	FailureCategoryInternalError  FailureCategory = "internal_error"
)

func (c FailureCategory) Valid() bool {
	switch c {
	case FailureCategoryUsageLimit, FailureCategoryAuthRequired, FailureCategoryBillingCredits, FailureCategoryRateLimit, FailureCategoryInternalError:
		return true
	default:
		return false
	}
}

var workerFailureProviderCodes = map[string]string{
	"usage_limit_exceeded":    "usage_limit_exceeded",
	"usagelimitexceeded":      "usage_limit_exceeded",
	"auth_required":           "auth_required",
	"invalid_api_key":         "invalid_api_key",
	"token_expired":           "token_expired",
	"unauthorized":            "unauthorized",
	"authentication_required": "authentication_required",
	"insufficient_quota":      "insufficient_quota",
	"billing_credits":         "billing_credits",
	"rate_limit":              "rate_limit",
	"rate_limit_exceeded":     "rate_limit_exceeded",
	"too_many_requests":       "too_many_requests",
}

func SanitizeFailureProviderCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || utf8.RuneCountInString(code) > 1000 {
		return ""
	}
	return workerFailureProviderCodes[strings.ToLower(code)]
}

type WorkerFailure struct {
	Category     FailureCategory `json:"category"`
	ProviderCode string          `json:"provider_code,omitempty"`
	Message      string          `json:"message"`
	Technical    string          `json:"technical,omitempty"`
}

type Worker struct {
	ID                     string       `json:"id"`
	WorkerRef              string       `json:"worker_ref"`
	Title                  string       `json:"title"`
	Intent                 string       `json:"intent"`
	ProjectID              string       `json:"project_id"`
	NodeID                 string       `json:"node_id"`
	HarnessInstanceID      string       `json:"harness_instance_id"`
	PolicySnapshot         string       `json:"policy_snapshot"`
	ProjectSnapshot        string       `json:"project_snapshot,omitempty"`
	ProfileSnapshot        string       `json:"-"`
	WorkerTemplateRequired bool         `json:"-"`
	Workspace              string       `json:"workspace,omitempty"`
	Status                 WorkerStatus `json:"status"`
	CurrentTurnID          string       `json:"current_turn_id,omitempty"`
	LastResultSummary      string       `json:"last_result_summary,omitempty"`
	CreatedAt              time.Time    `json:"created_at"`
	UpdatedAt              time.Time    `json:"updated_at"`
	ClosedAt               *time.Time   `json:"closed_at,omitempty"`
	Archived               bool         `json:"archived"`
}

type Turn struct {
	PreviousAttemptID string    `json:"-"`
	ID                string    `json:"id"`
	WorkerID          string    `json:"worker_id"`
	Input             string    `json:"input"`
	NormalizedIntent  string    `json:"normalized_intent,omitempty"`
	ContextSnapshot   string    `json:"context_snapshot,omitempty"`
	State             TurnState `json:"state"`
	CurrentAttemptID  string    `json:"current_attempt_id,omitempty"`
	ResultID          string    `json:"result_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Phase4Attempt = Attempt

type AttemptOutcome struct {
	ID              string                `json:"id"`
	AttemptID       string                `json:"attempt_id"`
	Status          AttemptOutcomeStatus  `json:"status"`
	Classification  OutcomeClassification `json:"classification"`
	ErrorCode       string                `json:"error_code,omitempty"`
	ErrorMessage    string                `json:"error_message,omitempty"`
	Diagnostics     string                `json:"diagnostics,omitempty"`
	FailureCategory FailureCategory       `json:"failure_category,omitempty"`
	CreatedAt       time.Time             `json:"created_at"`
}

type Phase4Result = Result

type ApprovalKind string

const (
	ApprovalPermission ApprovalKind = "permission"
	ApprovalInput      ApprovalKind = "input"
)

type ApprovalState string

const (
	ApprovalPending   ApprovalState = "pending"
	ApprovalResolving ApprovalState = "resolving"
	ApprovalApproved  ApprovalState = "approved"
	ApprovalDenied    ApprovalState = "denied"
	ApprovalExpired   ApprovalState = "expired"
	ApprovalRevoked   ApprovalState = "revoked"
)

type Approval struct {
	ID            string        `json:"id"`
	RequestID     string        `json:"request_id"`
	WorkerID      string        `json:"worker_id"`
	TurnID        string        `json:"turn_id"`
	AttemptID     string        `json:"attempt_id"`
	NodeID        string        `json:"node_id"`
	ProjectID     string        `json:"project_id"`
	Kind          ApprovalKind  `json:"kind"`
	ActionSummary string        `json:"action_summary"`
	RiskCategory  string        `json:"risk_category,omitempty"`
	RequestedAt   time.Time     `json:"requested_at"`
	ExpiresAt     *time.Time    `json:"expires_at,omitempty"`
	State         ApprovalState `json:"state"`
	Response      string        `json:"-"`
	ResolvedBy    string        `json:"resolved_by,omitempty"`
	ResolvedAt    *time.Time    `json:"resolved_at,omitempty"`
	AuditEventID  string        `json:"audit_event_id,omitempty"`

	ResolutionCommandID  string        `json:"-"`
	ResolutionState      ApprovalState `json:"-"`
	ResolutionResponse   string        `json:"-"`
	ResolutionResolvedBy string        `json:"-"`
}

type TrustedLocalApprovalPolicy struct {
	Enabled   bool
	Explicit  bool
	LocalNode bool
	Node      NodeReference
}

type WorkerCommandState string

const (
	WorkerCommandPending   WorkerCommandState = "pending"
	WorkerCommandUncertain WorkerCommandState = "uncertain"
	WorkerCommandDelivered WorkerCommandState = "delivered"
	WorkerCommandFailed    WorkerCommandState = "failed"
)

type WorkerCommandReceiptState string

const (
	WorkerCommandReceiptAccepted  WorkerCommandReceiptState = "accepted"
	WorkerCommandReceiptDenied    WorkerCommandReceiptState = "denied"
	WorkerCommandReceiptUncertain WorkerCommandReceiptState = "uncertain"
)

type WorkerCommandReceipt struct {
	CommandID       string
	Kind            string
	TurnID          string
	AttemptID       string
	State           WorkerCommandReceiptState
	ErrorCode       string
	ErrorMessage    string
	FailureCategory FailureCategory
	ProviderCode    string
	Technical       string
}

type WorkerCommand struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`
	DedupeKey  string             `json:"dedupe_key"`
	WorkerID   string             `json:"worker_id"`
	AttemptID  string             `json:"attempt_id"`
	State      WorkerCommandState `json:"state"`
	LastError  string             `json:"last_error,omitempty"`
	LeaseUntil time.Time          `json:"lease_until"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

type WorkerDetails struct {
	ActionMode      string                `json:"action_mode,omitempty"`
	ActionMessageID string                `json:"action_message_id,omitempty"`
	QueuedMessages  []QueuedWorkerMessage `json:"queued_messages"`
	ActionTurnID    string                `json:"-"`
	Worker          Worker                `json:"worker"`
	Turns           []Turn                `json:"turns"`
	Attempts        []Phase4Attempt       `json:"attempts"`
	Outcomes        []AttemptOutcome      `json:"outcomes"`
	Results         []Phase4Result        `json:"results"`
	Approvals       []Approval            `json:"approvals,omitempty"`
	ActiveSchedule  *AttemptSchedule      `json:"active_schedule,omitempty"`
}

func (d WorkerDetails) CurrentAttempt() *Phase4Attempt {
	if d.Worker.CurrentTurnID == "" {
		return nil
	}
	for i := range d.Turns {
		if d.Turns[i].ID != d.Worker.CurrentTurnID || d.Turns[i].CurrentAttemptID == "" {
			continue
		}
		for j := range d.Attempts {
			if d.Attempts[j].ID == d.Turns[i].CurrentAttemptID {
				attempt := d.Attempts[j]
				return &attempt
			}
		}
	}
	return nil
}

func (d WorkerDetails) CurrentTurn() *Turn {
	for i := range d.Turns {
		if d.Turns[i].ID == d.Worker.CurrentTurnID {
			turn := d.Turns[i]
			return &turn
		}
	}
	return nil
}

type WorkerSpec struct {
	WorkerRef             string
	Title                 string
	Intent                string
	ProjectID             string
	NodeID                string
	HarnessInstanceID     string
	PolicySnapshot        string
	ProjectSnapshot       string
	ProfileSnapshot       string
	Workspace             string
	ProjectRevision       int64
	ExpectedNodeOnline    bool
	ExpectedNodeDraining  bool
	ExpectedNodeRevoked   bool
	ExpectedInventoryJSON string
	IdempotencyKey        string
}

type TurnSpec struct {
	Input            string
	NormalizedIntent string
	ContextSnapshot  string
	IdempotencyKey   string
	CommandKind      string
}

type AttemptOutcomeInput struct {
	Status          AttemptOutcomeStatus
	Classification  OutcomeClassification
	ErrorCode       string
	ErrorMessage    string
	Diagnostics     string
	Summary         string
	FailureCode     string
	FailureCategory FailureCategory
	ArtifactRefs    string
	ResetsAt        *time.Time
	QuotaScope      string
}

const ContinuationPrompt = "Continue previous work from the point where it was interrupted by usage limit."

type AttemptScheduleState string

const (
	SchedulePending    AttemptScheduleState = "pending"
	ScheduleManualHold AttemptScheduleState = "manual_hold"
	ScheduleClaimed    AttemptScheduleState = "claimed"
	ScheduleCompleted  AttemptScheduleState = "completed"
	ScheduleCanceled   AttemptScheduleState = "canceled"
)

type AttemptSchedule struct {
	ID                 string               `json:"id"`
	WorkerID           string               `json:"worker_id"`
	TurnID             string               `json:"turn_id"`
	SourceAttemptID    string               `json:"source_attempt_id"`
	AttemptID          string               `json:"attempt_id"`
	ScheduledAt        *time.Time           `json:"scheduled_at,omitempty"`
	State              AttemptScheduleState `json:"state"`
	FailureCategory    FailureCategory      `json:"failure_category,omitempty"`
	FailureCode        string               `json:"failure_code,omitempty"`
	FailureMessage     string               `json:"failure_message,omitempty"`
	QuotaScope         string               `json:"quota_scope,omitempty"`
	DeadlineEvidence   string               `json:"deadline_evidence,omitempty"`
	ContinuationPrompt string               `json:"continuation_prompt"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

type FinishAttemptInput struct {
	AttemptOutcomeInput
	RetryCommandID string
}

type FinishAttemptResult struct {
	Outcome     AttemptOutcome
	Result      *Phase4Result
	NextAttempt *Phase4Attempt
	Duplicate   bool
}

type Phase4RecoveryDecision string

const (
	Phase4RecoveryAlive   Phase4RecoveryDecision = "alive"
	Phase4RecoveryUnknown Phase4RecoveryDecision = "unknown"
)

type Phase4AttemptRecoveryResolver interface {
	ResolvePhase4Attempt(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error)
}

type Phase4AttemptRecoveryFunc func(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error)

func (f Phase4AttemptRecoveryFunc) ResolvePhase4Attempt(ctx context.Context, attempt Phase4Attempt) (Phase4RecoveryDecision, error) {
	return f(ctx, attempt)
}
