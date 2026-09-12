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

func TestClaudeCodeRuntimeRejectsConfiguredPolicyAndUnknownArguments(t *testing.T) {
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	unsafe := []string{
		"--add-dir", "--add-dir=outside",
		"--permission-mode", "--permission-mode=acceptEdits", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--permission-prompts",
		"--tools", "--tools=Bash", "--allowedTools", "--allowed-tools", "--disallowedTools", "--disallowed-tools", "--restricted", "--settings", "--settings=config.json",
		"--fallback-model", "--fallback-model=attacker", "--model", "--model=attacker", "-m", "-mattacker", "--effort", "--effort=low",
		"--session-id", "--session-id=attacker", "--resume", "--resume=attacker", "-r", "-rattacker", "--continue", "-c", "--fork-session", "--from-pr", "--no-session-persistence", "--",
		"--print", "-p", "-pattacker", "--verbose", "--output-format", "--output-format=text", "--input-format", "--stream-json", "--include-partial-messages", "--json-schema", "--replay-user-messages",
		"--permission-prompt-tool", "--thinking", "--max-thinking-tokens", "--mcp-config", "--plugin-dir", "--system-prompt", "--max-turns", "--unknown-flag", "--disable-slash-commands=true", "attacker-prompt",
	}
	for _, argument := range unsafe {
		t.Run(argument, func(t *testing.T) {
			runtime := ClaudeCodeRuntime{Arguments: []string{argument}}
			_, err := runtime.Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect", Model: "claude-sonnet", Reasoning: "high"})
			if !errors.Is(err, ErrClaudeCodeConfiguration) {
				t.Fatalf("argument %q err=%v, want configuration error", argument, err)
			}
		})
	}
}

func TestClaudeCodeRuntimeAllowsOnlyDocumentedHarmlessStaticArgument(t *testing.T) {
	runtime := ClaudeCodeRuntime{Arguments: []string{"--disable-slash-commands"}}
	request := StartRequest{Task: "inspect", Model: "claude-sonnet", Reasoning: "extended"}
	got, err := runtime.authoritativeArguments(request, "session-id", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--disable-slash-commands", "--print", "--output-format", "stream-json", "--verbose", "--model", "claude-sonnet", "--effort", "extended", "--session-id", "session-id", "inspect"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude CLI args=%#v, want %#v", got, want)
	}
	got, err = runtime.authoritativeArguments(request, "saved-session", true)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--disable-slash-commands", "--print", "--output-format", "stream-json", "--verbose", "--model", "claude-sonnet", "--effort", "extended", "--resume", "saved-session", "inspect"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude resume args=%#v, want %#v", got, want)
	}
}

func TestClaudeCodeRuntimeRejectsConfigurationBeforeStartingProcess(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	launcher := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\ntouch \"$CLAUDE_STARTED\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	_, err := (ClaudeCodeRuntime{Command: launcher, Arguments: []string{"--permission-mode", "acceptEdits"}, Environment: []string{"CLAUDE_STARTED=" + started}}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
	if !errors.Is(err, ErrClaudeCodeConfiguration) {
		t.Fatalf("err=%v, want configuration error", err)
	}
	if _, err := os.Stat(started); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Claude process started, stat err=%v", err)
	}
}

func TestClaudeCodeRuntimeRejectsCapabilityMismatch(t *testing.T) {
	for _, capability := range []core.ExecutionCapability{core.CapabilitySteering, core.CapabilityApprovals} {
		t.Run(string(capability), func(t *testing.T) {
			instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{capability}}}
			_, err := (ClaudeCodeRuntime{}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
			if !errors.Is(err, ErrClaudeCodeConfiguration) {
				t.Fatalf("capability %q err=%v, want configuration error", capability, err)
			}
		})
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
