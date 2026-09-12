package node

import (
	"context"
	"fmt"
)

// RuntimeRouter keeps existing sessions on their harness while allowing a
// reloaded config snapshot to select a different harness for new sessions.
// When a HarnessInstance is bound, its kind and ID are authoritative. Profile
// and DefaultHarness are only used for unbound compatibility requests.
type RuntimeRouter struct {
	DefaultHarness string
	ACP            Runtime
	Claude         Runtime
	// ClaudeCode is an explicit alias for integrations that name the adapter
	// after the product. Claude takes precedence when both are configured.
	ClaudeCode Runtime
	FX         Runtime
	OpenCode   Runtime
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
	// A bound HarnessInstance must never be collapsed into the generic ACP
	// runtime. In particular, Claude Code is a native CLI adapter, not Codex ACP.
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
