//go:build windows

package node

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func claudeWindowsCmdFixture(t *testing.T, discovery string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude.cmd")
	script := "@echo off\r\n" +
		"if \"%~1\"==\"--version\" (echo 2.1.306& exit /b 0)\r\n" +
		"if \"%~1\"==\"auth\" (echo {\"loggedIn\":true,\"authMethod\":\"credential\"}& exit /b 0)\r\n" +
		"if not \"%~1\"==\"--print\" exit /b 15\r\n" +
		"if not \"%CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY%\"==\"1\" exit /b 16\r\n" +
		"set /p REQUEST=\r\n" +
		discovery + "\r\n"
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClaudeWindowsCmdNativeDiscoveryAndPin(t *testing.T) {
	response := `{"type":"control_response","response":{"request_id":"secretary-model-inventory","subtype":"success","response":{"models":[{"value":"haiku","resolvedModel":"claude-haiku-5-5"}]}}}`
	path := claudeWindowsCmdFixture(t, "echo "+response)
	spec := DefaultClaudeCodeProbeSpec()
	spec.Binary = path
	result := (HarnessProbe{Node: "win-node", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
	if !result.Available() || !result.Instance.SupportsModel("claude-haiku-5-5") {
		t.Fatalf("Windows .cmd discovery failed: status=%s code=%s models=%v", result.Instance.Status, result.ErrorCode, result.Instance.ModelIDs)
	}
	if len(result.Instance.ModelIDs) != 1 {
		t.Fatalf("Windows native inventory=%v", result.Instance.ModelIDs)
	}
}

func TestClaudeWindowsCmdTimeoutKillsChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "orphan-marker")
	child := filepath.Join(t.TempDir(), "child.cmd")
	if err := os.WriteFile(child, []byte("@echo off\r\nping -n 5 127.0.0.1 >nul\r\necho orphan > \""+marker+"\"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := claudeWindowsCmdFixture(t, "call \""+child+"\"")
	spec := DefaultClaudeCodeProbeSpec()
	spec.Binary = path
	spec.StepTimeout = 100 * time.Millisecond
	start := time.Now()
	result := (HarnessProbe{Node: "win-node", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
	if result.Instance.Status != core.HarnessDegraded || result.ErrorCode != "native_models_probe_failed" || len(result.Instance.ModelIDs) != 0 {
		t.Fatalf("timed-out Windows discovery must fail closed: status=%s code=%s models=%v", result.Instance.Status, result.ErrorCode, result.Instance.ModelIDs)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("Windows native timeout exceeded bound")
	}
	time.Sleep(5 * time.Second)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Windows discovery descendant survived timeout: %v", err)
	}
	if strings.Contains(result.Err.Error(), "ANTHROPIC_AUTH_TOKEN") {
		t.Fatal("native failure diagnostics exposed credential field")
	}
}

func TestClaudeWindowsCmdCannotSpawnBeforeJobAssignment(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "immediate-child-marker")
	path := claudeWindowsCmdFixture(t, "echo spawned > \""+marker+"\"")
	original := claudeProbeAssignJob
	claudeProbeAssignJob = func(windows.Handle, windows.Handle) error {
		return errors.New("synthetic job assignment denial")
	}
	defer func() { claudeProbeAssignJob = original }()
	spec := DefaultClaudeCodeProbeSpec()
	spec.Binary = path
	result := (HarnessProbe{Node: "win-node", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
	if result.Instance.Status != core.HarnessDegraded || result.ErrorCode != "native_models_probe_failed" || len(result.Instance.ModelIDs) != 0 {
		t.Fatalf("failed assignment must fail closed: status=%s code=%s models=%v", result.Instance.Status, result.ErrorCode, result.Instance.ModelIDs)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("uncontained native CLI ran before job assignment: %v", err)
	}
}
