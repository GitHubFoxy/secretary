package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type AntigravityRuntime struct {
	ACPRuntime
	AntigravityCommand string
}

func (r AntigravityRuntime) configured(request StartRequest) (ACPRuntime, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return ACPRuntime{}, err
	}
	if profile.ReplyContractVersion != "" {
		return ACPRuntime{}, fmt.Errorf("antigravity: addressed replies are unavailable; use the canonical assistant final contract")
	}

	realExec, companion, err := ResolveAntigravityExecutable(r.AntigravityCommand)
	if err != nil {
		return ACPRuntime{}, fmt.Errorf("antigravity: %w", err)
	}

	workerRef := request.WorkerRef
	if workerRef == "" {
		workerRef = "default"
	}

	profileDir := filepath.Join(os.TempDir(), "secretary-antigravity-"+workerRef)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return ACPRuntime{}, fmt.Errorf("antigravity: profile directory unavailable: %w", err)
	}
	tempDir := filepath.Join(os.TempDir(), "secretary-antigravity-tmp-"+workerRef)
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return ACPRuntime{}, fmt.Errorf("antigravity: temp directory unavailable: %w", err)
	}

	env := BuildAntigravityEnvironment(r.Environment, companion, profileDir, tempDir)

	base := r.ACPRuntime
	base.Command = realExec
	base.Arguments = BuildAntigravityPlatformArgs(r.Arguments)
	base.Environment = env
	base.ExactEnvironment = true
	base.PreferResumeSession = true
	base.DrainPromptEvents = true

	if model := strings.TrimSpace(profile.Model); model != "" && !isModelAlias(model) {
		base.ModelSelection = model
	}
	if effort := strings.TrimSpace(profile.Reasoning); effort != "" && effort != "default" {
		base.ReasoningSelection = effort
	}

	return base, nil
}

func (r AntigravityRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	base, err := r.configured(request)
	if err != nil {
		return nil, err
	}
	return base.Start(ctx, request)
}

func (r AntigravityRuntime) Resume(ctx context.Context, request StartRequest, id string) (Session, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrRuntimeSessionUnavailable
	}
	base, err := r.configured(request)
	if err != nil {
		return nil, err
	}
	return base.Resume(ctx, request, id)
}
