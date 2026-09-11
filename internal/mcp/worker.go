package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

// WorkerHandler deliberately exposes only the child-spawn capability. The
// actual child lifecycle implementation is supplied by the server boundary.
type WorkerHandler struct {
	Spawn     func(context.Context, string, string) (any, error)
	Authorize func(context.Context, string) error
	WorkerRef string
}

func (h WorkerHandler) Tools() []Tool {
	return []Tool{{
		Name:        "spawn_subagent",
		Description: "Create one bounded Child Worker for a subtask and return its reference.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]string{"type": "string"}},
			"required":   []string{"text"},
		},
	}}
}

func (h WorkerHandler) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name != "spawn_subagent" {
		return nil, errors.New("worker MCP tool is not available")
	}
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Text == "" {
		return nil, errors.New("spawn_subagent: text is required")
	}
	if h.Authorize != nil {
		if err := h.Authorize(ctx, h.WorkerRef); err != nil {
			return nil, err
		}
	}
	if h.Spawn == nil {
		return nil, errors.New("spawn_subagent: child spawner is unavailable")
	}
	return h.Spawn(ctx, h.WorkerRef, args.Text)
}

type EmptyHandler struct{}

func (EmptyHandler) Tools() []Tool { return []Tool{} }
func (EmptyHandler) Call(context.Context, string, json.RawMessage) (any, error) {
	return nil, errors.New("no MCP tools are available for this role")
}
