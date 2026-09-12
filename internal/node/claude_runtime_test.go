package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
