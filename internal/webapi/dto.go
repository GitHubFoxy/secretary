package webapi

import "github.com/beruseruko/secretary/internal/core"

type messageAcknowledgement struct {
	Entry     core.ConversationEntry `json:"entry"`
	MessageID string                 `json:"message_id"`
	EntrySeq  int64                  `json:"entry_seq"`
	State     string                 `json:"state"`
	WorkerRef string                 `json:"worker_ref,omitempty"`
	TurnID    string                 `json:"turn_id,omitempty"`
	Duplicate bool                   `json:"duplicate"`
}
