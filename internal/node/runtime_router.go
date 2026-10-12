package node

import (
	"context"
	"fmt"
)

type RuntimeRouter struct {
	DefaultHarness string
	ACP            Runtime
	Claude         Runtime
	ClaudeCode     Runtime
	FX             Runtime
	OpenCode       Runtime
	Antigravity    Runtime
}

func (r RuntimeRouter) Start(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	harness := r.harness(request)
	runtime := r.runtime(request)
	if runtime == nil {
		return nil, fmt.Errorf("node: harness %q is unavailable", harness)
	}
	return runtime.Start(ctx, request)
}

func (r RuntimeRouter) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	harness := r.harness(request)
	runtime := r.runtime(request)
	if runtime == nil {
		return nil, fmt.Errorf("node: harness %q is unavailable", harness)
	}
	if resumer, ok := runtime.(Resumer); ok {
		return resumer.Resume(ctx, request, runtimeSessionID)
	}
	return nil, errRuntimeResumeUnsupported{}
}

func (r RuntimeRouter) harness(request StartRequest) string {
	if request.HarnessInstance.Kind != "" {
		return string(request.HarnessInstance.Kind)
	}
	harness := request.Profile.Runtime
	if harness == "" {
		harness = r.DefaultHarness
	}
	return harness
}

func (r RuntimeRouter) runtime(request StartRequest) Runtime {
	harness := r.harness(request)
	switch harness {
	case "opencode":
		return r.OpenCode
	case "fx":
		return r.FX
	case "codex":
		return r.ACP
	case "claude_code":
		if r.Claude != nil {
			return r.Claude
		}
		return r.ClaudeCode
	case "antigravity":
		return r.Antigravity
	default:
		return unavailableRuntime{harness: harness}
	}
}

type unavailableRuntime struct{ harness string }

func (r unavailableRuntime) Start(context.Context, StartRequest) (Session, error) {
	return nil, fmt.Errorf("node: harness %q is unavailable", r.harness)
}

func (r unavailableRuntime) Error() string {
	return fmt.Sprintf("node: harness %q is unavailable", r.harness)
}

func (r unavailableRuntime) Resume(context.Context, StartRequest, string) (Session, error) {
	return nil, r
}

type errRuntimeResumeUnsupported struct{}

func (errRuntimeResumeUnsupported) Error() string {
	return "node: selected runtime does not support session resume"
}

var _ Runtime = RuntimeRouter{}
var _ Resumer = RuntimeRouter{}
