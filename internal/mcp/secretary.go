package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/beruseruko/secretary/internal/ctl"
)

// Secretary exposes only the Worker-first lifecycle contract. Retry is an
// internal terminal-event operation, not an MCP or client tool.
type Secretary struct {
	Workers ctl.WorkerService
}

func (s Secretary) Tools() []Tool {
	return []Tool{
		{Name: "list_nodes", Description: "List durable Execution Node records.", InputSchema: map[string]any{"type": "object"}},
		{Name: "list_projects", Description: "List registered Projects.", InputSchema: map[string]any{"type": "object"}},
		{Name: "list_workers", Description: "List persistent Workers in the Personal Conversation.", InputSchema: map[string]any{"type": "object"}},
		{Name: "get_worker", Description: "Read one Worker lifecycle and immutable binding.", InputSchema: objectSchema("worker_ref")},
		{Name: "spawn_worker", Description: "Create one bound Worker and its first Turn.", InputSchema: spawnWorkerSchema()},
		{Name: "message_worker", Description: "Steer, answer, follow up, or resume a Worker.", InputSchema: messageWorkerSchema()},
		{Name: "cancel_worker", Description: "Cancel a Worker's active Attempt.", InputSchema: objectSchema("worker_ref")},
		{Name: "close_worker", Description: "Close a Worker after safely stopping active work.", InputSchema: objectSchema("worker_ref")},
	}
}

func (s Secretary) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var args struct {
		WorkerRef string `json:"worker_ref"`
		Text      string `json:"text"`
		RequestID string `json:"request_id"`
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
		return s.Workers.SpawnWorker(ctx, args.SpawnWorkerRequest)
	case "message_worker":
		return s.Workers.MessageWorker(ctx, ctl.MessageWorkerRequest{WorkerRef: args.WorkerRef, Text: args.Text, RequestID: args.RequestID, IdempotencyKey: args.IdempotencyKey})
	case "cancel_worker":
		return s.Workers.CancelWorker(ctx, args.WorkerRef)
	case "close_worker":
		return s.Workers.CloseWorker(ctx, args.WorkerRef)
	default:
		return nil, fmt.Errorf("unknown Secretary tool %q", name)
	}
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
