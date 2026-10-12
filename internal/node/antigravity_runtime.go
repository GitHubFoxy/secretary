package node

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

type AntigravityRuntime struct {
	ACPRuntime
	AntigravityCommand string
	ProfileDir         string
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

	profileDir := ResolveAntigravityProfileDir(r.ProfileDir)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return ACPRuntime{}, fmt.Errorf("antigravity: profile directory unavailable: %w", err)
	}

	tempDir := filepath.Join(profileDir, "tmp")
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

func (r AntigravityRuntime) ProbeSpec() HarnessProbeSpec {
	spec := DefaultAntigravityProbeSpec()
	if strings.TrimSpace(r.AntigravityCommand) != "" {
		spec.Binary = r.AntigravityCommand
	}
	spec.ProfileDir = ResolveAntigravityProfileDir(r.ProfileDir)
	return spec
}

func (r AntigravityRuntime) Probe(ctx context.Context, node core.NodeReference, runner CommandRunner) ProbeResult {
	spec := r.ProbeSpec()
	return HarnessProbe{Node: node, Spec: spec, Runner: runner}.Probe(ctx)
}
