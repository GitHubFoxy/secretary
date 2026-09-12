package node

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

type recordingRuntime struct{ started chan StartRequest }

func (r recordingRuntime) Start(_ context.Context, request StartRequest) (Session, error) {
	r.started <- request
	return nil, nil
}

func TestRuntimeRouterUsesImmutableHarnessBindingAndPins(t *testing.T) {
	codex := recordingRuntime{started: make(chan StartRequest, 1)}
	fx := recordingRuntime{started: make(chan StartRequest, 1)}
	router := RuntimeRouter{DefaultHarness: "fx", ACP: codex, FX: fx}
	instance := core.HarnessInstance{ID: "node/codex", Node: "node", Kind: core.HarnessCodex, Version: "1", Status: core.HarnessReady}
	if _, err := router.Start(context.Background(), StartRequest{HarnessInstance: instance, Model: "gpt-5-codex", Reasoning: "high", Profile: ManagedProfile{Runtime: "fx"}}); err == nil {
		t.Fatal("accepted profile runtime that conflicts with immutable codex binding")
	}
	if _, err := router.Start(context.Background(), StartRequest{HarnessInstance: instance, Model: "gpt-5-codex", Reasoning: "high"}); err != nil {
		t.Fatal(err)
	}
	request := <-codex.started
	if !reflect.DeepEqual(request.HarnessInstance, instance) || request.Model != "gpt-5-codex" || request.Reasoning != "high" || request.Profile.Model != request.Model || request.Profile.Reasoning != request.Reasoning {
		t.Fatalf("runtime request lost immutable contract: %#v", request)
	}
	select {
	case <-fx.started:
		t.Fatal("fx runtime was selected for codex binding")
	default:
	}
}

func TestRuntimeRouterUsesDistinctClaudeRuntime(t *testing.T) {
	codex := recordingRuntime{started: make(chan StartRequest, 1)}
	claude := recordingRuntime{started: make(chan StartRequest, 1)}
	router := RuntimeRouter{DefaultHarness: "fx", ACP: codex, Claude: claude}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	if _, err := router.Start(context.Background(), StartRequest{HarnessInstance: instance, Model: "claude-sonnet", Reasoning: "extended", Profile: ManagedProfile{Runtime: "claude_code"}}); err != nil {
		t.Fatal(err)
	}
	request := <-claude.started
	if request.HarnessInstance.Kind != core.HarnessClaudeCode || request.Model != "claude-sonnet" || request.Reasoning != "extended" {
		t.Fatalf("Claude runtime lost immutable request: %#v", request)
	}
	select {
	case <-codex.started:
		t.Fatal("Codex runtime was selected for Claude binding")
	default:
	}
}

func TestRuntimeRouterNeverFallsBackFromClaudeToCodex(t *testing.T) {
	codex := recordingRuntime{started: make(chan StartRequest, 1)}
	router := RuntimeRouter{DefaultHarness: "codex", ACP: codex}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	_, err := router.Start(context.Background(), StartRequest{HarnessInstance: instance, Profile: ManagedProfile{Runtime: "fx"}})
	if err == nil {
		t.Fatal("Claude binding with conflicting profile was silently accepted")
	}
	select {
	case <-codex.started:
		t.Fatal("Codex runtime was used as Claude fallback")
	default:
	}
	_, err = router.Start(context.Background(), StartRequest{HarnessInstance: instance})
	if err == nil || !strings.Contains(err.Error(), `harness "claude_code" is unavailable`) {
		t.Fatalf("missing Claude runtime error=%v", err)
	}
	select {
	case <-codex.started:
		t.Fatal("Codex runtime was used after Claude runtime was unavailable")
	default:
	}
}

func TestRuntimeRouterUsesProfileHarness(t *testing.T) {
	compat := recordingRuntime{started: make(chan StartRequest, 1)}
	fx := recordingRuntime{started: make(chan StartRequest, 1)}
	native := recordingRuntime{started: make(chan StartRequest, 1)}
	router := RuntimeRouter{DefaultHarness: "codex", ACP: compat, FX: fx, OpenCode: native}
	if _, err := router.Start(context.Background(), StartRequest{Profile: ManagedProfile{Runtime: "opencode"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-native.started:
	default:
		t.Fatal("OpenCode runtime was not selected")
	}
	if _, err := router.Start(context.Background(), StartRequest{Profile: ManagedProfile{Runtime: "codex"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-compat.started:
	default:
		t.Fatal("compatibility runtime was not selected")
	}
	if _, err := router.Start(context.Background(), StartRequest{Profile: ManagedProfile{Runtime: "fx"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fx.started:
	default:
		t.Fatal("fx runtime was not selected")
	}
}
