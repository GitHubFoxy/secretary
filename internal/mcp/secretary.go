package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

type Secretary struct {
	Workers              ctl.WorkerService
	ReplyContractVersion string
	UserPath string
}

func (s Secretary) Tools() []Tool {
	spawnSchema := spawnWorkerSchema()
	messageSchema := messageWorkerSchema()
	tools := []Tool{
		{Name: "read_memory", Description: "Read owner Markdown memory or preferences and their current revision. kind is memory or preferences.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"memory", "preferences"}}}, "required": []string{"kind"}}},
		{Name: "remember", Description: "Append an explicitly owner-requested memory or preference. First call read_memory, then supply its expected_revision and the active owner's secretary_turn_id/input_id. Normal conversation is not permission to write.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"memory", "preferences"}}, "entry": map[string]string{"type": "string"}, "expected_revision": map[string]string{"type": "integer"}, "secretary_turn_id": map[string]string{"type": "string"}, "input_id": map[string]string{"type": "string"}}, "required": []string{"kind", "entry", "expected_revision", "secretary_turn_id", "input_id"}}},
		{Name: "list_nodes", Description: "List durable Execution Node records.", InputSchema: map[string]any{"type": "object"}},
		{Name: "list_projects", Description: "List registered Projects.", InputSchema: map[string]any{"type": "object"}},
		{Name: "list_workers", Description: "List persistent Workers in the Personal Conversation.", InputSchema: map[string]any{"type": "object"}},
		{Name: "get_worker", Description: "Read one Worker lifecycle and immutable binding.", InputSchema: objectSchema("worker_ref")},
		{Name: "spawn_worker", Description: "Start a new Task with a persistent Worker, automatically dispatched to a ready Node and harness. Only intent is required; preferences can override node_id, harness_kind, harness_instance_id, model or Project. Do not use for an answer to an existing Worker's question; use message_worker with that worker_ref instead.", InputSchema: spawnSchema},
		{Name: "message_worker", Description: "Send an owner's answer or new instruction to an existing Worker by worker_ref. An idle Worker starts a Follow-up Turn in the same Task; an active Worker receives steering; /q explicitly queues. Use this when answering a Worker Result question, not spawn_worker.", InputSchema: messageSchema},
		{Name: "cancel_worker", Description: "Cancel a Worker's active Attempt.", InputSchema: objectSchema("worker_ref")},
		{Name: "close_worker", Description: "Close a Worker after safely stopping active work.", InputSchema: objectSchema("worker_ref")},
	}
	if s.ReplyContractVersion == "" {
		tools = append(tools, Tool{Name: "acknowledge_user", Description: "Send one brief acknowledgement before any work or lifecycle tools. This is progress, not a completed answer; report failures in the final answer.", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"secretary_turn_id": map[string]string{"type": "string"}, "input_id": map[string]string{"type": "string"}, "text": map[string]string{"type": "string"}}, "required": []string{"secretary_turn_id", "input_id", "text"},
		}})
	}
	if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
		addOriginFields(spawnSchema)
		addOriginFields(messageSchema)
		tools = append(tools, Tool{
			Name: "reply_to_user", Description: "Send one user-facing reply for the active originating input. Use for independent answers, clarifications, and errors; Worker Results arrive separately. A claim that a new Worker was created must set action_kind=spawn_worker and worker_turn_id to the action_turn_id returned by the successful Secretary spawn_worker MCP call; the server verifies the canonical action.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"secretary_turn_id": map[string]string{"type": "string"},
					"input_id":          map[string]string{"type": "string"},
					"text":              map[string]string{"type": "string"},
					"action_kind":       map[string]any{"type": "string", "enum": []string{"spawn_worker"}},
                    "reply_kind": map[string]any{"type":"string","enum":[]string{"answer","error","spawn_worker"}},
					"worker_turn_id":    map[string]string{"type": "string"},
				},
				"required": []string{"secretary_turn_id", "input_id", "text", "reply_kind"},
			},
		})
	}
	return tools
}

