package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

type CodexACPObserver interface {
	ObserveCodexACP(context.Context, string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error)
}

func (r ExecCommandRunner) ObserveCodexACP(ctx context.Context, binary string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	command := strings.TrimSpace(os.Getenv("SECRETARY_ACP_COMMAND"))
	if command == "" {
		command = "codex-acp"
	}
	return r.ObserveCodexACPWithCommand(ctx, binary, command)
}

func (r ExecCommandRunner) ObserveCodexACPWithCommand(ctx context.Context, binary, command string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	if strings.TrimSpace(command) == "" {
		return nil, nil, fmt.Errorf("codex: ACP command is required")
	}
	workspace, err := os.MkdirTemp("", "secretary-codex-readiness-")
	if err != nil {
		return nil, nil, fmt.Errorf("codex: readiness workspace unavailable")
	}
	defer os.RemoveAll(workspace)
	client, err := acp.StartWithLogEnvDir(ctx, nil, []string{"CODEX_PATH=" + binary, "CODEX_CONFIG={}"}, workspace, command, strings.Fields(os.Getenv("SECRETARY_ACP_ARGS"))...)
	if err != nil {
		return nil, nil, fmt.Errorf("codex: ACP adapter unavailable")
	}
	defer client.Close()
	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       struct {
			Version string `json:"version"`
		} `json:"agentInfo"`
		AgentCapabilities struct {
			LoadSession bool `json:"loadSession"`
		} `json:"agentCapabilities"`
		Meta struct {
			Steering struct {
				Supported bool `json:"supported"`
			} `json:"steering"`
		} `json:"_meta"`
	}
	if err := client.Request(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": acpClientCapabilities(), "clientInfo": map[string]string{"name": "secretary-readiness", "version": "1"}}, &init); err != nil {
		return nil, nil, classifyCodexACPFailure("initialize", err)
	}
	if init.ProtocolVersion != 1 || init.AgentInfo.Version == "" || !init.AgentCapabilities.LoadSession || !init.Meta.Steering.Supported {
		return nil, nil, fmt.Errorf("codex: ACP version/load/steering contract unavailable")
	}
	var created struct {
		SessionID string `json:"sessionId"`
		Models    struct {
			Available []struct {
				ID string `json:"modelId"`
			} `json:"availableModels"`
		} `json:"models"`
		nativeConfigResponse
	}
	if err := client.Request(ctx, "session/new", map[string]any{"cwd": workspace, "mcpServers": []MCPServer{}}, &created); err != nil {
		return nil, nil, classifyCodexACPFailure("session/new", err)
	}
	if created.SessionID == "" {
		return nil, nil, fmt.Errorf("codex: native session configuration unavailable")
	}
	models := []core.ObservedModelID{}
	levels := []core.ObservedReasoningLevel{}
	seenModels := map[string]bool{}
	seenLevels := map[string]bool{}
	for _, variant := range created.Models.Available {
		model := variant.ID
		effort := ""
		if i := strings.LastIndex(model, "["); i > 0 && strings.HasSuffix(model, "]") {
			effort = model[i+1 : len(model)-1]
			model = model[:i]
		}
		if model != "" && !seenModels[model] {
			models = append(models, core.ObservedModelID(model))
			seenModels[model] = true
		}
		if effort != "" && !seenLevels[effort] {
			levels = append(levels, core.ObservedReasoningLevel(effort))
			seenLevels[effort] = true
		}
	}
	if len(models) == 0 {
		return nil, nil, fmt.Errorf("codex: native model catalog unavailable")
	}
	return models, levels, nil
}

func classifyCodexACPFailure(phase string, err error) error {
 var rpcErr *acp.RPCError
 if !errors.As(err, &rpcErr) {
  return fmt.Errorf("codex: ACP %s protocol/transport failure: %w", phase, err)
 }
 if rpcErr.Code <= -32600 && rpcErr.Code >= -32700 {
  return fmt.Errorf("codex: ACP %s protocol/transport failure: %w", phase, err)
 }
 status := rpcErr.Code
 var details struct {
  Status int `json:"status"`
  StatusCode int `json:"statusCode"`
  HTTPStatus int `json:"httpStatus"`
  Code string `json:"code"`
  Type string `json:"type"`
 }
 if len(rpcErr.Data) != 0 {
  _ = json.Unmarshal(rpcErr.Data, &details)
  if details.Status != 0 { status = details.Status }
  if details.StatusCode != 0 { status = details.StatusCode }
  if details.HTTPStatus != 0 { status = details.HTTPStatus }
 }
 category := strings.ToLower(strings.TrimSpace(details.Code + " " + details.Type))
 switch status {
 case 401, 403:
  return fmt.Errorf("codex: native provider authentication required during %s: %w", phase, err)
 case 429:
  return fmt.Errorf("codex: native quota/provider restriction during %s: %w", phase, err)
 case 502, 503, 504:
  return fmt.Errorf("codex: native provider outage during %s: %w", phase, err)
 }
 for _, token := range strings.FieldsFunc(category, func(r rune) bool { return r == ' ' || r == '_' || r == '-' }) {
  switch token {
  case "unauthorized", "unauthenticated", "authentication", "invalidcredentials":
   return fmt.Errorf("codex: native provider authentication required during %s: %w", phase, err)
  case "quota", "ratelimit", "resourceexhausted":
   return fmt.Errorf("codex: native quota/provider restriction during %s: %w", phase, err)
  case "unavailable", "provideroutage":
   return fmt.Errorf("codex: native provider outage during %s: %w", phase, err)
  }
 }
 if rpcErr.Code <= -32000 && rpcErr.Code >= -32099 {
  message := strings.ToLower(strings.TrimSpace(rpcErr.Message))
  switch {
  case strings.HasPrefix(message, "unauthorized:"), strings.HasPrefix(message, "not logged in"), strings.HasPrefix(message, "authentication required"), strings.HasPrefix(message, "invalid api key"):
   return fmt.Errorf("codex: native provider authentication required during %s: %w", phase, err)
  case strings.HasPrefix(message, "quota exceeded"), strings.HasPrefix(message, "rate limit exceeded"), strings.HasPrefix(message, "usage limit"):
   return fmt.Errorf("codex: native quota/provider restriction during %s: %w", phase, err)
  case strings.HasPrefix(message, "upstream service unavailable"), strings.HasPrefix(message, "provider unavailable"):
   return fmt.Errorf("codex: native provider outage during %s: %w", phase, err)
  }
 }
 return fmt.Errorf("codex: ACP %s protocol/transport failure: %w", phase, err)
}
