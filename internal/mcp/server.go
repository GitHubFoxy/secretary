// Package mcp implements the small MCP stdio surface owned by Secretary.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}
type Handler interface {
	Tools() []Tool
	Call(context.Context, string, json.RawMessage) (any, error)
}
type Server struct{ Handler Handler }
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s.Handler == nil {
		return fmt.Errorf("mcp: handler is required")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		result, rpcErr := s.handle(ctx, req)
		// JSON-RPC notifications do not receive responses.
		if len(req.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			response["error"] = rpcErr
		} else {
			response["result"] = result
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := out.Write(append(encoded, '\n')); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func toolText(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func (s Server) handle(ctx context.Context, req request) (any, map[string]any) {
	switch req.Method {
	case "initialize":
		return map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]string{"name": "secretary-mcp", "version": "0.1.0"}, "capabilities": map[string]any{"tools": map[string]any{}}}, nil
	case "notifications/initialized", "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.Handler.Tools()}, nil
	case "tools/call":
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if len(req.Params) == 0 {
			return nil, map[string]any{"code": -32602, "message": "tools/call params are required"}
		}
		if err := json.Unmarshal(req.Params, &call); err != nil || call.Name == "" {
			return nil, map[string]any{"code": -32602, "message": "tools/call requires a tool name and arguments"}
		}
		if call.Arguments == nil {
			call.Arguments = json.RawMessage(`{}`)
		}
		value, err := s.Handler.Call(ctx, call.Name, call.Arguments)
		if err != nil {
			return map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}, nil
		}
		text, err := toolText(value)
		if err != nil {
			return nil, map[string]any{"code": -32000, "message": fmt.Sprintf("encode tool result: %v", err)}
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}, nil
	default:
		return nil, map[string]any{"code": -32601, "message": "method not found"}
	}
}
