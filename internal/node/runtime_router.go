package node

import (
	"context"
	"fmt"
)

// RuntimeRouter keeps existing sessions on their harness while allowing a
// reloaded config snapshot to select a different harness for new sessions.
// The dispatcher includes the immutable Profile runtime in every request.
type RuntimeRouter struct {
	DefaultHarness string
	ACP            Runtime
	FX             Runtime
	OpenCode       Runtime
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
	case "codex", "claude_code":
		return r.ACP
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
