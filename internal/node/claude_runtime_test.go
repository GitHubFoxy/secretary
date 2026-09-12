package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestClaudeCodeRuntimeUsesNativeCLIWithAuthoritativePins(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "claude-request.json")
	launcher := filepath.Join(t.TempDir(), "claude")
	launcherScript := "#!/bin/sh\nprintf '%s' \"$*\" > \"$CLAUDE_CAPTURE\"\nexec \"$CLAUDE_TEST_BINARY\" -test.run=TestFakeClaudeCodeProcess\n"
	if err := os.WriteFile(launcher, []byte(launcherScript), 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := ClaudeCodeRuntime{Command: launcher, Environment: []string{"CLAUDE_CAPTURE=" + capture, "CLAUDE_TEST_BINARY=" + os.Args[0]}}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: t.TempDir(), HarnessInstance: instance, Model: "claude-sonnet", Reasoning: "extended"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" || result.Summary != "native Claude result" {
			t.Fatalf("result=%#v", result)
		}
	case <-ctx.Done():
		t.Fatal("Claude Code did not complete")
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--print", "--output-format", "stream-json", "--model", "claude-sonnet", "--effort", "extended", "--session-id", "inspect"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("Claude CLI args=%s, missing %q", args, want)
		}
	}
}

func TestClaudeCodeRuntimeRejectsConfiguredSemanticArgumentInjection(t *testing.T) {
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	dangerous := []string{
		"--model", "--model=attacker", "-m", "-mattacker",
		"--effort", "--effort=low", "--thinking", "--max-thinking-tokens",
		"--fallback-model", "--fallback-model=attacker",
		"--session-id", "--session-id=attacker", "--resume", "--resume=attacker",
		"--continue", "-c", "--fork-session", "--from-pr", "--no-session-persistence", "--",
		"--print", "-p", "--verbose", "--output-format", "--output-format=text",
		"--input-format", "--input-format=text", "--stream-json",
		"--include-partial-messages", "--json-schema", "--replay-user-messages",
		"--permission-prompt-tool",
	}
	for _, argument := range dangerous {
		t.Run(argument, func(t *testing.T) {
			runtime := ClaudeCodeRuntime{Arguments: []string{argument, "attacker-value"}}
			_, err := runtime.Start(context.Background(), StartRequest{
				HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect",
				Model: "claude-sonnet", Reasoning: "high",
			})
			if !errors.Is(err, ErrClaudeCodeConfiguration) {
				t.Fatalf("argument %q err=%v, want configuration error", argument, err)
			}
		})
	}
}

func TestClaudeCodeRuntimeBuildsExactAuthoritativeArguments(t *testing.T) {
	runtime := ClaudeCodeRuntime{Arguments: []string{"--permission-mode", "acceptEdits"}}
	request := StartRequest{Task: "inspect", Model: "claude-sonnet", Reasoning: "extended"}
	got, err := runtime.authoritativeArguments(request, "session-id", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--permission-mode", "acceptEdits", "--print", "--output-format", "stream-json", "--verbose", "--model", "claude-sonnet", "--effort", "extended", "--session-id", "session-id", "inspect"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude CLI args=%#v, want %#v", got, want)
	}
	got, err = runtime.authoritativeArguments(request, "saved-session", true)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--permission-mode", "acceptEdits", "--print", "--output-format", "stream-json", "--verbose", "--model", "claude-sonnet", "--effort", "extended", "--resume", "saved-session", "inspect"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude resume args=%#v, want %#v", got, want)
	}
	policyRequest := StartRequest{Task: "inspect", Profile: ManagedProfile{Model: "policy-model", Reasoning: "high"}}
	got, err = runtime.authoritativeArguments(policyRequest, "policy-session", false)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--permission-mode", "acceptEdits", "--print", "--output-format", "stream-json", "--verbose", "--model", "policy-model", "--effort", "high", "--session-id", "policy-session", "inspect"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude policy args=%#v, want %#v", got, want)
	}
}

func TestClaudeCodeSessionSteeringIsExplicitlyUnsupported(t *testing.T) {
	session := &claudeSession{}
	injected, err := session.Steer(context.Background(), "continue")
	if injected || !errors.Is(err, ErrClaudeCodeSteeringUnsupported) {
		t.Fatalf("injected=%v err=%v, want explicit unsupported error", injected, err)
	}

	execution := &ExecutionNode{sessions: map[string]Session{"attempt": session}}
	outcome := execution.steer(context.Background(), &SteeringCommand{
		Metadata: core.CommandMetadata{CommandID: "steer", Node: "node", AttemptID: "attempt"},
		Text:     "continue",
	})
	if outcome.State != CommandFailed || outcome.ErrorCode != "runtime_not_steerable" {
		t.Fatalf("steering outcome=%#v, want explicit unsupported failure", outcome)
	}
}

func TestClaudeBindingRejectsHarnessKindChangeAfterInventoryRefresh(t *testing.T) {
	claude := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1.0.0", Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady}
	envelope := WorkerEnvelope{WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", OriginalUserIntent: "inspect", HarnessInstance: claude}
	oldInventory := core.HarnessInventorySnapshot{Node: "node", Instances: []core.HarnessInstance{claude}, ObservedAt: time.Now().UTC()}
	if err := envelope.ValidateAgainstInventory("node", oldInventory); err != nil {
		t.Fatal(err)
	}
	refreshed := claude
	refreshed.Kind = core.HarnessCodex
	newInventory := core.HarnessInventorySnapshot{Node: "node", Instances: []core.HarnessInstance{refreshed}, ObservedAt: time.Now().UTC()}
	if err := envelope.ValidateAgainstInventory("node", newInventory); err == nil {
		t.Fatal("immutable Claude binding changed when inventory reused its HarnessInstance ID")
	}
}

func TestClaudeCodeRuntimeUnauthenticatedIsExplicit(t *testing.T) {
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady}
	_, err := (ClaudeCodeRuntime{}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
	if err == nil || !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestClaudeCodeRuntimeMissingExecutableIsExplicit(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-claude")
	codex := recordingRuntime{started: make(chan StartRequest, 1)}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	_, err := (RuntimeRouter{ACP: codex, Claude: ClaudeCodeRuntime{Command: missing}}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
	if err == nil || !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("err=%v", err)
	}
	select {
	case <-codex.started:
		t.Fatal("missing Claude executable fell back to Codex")
	default:
	}
}

func TestFakeClaudeCodeProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg != "-test.run=TestFakeClaudeCodeProcess" {
			continue
		}
		encoder := json.NewEncoder(os.Stdout)
		_ = encoder.Encode(map[string]any{"type": "system", "session_id": "native-session"})
		_ = encoder.Encode(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "native activity"}}})
		_ = encoder.Encode(map[string]any{"type": "result", "subtype": "success", "result": "native Claude result"})
		return
	}
}
