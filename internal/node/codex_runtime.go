package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type CodexRuntime struct {
	ACPRuntime
	CodexCommand string
}

func (r CodexRuntime) configured(request StartRequest) (ACPRuntime, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return ACPRuntime{}, err
	}
	base := r.ACPRuntime
	if base.Command == "/__secretary_codex_acp_inventory_unavailable__" {
		return ACPRuntime{}, fmt.Errorf("Codex runtime unavailable: selected native inventory is not ready; configure the Codex pair, run secretary doctor and restart")
	}
	base.CodexMessagePhases = true
	base.DrainPromptEvents = true
	base.TerminalMessageGrouping = true
	if model := strings.TrimSpace(profile.Model); model != "" && !isModelAlias(model) {
		base.ModelSelection = model
	}
	if effort := strings.TrimSpace(profile.Reasoning); effort != "" && effort != "default" {
		base.ReasoningSelection = effort
	}
	instructions := profile.EffectivePrompt()
	instructions = strings.TrimSpace(instructions + "\n\n" + CodexRecoveryInstruction)
	environment, err := profileEnvironment(base.Environment, profile)
	if err != nil {
		return ACPRuntime{}, err
	}
	overrides := map[string]any{}
	for _, value := range environment {
		if strings.HasPrefix(value, "CODEX_CONFIG=") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(value, "CODEX_CONFIG=")), &overrides); err != nil {
				return ACPRuntime{}, err
			}
		}
	}
	if instructions != "" {
		overrides["developer_instructions"] = instructions
	}
	if profile.Name == "worker" {
		base.ModeSelection = "agent-full-access"
	}
	if model := strings.TrimSpace(profile.Model); model == "" || strings.EqualFold(model, "default") {
		if configured, exists := overrides["model"]; exists {
			selected, ok := configured.(string)
			if !ok || strings.TrimSpace(selected) == "" {
				return ACPRuntime{}, fmt.Errorf("codex: configured model must be a non-empty string")
			}
		} else {
			overrides["model"] = "gpt-5.4"
		}
	}
	encoded, err := json.Marshal(overrides)
	if err != nil {
		return ACPRuntime{}, err
	}
	base.Environment = nil
	for _, value := range environment {
		if !strings.HasPrefix(value, "CODEX_CONFIG=") && !strings.HasPrefix(value, "CODEX_PATH=") {
			base.Environment = append(base.Environment, value)
		}
	}
	command := r.CodexCommand
	if command == "" {
		command = os.Getenv("SECRETARY_CODEX_COMMAND")
	}
	if command == "" {
		command = os.Getenv("CODEX_PATH")
	}
	if command == "" {
		command = "codex"
	}
	base.Environment = append(base.Environment, "CODEX_CONFIG="+string(encoded), "CODEX_PATH="+command)
	return base, nil
}
func (r CodexRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	base, err := r.configured(request)
	if err != nil {
		return nil, err
	}
	return base.Start(ctx, request)
}
func (r CodexRuntime) Resume(ctx context.Context, request StartRequest, id string) (Session, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrRuntimeSessionUnavailable
	}
	base, err := r.configured(request)
	if err != nil {
		return nil, err
	}
	return base.Resume(ctx, request, id)
}
