package core

import (
	"context"
	"time"
)

// WorkerStatus is the server-owned lifecycle of a persistent Worker.
type WorkerStatus string

const (
	WorkerQueued          WorkerStatus = "queued"
	WorkerStarting        WorkerStatus = "starting"
	WorkerWorking         WorkerStatus = "working"
	WorkerWaitingApproval WorkerStatus = "waiting_approval"
	WorkerNeedsInput      WorkerStatus = "needs_input"
	WorkerOffline         WorkerStatus = "offline"
	WorkerIdle            WorkerStatus = "idle"
	WorkerClosed          WorkerStatus = "closed"
)

// TurnState describes one user intent or follow-up within a Worker.
type TurnState string

const (
	TurnQueued          TurnState = "queued"
	TurnStarting        TurnState = "starting"
	TurnActive          TurnState = "active"
	TurnWaitingApproval TurnState = "waiting_approval"
	TurnNeedsInput      TurnState = "needs_input"
	TurnSucceeded       TurnState = "succeeded"
	TurnFailed          TurnState = "failed"
	TurnCanceled        TurnState = "canceled"
	TurnInterrupted     TurnState = "interrupted"
)

func (s TurnState) Terminal() bool {
	return s == TurnSucceeded || s == TurnFailed || s == TurnCanceled || s == TurnInterrupted
}

func (s TurnState) Active() bool {
	return s == TurnQueued || s == TurnStarting || s == TurnActive || s == TurnWaitingApproval || s == TurnNeedsInput
}

// AttemptOutcomeStatus is the terminal observation for an Attempt.
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

// OutcomeClassification controls whether the Turn may be retried internally.
type OutcomeClassification string

const (
	OutcomeRetryable OutcomeClassification = "retryable"
	OutcomeFinal     OutcomeClassification = "final"
)

func (c OutcomeClassification) Valid() bool {
	return c == OutcomeRetryable || c == OutcomeFinal
}

