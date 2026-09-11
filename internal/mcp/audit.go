package mcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/beruseruko/secretary/internal/core"
)

var ErrHandlerUnavailable = errors.New("mcp: handler is unavailable")

// AuditedHandler makes each MCP invocation observable before and after the
// underlying capability handler runs. The pre-event is the commit point for
// the operation, so a storage failure prevents the tool from running.
type AuditedHandler struct {
	Handler   Handler
	Store     *core.Store
	Role      string
	WorkerRef string
}

func (h AuditedHandler) Tools() []Tool { return h.Handler.Tools() }

func (h AuditedHandler) Call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	if h.Handler == nil {
		return nil, ErrHandlerUnavailable
	}
	payload := map[string]any{"role": h.Role, "tool": name, "arguments": json.RawMessage(args), "status": "started"}
	if h.Store != nil {
		if _, err := h.Store.RecordEvent(ctx, "mcp.tool_call", h.WorkerRef, "", "", payload); err != nil {
			return nil, err
		}
	}
	value, err := h.Handler.Call(ctx, name, args)
	outcome := map[string]any{"role": h.Role, "tool": name, "status": "succeeded"}
	if err != nil {
		outcome["status"] = "failed"
		outcome["error"] = err.Error()
	}
	if h.Store != nil {
		_, _ = h.Store.RecordEvent(context.Background(), "mcp.tool_result", h.WorkerRef, "", "", outcome)
	}
	return value, err
}