func (s Secretary) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var args struct {
		Kind            string `json:"kind"`
		Entry           string `json:"entry"`
		ExpectedRevision int64 `json:"expected_revision"`
		WorkerRef       string `json:"worker_ref"`
		Text            string `json:"text"`
		RequestID       string `json:"request_id"`
		SecretaryTurnID string `json:"secretary_turn_id"`
		InputID         string `json:"input_id"`
		ActionKind      string `json:"action_kind"`
        ReplyKind string `json:"reply_kind"`
		WorkerTurnID    string `json:"worker_turn_id"`
		ctl.SpawnWorkerRequest
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	switch name {
	case "read_memory":
		if s.Workers.Store == nil {
			return nil, fmt.Errorf("memory store unavailable")
		}
		return s.Workers.Store.ReadMemoryDocument(ctx, s.UserPath, args.Kind)
	case "remember":
		if s.Workers.Store == nil {
			return nil, fmt.Errorf("memory store unavailable")
		}
		return s.Workers.Store.RememberOwnerMemory(ctx, s.UserPath, args.Kind, args.Entry, args.ExpectedRevision, s.Workers.PersonID, s.Workers.Capability, args.SecretaryTurnID, args.InputID)
	case "list_nodes":
		return s.Workers.ListNodes(ctx)
	case "list_projects":
		return s.Workers.ListProjects(ctx)
	case "list_workers":
		return s.Workers.ListWorkers(ctx)
	case "get_worker":
		return s.Workers.GetWorker(ctx, args.WorkerRef)
	case "spawn_worker":
		if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
			if err := s.Workers.ValidateSecretaryOrigin(ctx, args.SecretaryTurnID, args.InputID); err != nil {
				return nil, err
			}
		}
		spawnRequest := args.SpawnWorkerRequest
		if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
			spawnRequest.SecretaryTurnID, spawnRequest.InputID = args.SecretaryTurnID, args.InputID
		}
		details, err := s.Workers.SpawnWorker(ctx, spawnRequest)
		if err != nil {
			return nil, err
		}
		if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
			if _, err := s.Workers.LinkSecretaryWorkerTurn(ctx, args.SecretaryTurnID, args.InputID, details.ActionTurnID); err != nil {
				return nil, err
			}
		}
		return details, nil
	case "message_worker":
		if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
			if err := s.Workers.ValidateSecretaryOrigin(ctx, args.SecretaryTurnID, args.InputID); err != nil {
				return nil, err
			}
		}
		messageRequest := ctl.MessageWorkerRequest{WorkerRef: args.WorkerRef, Text: args.Text, RequestID: args.RequestID, IdempotencyKey: args.IdempotencyKey}
		if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
			messageRequest.SecretaryTurnID, messageRequest.InputID = args.SecretaryTurnID, args.InputID
		}
		details, err := s.Workers.MessageWorker(ctx, messageRequest)
		if err != nil {
			return nil, err
		}
		if s.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 && details.ActionTurnID != "" {
			if _, err := s.Workers.LinkSecretaryWorkerTurn(ctx, args.SecretaryTurnID, args.InputID, details.ActionTurnID); err != nil {
				return nil, err
			}
		}
		return details, nil
	case "acknowledge_user":
		if s.ReplyContractVersion != "" {
			return nil, fmt.Errorf("unknown Secretary tool %q", name)
		}
		entry, duplicate, err := s.Workers.AcknowledgeUser(ctx, ctl.SecretaryReplyRequest{SecretaryTurnID: args.SecretaryTurnID, InputID: args.InputID, Text: args.Text})
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry_id": entry.ID, "duplicate": duplicate}, nil
	case "reply_to_user":
		if s.ReplyContractVersion != core.SecretaryReplyContractAddressedV1 {
			return nil, fmt.Errorf("unknown Secretary tool %q", name)
		}
		switch args.ReplyKind {
        case "spawn_worker":
            if args.ActionKind != "spawn_worker" || args.WorkerTurnID == "" { return nil, fmt.Errorf("spawn confirmation requires a typed accepted action") }
            if err := s.Workers.Store.ValidateSecretaryAcceptedSpawnOrigin(ctx, core.SecretaryOriginIdentity{PersonID:s.Workers.PersonID,Capability:s.Workers.Capability,SecretaryTurnID:args.SecretaryTurnID,InputID:args.InputID}, args.WorkerTurnID); err != nil { return nil, fmt.Errorf("worker creation has no accepted Secretary action: %w",err) }
        case "answer","error":
            if args.ActionKind != "" || args.WorkerTurnID != "" { return nil, fmt.Errorf("non-lifecycle reply cannot contain an action confirmation") }
        default:
            return nil, fmt.Errorf("reply_kind is required")
        }
		entry, duplicate, err := s.Workers.ReplyToUser(ctx, ctl.SecretaryReplyRequest{SecretaryTurnID: args.SecretaryTurnID, InputID: args.InputID, Text: args.Text})
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry_id": entry.ID, "duplicate": duplicate}, nil
	case "cancel_worker":
		return s.Workers.CancelWorker(ctx, args.WorkerRef)
	case "close_worker":
		return s.Workers.CloseWorker(ctx, args.WorkerRef)
	default:
		return nil, fmt.Errorf("unknown Secretary tool %q", name)
	}
}

func addOriginFields(schema map[string]any) {
	properties := schema["properties"].(map[string]any)
	properties["secretary_turn_id"] = map[string]string{"type": "string"}
	properties["input_id"] = map[string]string{"type": "string"}
	required := schema["required"].([]string)
	schema["required"] = append(required, "secretary_turn_id", "input_id")
}

func objectSchema(required string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{required: map[string]string{"type": "string"}}, "required": []string{required}}
}

func spawnWorkerSchema() map[string]any {
	preferences := map[string]any{"type": "object", "properties": map[string]any{
		"project_id": map[string]string{"type": "string"}, "node_id": map[string]string{"type": "string"},
		"harness_instance_id": map[string]string{"type": "string"}, "harness_kind": map[string]string{"type": "string"},
		"workspace": map[string]string{"type": "string", "description": "Only valid with an authorized project_id; projectless Workers always get a Node-created workspace."}, "model_id": map[string]string{"type": "string"},
		"reasoning": map[string]string{"type": "string"},
	}}
	return map[string]any{"type": "object", "properties": map[string]any{
		"intent": map[string]string{"type": "string"}, "preferences": preferences,
		"idempotency_key": map[string]string{"type": "string"},
	}, "required": []string{"intent"}}
}

func messageWorkerSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"worker_ref": map[string]string{"type": "string"}, "text": map[string]string{"type": "string"},
		"request_id": map[string]string{"type": "string"}, "idempotency_key": map[string]string{"type": "string"},
	}, "required": []string{"worker_ref", "text"}}
}
