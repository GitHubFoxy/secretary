package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CodexRuntime retains ACP transport but uses Codex's explicit message phase
// and session-scoped instructions. It never rewrites user/global configuration.
type CodexRuntime struct {
	ACPRuntime
	CodexCommand string
}

func (r CodexRuntime) configured(request StartRequest) (ACPRuntime, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return ACPRuntime{}, err
	}
	if profile.ReplyContractVersion != "" {
		return ACPRuntime{}, fmt.Errorf("codex: addressed replies are unavailable; use the canonical assistant final contract")
	}
	base := r.ACPRuntime
	base.CodexMessagePhases = true
	base.DrainPromptEvents = true
	if model := strings.TrimSpace(profile.Model); model != "" && !isModelAlias(model) {
		base.ModelSelection = model
	}
	if effort := strings.TrimSpace(profile.Reasoning); effort != "" && effort != "default" {
		base.ReasoningSelection = effort
	}
	instructions := profile.EffectivePrompt()
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
