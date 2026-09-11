package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/beruseruko/secretary/internal/ctl"
)

type Secretary struct {
	Service ctl.Service
	Models  func() map[string]string
}

func (s Secretary) Tools() []Tool {
	return []Tool{
		{Name: "delegate_task", Description: "Create and dispatch a persistent Worker Task.", InputSchema: objectSchema("text")},
		{Name: "retry_task", Description: "Retry a dispatch_failed Task.", InputSchema: objectSchema("task_id")},
		{Name: "close_task", Description: "Close a Task.", InputSchema: objectSchema("task_id")},
		{Name: "list_tasks", Description: "List Tasks in the Personal Conversation.", InputSchema: map[string]any{"type": "object"}},
		{Name: "show_task", Description: "Show Task, Worker, Attempts and Results.", InputSchema: objectSchema("task_id")},
		{Name: "available_models_for_worker", Description: "Inspect configured Worker model aliases after an explicit user request.", InputSchema: map[string]any{"type": "object"}},
	}
}
func (s Secretary) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	var args struct {
		Text   string `json:"text"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	switch name {
	case "delegate_task":
		return s.Service.Create(ctx, args.Text)
	case "retry_task":
		return s.Service.Retry(ctx, args.TaskID)
	case "close_task":
		return s.Service.Close(ctx, args.TaskID)
	case "list_tasks":
		return s.Service.List(ctx)
	case "show_task":
		return s.Service.Show(ctx, args.TaskID)
	case "available_models_for_worker":
		if s.Models == nil {
			return map[string]string{}, nil
		}
		return s.Models(), nil
	default:
		return nil, fmt.Errorf("unknown Secretary tool %q", name)
	}
}
func objectSchema(required string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{required: map[string]string{"type": "string"}}, "required": []string{required}}
}
