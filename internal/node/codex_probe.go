package node

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

type CodexACPObserver interface {
	ObserveCodexACP(context.Context, string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error)
}

// No model call, credential export, or user configuration edit: initialize the
// actual adapter and observe the configuration of a disposable native session.
func (r ExecCommandRunner) ObserveCodexACP(ctx context.Context, binary string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	command := strings.TrimSpace(os.Getenv("SECRETARY_ACP_COMMAND"))
	if command == "" {
		command = "codex-acp"
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
		return nil, nil, fmt.Errorf("codex: ACP initialize failed")
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
	if err := client.Request(ctx, "session/new", map[string]any{"cwd": workspace, "mcpServers": []MCPServer{}}, &created); err != nil || created.SessionID == "" {
		return nil, nil, fmt.Errorf("codex: native session configuration unavailable")
	}
	// Model variants encode supported effort per model, independent of the current
	// native default. Preserve every observed model/effort, never synthesize pins.
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
