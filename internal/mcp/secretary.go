package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

// Secretary exposes only the Worker-first lifecycle contract. Retry is an
// internal terminal-event operation, not an MCP or client tool.
type Secretary struct {
	Workers              ctl.WorkerService
	ReplyContractVersion string
}

func (s Secretary) Tools() []Tool {
	spawnSchema := spawnWorkerSchema()
	messageSchema := messageWorkerSchema()
	tools := []Tool{
		{Name: "list_nodes", Description: "List durable Execution Node records.", InputSchema: map[string]any{"type": "object"}},
		{Name: "list_projects", Description: "List registered Projects.", InputSchema: map[string]any{"type": "object"}},
		{Name: "list_workers", Description: "List persistent Workers in the Personal Conversation.", InputSchema: map[string]any{"type": "object"}},
		{Name: "get_worker", Description: "Read one Worker lifecycle and immutable binding.", InputSchema: objectSchema("worker_ref")},
		{Name: "spawn_worker", Description: "Create one bound Worker and its first Turn.", InputSchema: spawnSchema},
		{Name: "message_worker", Description: "Steer, answer, follow up, or resume a Worker.", InputSchema: messageSchema},
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
			Name: "reply_to_user", Description: "Send one user-facing reply for the active originating input. Use for independent answers, clarifications, and errors; Worker Results arrive separately.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"secretary_turn_id": map[string]string{"type": "string"},
					"input_id":          map[string]string{"type": "string"},
					"text":              map[string]string{"type": "string"},
				},
				"required": []string{"secretary_turn_id", "input_id", "text"},
			},
		})
	}
	return tools
}

func (s Secretary) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var args struct {
		WorkerRef       string `json:"worker_ref"`
		Text            string `json:"text"`
		RequestID       string `json:"request_id"`
		SecretaryTurnID string `json:"secretary_turn_id"`
		InputID         string `json:"input_id"`
		ctl.SpawnWorkerRequest
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	switch name {
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
		"workspace": map[string]string{"type": "string"}, "model_id": map[string]string{"type": "string"},
		"reasoning": map[string]string{"type": "string"},
	}, "required": []string{"project_id"}}
	return map[string]any{"type": "object", "properties": map[string]any{
		"intent": map[string]string{"type": "string"}, "preferences": preferences,
		"idempotency_key": map[string]string{"type": "string"},
	}, "required": []string{"intent", "preferences"}}
}

func messageWorkerSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"worker_ref": map[string]string{"type": "string"}, "text": map[string]string{"type": "string"},
		"request_id": map[string]string{"type": "string"}, "idempotency_key": map[string]string{"type": "string"},
	}, "required": []string{"worker_ref", "text"}}
}
