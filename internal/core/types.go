package core

import "time"

type TaskState string

const (
	TaskDispatching    TaskState = "dispatching"
	TaskDispatchFailed TaskState = "dispatch_failed"
	TaskOpen           TaskState = "open"
	TaskClosing        TaskState = "closing"
	TaskClosed         TaskState = "closed"
)

type AttemptState string

const (
	AttemptStarting    AttemptState = "starting"
	AttemptActive      AttemptState = "active"
	AttemptSucceeded   AttemptState = "succeeded"
	AttemptFailed      AttemptState = "failed"
	AttemptCanceled    AttemptState = "canceled"
	AttemptInterrupted AttemptState = "interrupted"
)

func (s AttemptState) Terminal() bool {
	return s == AttemptSucceeded || s == AttemptFailed || s == AttemptCanceled || s == AttemptInterrupted
}

type ResultStatus string

const (
	ResultSucceeded ResultStatus = "succeeded"
	ResultFailed    ResultStatus = "failed"
	ResultCanceled  ResultStatus = "canceled"
)

type EntryKind string

const (
	EntryUser         EntryKind = "user"
	EntrySecretary    EntryKind = "secretary"
	EntryWorkerInput  EntryKind = "worker_input"
	EntryWorkerResult EntryKind = "worker_result"
	EntrySystem       EntryKind = "system"
)

type Person struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type Conversation struct {
	ID       string `json:"id"`
	PersonID string `json:"person_id"`
}

type ConversationEntry struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Seq            int64     `json:"seq"`
	Kind           EntryKind `json:"kind"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"created_at"`
}

type Task struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Text           string    `json:"text"`
	State          TaskState `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type WorkerBinding struct {
	ID               string    `json:"id"`
	TaskID           string    `json:"task_id"`
	WorkerRef        string    `json:"worker_ref"`
	NodeID           string    `json:"node_id"`
	RuntimeSessionID string    `json:"runtime_session_id"`
	Archived         bool      `json:"archived"`
	CreatedAt        time.Time `json:"created_at"`
}

type Attempt struct {
	ID              string       `json:"id"`
	WorkerBindingID string       `json:"worker_binding_id"`
	Number          int          `json:"number"`
	State           AttemptState `json:"state"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Result struct {
	ID        string       `json:"id"`
	AttemptID string       `json:"attempt_id"`
	Status    ResultStatus `json:"status"`
	Summary   string       `json:"summary"`
	CreatedAt time.Time    `json:"created_at"`
}

type TaskDetails struct {
	Task     Task           `json:"task"`
	Binding  *WorkerBinding `json:"binding,omitempty"`
	Attempts []Attempt      `json:"attempts"`
	Results  []Result       `json:"results"`
}

type CloseOutcome struct {
	Task          Task     `json:"task"`
	CancelAttempt *Attempt `json:"cancel_attempt,omitempty"`
}
