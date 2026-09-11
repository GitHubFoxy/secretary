package core

import "time"

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
	ID             string             `json:"id"`
	IdentityID     string             `json:"identity_id"`
	ConversationID string             `json:"conversation_id"`
	Input          string             `json:"input"`
	State          SecretaryTurnState `json:"state"`
	QueuePosition  int64              `json:"queue_position"`
	Error          string             `json:"error,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	StartedAt      *time.Time         `json:"started_at,omitempty"`
	FinishedAt     *time.Time         `json:"finished_at,omitempty"`
	UpdatedAt      time.Time          `json:"updated_at"`
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
