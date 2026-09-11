package core

import (
	"encoding/json"
	"time"
)

// EventInput is the normalized server event contract. Aggregate and source
// metadata are kept separately so clients can audit causation without parsing payloads.
type EventInput struct {
	Kind             string
	AggregateType    string
	AggregateID      string
	Source           string
	CorrelationID    string
	CausationID      string
	WorkerRef        string
	AttemptID        string
	RuntimeSessionID string
	Payload          any
}

// Event is an append-only, globally ordered durable event.
// Keep descriptive aliases for callers that use the normalized-event name.
type DurableEvent = Event
type NormalizedEvent = Event

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryFailed    DeliveryState = "failed"
)

const MaxDeliveryRetries = 3

type Delivery struct {
	ID             string        `json:"id"`
	EventID        string        `json:"event_id"`
	EntryID        string        `json:"entry_id,omitempty"`
	Target         string        `json:"target"`
	IdempotencyKey string        `json:"idempotency_key"`
	State          DeliveryState `json:"state"`
	RetryCount     int           `json:"retry_count"`
	LastError      string        `json:"last_error,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	DeliveredAt    *time.Time    `json:"delivered_at,omitempty"`
}

type ConversationReplay struct {
	BoundarySeq int64               `json:"boundary_seq"`
	Entries     []ConversationEntry `json:"entries"`
}

type EventReplay struct {
	BoundarySeq int64   `json:"boundary_seq"`
	Events      []Event `json:"events"`
}

type idempotencyRecord struct {
	Operation string
	Key       string
	Outcome   json.RawMessage
}

type workerCreationOutcome struct {
	Worker  Worker
	Turn    Turn
	Attempt Phase4Attempt
}

type turnCreationOutcome struct {
	Turn    Turn
	Attempt Phase4Attempt
}

type LifecycleAction struct {
	IdempotencyKey string
	Kind           string
	AggregateType  string
	AggregateID    string
	Source         string
	Payload        any
}

type LifecycleOutcome struct {
	ActionID string `json:"action_id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Event    Event  `json:"event"`
}