// Worker is the Phase 4 product entity. Its execution binding and policy are
// immutable after creation. Native runtime sessions are deliberately absent.
type Worker struct {
	ID                string       `json:"id"`
	WorkerRef         string       `json:"worker_ref"`
	Title             string       `json:"title"`
	Intent            string       `json:"intent"`
	ProjectID         string       `json:"project_id"`
	NodeID            string       `json:"node_id"`
	HarnessInstanceID string       `json:"harness_instance_id"`
	PolicySnapshot    string       `json:"policy_snapshot"`
	ProjectSnapshot   string       `json:"project_snapshot,omitempty"`
	Workspace         string       `json:"workspace,omitempty"`
	Status            WorkerStatus `json:"status"`
	CurrentTurnID     string       `json:"current_turn_id,omitempty"`
	LastResultSummary string       `json:"last_result_summary,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
	ClosedAt          *time.Time   `json:"closed_at,omitempty"`
	Archived          bool         `json:"archived"`
}

// Turn is a single user direction. Retries stay inside the same Turn.
type Turn struct {
	ID               string    `json:"id"`
	WorkerID         string    `json:"worker_id"`
	Input            string    `json:"input"`
	NormalizedIntent string    `json:"normalized_intent,omitempty"`
	ContextSnapshot  string    `json:"context_snapshot,omitempty"`
	State            TurnState `json:"state"`
	CurrentAttemptID string    `json:"current_attempt_id,omitempty"`
	ResultID         string    `json:"result_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Attempt contains only server lifecycle metadata. It never contains a
// native runtime session identifier. The legacy-compatible Attempt type is
// extended with the same Phase 4 fields.
type Phase4Attempt = Attempt

// AttemptOutcome is stored separately from Result. Intermediate retryable
// outcomes never become Conversation entries.
type AttemptOutcome struct {
	ID             string                `json:"id"`
	AttemptID      string                `json:"attempt_id"`
	Status         AttemptOutcomeStatus  `json:"status"`
	Classification OutcomeClassification `json:"classification"`
	ErrorCode      string                `json:"error_code,omitempty"`
	ErrorMessage   string                `json:"error_message,omitempty"`
	Diagnostics    string                `json:"diagnostics,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
}

// Result is the one user-visible terminal outcome for a Turn.
// AttemptID identifies the final Attempt that produced it, not its identity.
// It does not contain a native runtime session ID.
type Phase4Result = Result

// WorkerSpec is the creation contract for a Worker and its immutable binding.
// WorkerDetails is the durable Secretary read model. It deliberately exposes
// immutable bindings and lifecycle records, never Node-local session IDs.
type WorkerCommandState string

const (
	WorkerCommandPending   WorkerCommandState = "pending"
	WorkerCommandDelivered WorkerCommandState = "delivered"
	WorkerCommandFailed    WorkerCommandState = "failed"
)

// WorkerCommand persists the server-side delivery decision for one immutable
// Worker Attempt command. It contains no Node-local runtime identifiers.
type WorkerCommand struct {
	ID        string             `json:"id"`
	Kind      string             `json:"kind"`
	DedupeKey string             `json:"dedupe_key"`
	WorkerID  string             `json:"worker_id"`
	AttemptID string             `json:"attempt_id"`
	State     WorkerCommandState `json:"state"`
	LastError string             `json:"last_error,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type WorkerDetails struct {
	Worker   Worker           `json:"worker"`
	Turns    []Turn           `json:"turns"`
	Attempts []Phase4Attempt  `json:"attempts"`
	Outcomes []AttemptOutcome `json:"outcomes"`
	Results  []Phase4Result   `json:"results"`
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
	WorkerRef         string
	Title             string
	Intent            string
	ProjectID         string
	NodeID            string
	HarnessInstanceID string
	PolicySnapshot    string
	ProjectSnapshot   string
	Workspace         string
	ProjectRevision   int64
	// Expected Node fields are set only by ResolveAndCreateWorker. They bind
	// creation to exactly the observed Node state and inventory snapshot.
	ExpectedNodeOnline    bool
	ExpectedNodeDraining  bool
	ExpectedNodeRevoked   bool
	ExpectedInventoryJSON string
	// IdempotencyKey makes Worker, first Turn, and first Attempt creation one
	// durable operation. An empty key preserves the legacy non-idempotent API.
	IdempotencyKey string
}

// TurnSpec contains the durable input for a new Turn.
type TurnSpec struct {
	Input            string
	NormalizedIntent string
	ContextSnapshot  string
	// IdempotencyKey makes follow-up Turn and Attempt creation one durable operation.
	IdempotencyKey string
}

// AttemptOutcomeInput is the only input accepted by terminal event handling.
type AttemptOutcomeInput struct {
	Status         AttemptOutcomeStatus
	Classification OutcomeClassification
	ErrorCode      string
	ErrorMessage   string
	Diagnostics    string
	Summary        string
	FailureCode    string
	ArtifactRefs   string
}

// FinishAttemptInput is the production completion contract. A retryable
// outcome must carry a durable command id so outcome, next Attempt and state
// projections commit together. Retryable completion must not use
// RecordAttemptOutcome.
type FinishAttemptInput struct {
	AttemptOutcomeInput
	RetryCommandID string
}

// FinishAttemptResult contains the durable state written by FinishAttempt.
// NextAttempt is set only when a retryable outcome creates the next Attempt.
type FinishAttemptResult struct {
	Outcome     AttemptOutcome
	Result      *Phase4Result
	NextAttempt *Phase4Attempt
	Duplicate   bool
}

// Phase4RecoveryDecision is returned by the Node or harness that can inspect
// its own execution. The server must not infer a remote Attempt is dead.
type Phase4RecoveryDecision string

const (
	Phase4RecoveryAlive   Phase4RecoveryDecision = "alive"
	Phase4RecoveryUnknown Phase4RecoveryDecision = "unknown"
)

// Phase4AttemptRecoveryResolver is implemented by an explicit Node/harness
// probe. A nil resolver means that Phase 4 recovery is not attempted.
type Phase4AttemptRecoveryResolver interface {
	ResolvePhase4Attempt(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error)
}

// Phase4AttemptRecoveryFunc adapts a function into a recovery resolver.
type Phase4AttemptRecoveryFunc func(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error)

func (f Phase4AttemptRecoveryFunc) ResolvePhase4Attempt(ctx context.Context, attempt Phase4Attempt) (Phase4RecoveryDecision, error) {
	return f(ctx, attempt)
}
