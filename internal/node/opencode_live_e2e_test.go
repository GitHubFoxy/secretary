package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in paid acceptance. Requires an owner-selected native store that was
// explicitly logged in; never falls back to or imports the personal OpenCode store.
func TestOpenCodeAuthenticatedSecretaryMCP(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_LIVE_E2E") != "1" {
		t.Skip("set SECRETARY_OPENCODE_LIVE_E2E=1 for authenticated OpenCode MCP acceptance")
	}
	dataHome := strings.TrimSpace(os.Getenv("SECRETARY_OPENCODE_LIVE_DATA_HOME"))
	if !filepath.IsAbs(dataHome) {
		t.Skip("set SECRETARY_OPENCODE_LIVE_DATA_HOME to an explicitly logged-in private store")
	}
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	state := filepath.Join(root, "mcp-state")
	for _, dir := range []string{workspace, state} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal("could not prepare private acceptance directory")
		}
	}
	t.Setenv("TEST_OPENCODE_WRAPPER", "1")
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal("private lifecycle command unavailable")
	}
	mcp := MCPServer{Name: "secretary", Command: command, Args: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "mcp-server"}, Env: []MCPEnv{
		{Name: "SECRETARY_MCP_DATA_DIR", Value: state},
		{Name: "SECRETARY_MCP_CAPABILITY", Value: "native-fixture-capability"},
	}}
	profile := ManagedProfile{Name: "secretary", Content: "Use only the server-owned list_workers tool. Call it exactly once, then respond with exactly OK. Do not include tool output or other text in your answer.", Model: "openai/gpt-6.1-sol", Reasoning: "xhigh"}
	profile.Hash = HashProfile(profile.Content, nil, profile.Model, profile.Reasoning)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	session, err := (OpenCodeRuntime{DataHome: dataHome}).Start(ctx, StartRequest{WorkerRef: "private-live-mcp-acceptance", Workspace: workspace, Profile: profile, MCPServers: []MCPServer{mcp}, DeferInitialPrompt: true})
	if err != nil {
		for _, marker := range []string{"model not found", "mode", "invalid params", "variant", "effort", "initialize", "process stopped", "deadline", "unauthorized", "401"} {
			if strings.Contains(strings.ToLower(err.Error()), marker) {
				t.Logf("safe startup error marker: %s", marker)
			}
		}
		t.Fatal("authenticated native Secretary startup failed")
	}
	defer session.Close()
	done := make(chan error, 1)
	go func() { done <- session.Prompt(ctx, "Call list_workers once, then reply with exactly OK.") }()
	for {
		select {
		case activity, ok := <-session.Activity():
			if ok && activity.Kind == ActivityPermission {
				t.Fatal("unexpected permission request in isolated lifecycle acceptance")
			}
		case result, ok := <-session.Result():
			t.Logf("safe terminal metadata: present=%t status=%s summary_bytes=%d expected_ok=%t", ok, result.Status, len(result.Summary), strings.TrimSpace(result.Summary) == "OK")
			data, err := os.ReadFile(filepath.Join(state, "fixture-status.json"))
			var status struct {
				Scoped bool `json:"scoped_environment"`
				Listed bool `json:"tools_listed"`
				Called bool `json:"tool_called"`
			}
			decoded := err == nil && json.Unmarshal(data, &status) == nil
			t.Logf("safe MCP metadata: decoded=%t scoped=%t listed=%t called=%t", decoded, status.Scoped, status.Listed, status.Called)
			if !ok || result.Status != "succeeded" || strings.TrimSpace(result.Summary) != "OK" {
				for _, marker := range []string{"unauthorized", "401", "unsupported", "not supported", "model", "tool", "failed", "low", "reasoning"} {
					if strings.Contains(strings.ToLower(result.Summary), marker) {
						t.Logf("safe terminal marker: %s", marker)
					}
				}
				t.Fatal("authenticated lifecycle acceptance did not return the expected terminal Result")
			}
			if !decoded || !status.Scoped || !status.Listed || !status.Called {
				t.Fatal("authenticated model did not complete the scoped MCP lifecycle call")
			}
			return
		case err := <-done:
			if err != nil {
				t.Fatal("authenticated native prompt failed")
			}
		case <-ctx.Done():
			t.Fatal("authenticated lifecycle acceptance timed out")
		}
	}
}

func TestOpenCodeAuthenticatedWorkerReadAndResume(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_LIVE_E2E") != "1" {
		t.Skip("set SECRETARY_OPENCODE_LIVE_E2E=1 for authenticated Worker acceptance")
	}
	dataHome := strings.TrimSpace(os.Getenv("SECRETARY_OPENCODE_LIVE_DATA_HOME"))
	if !filepath.IsAbs(dataHome) {
		t.Skip("set SECRETARY_OPENCODE_LIVE_DATA_HOME to an explicitly logged-in private store")
	}
	workspace := t.TempDir()
	var instructionNonce [16]byte
	if _, err := rand.Read(instructionNonce[:]); err != nil {
		t.Fatal("private instruction nonce unavailable")
	}
	marker := hex.EncodeToString(instructionNonce[:])
	profile := ManagedProfile{Name: "worker", Content: openCodeAuthenticatedWorkerInstructions(marker), AllowTools: []string{"read"}, Model: "openai/gpt-6-luna", Reasoning: "xhigh"}
	profile.Hash = HashProfile(profile.Content, nil, profile.Model, profile.Reasoning)
	request := StartRequest{WorkerRef: "private-live-worker-acceptance", Workspace: workspace, Profile: profile, DeferInitialPrompt: true}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	runtime := OpenCodeRuntime{DataHome: dataHome}
	session, err := runtime.Start(ctx, request)
	if err != nil {
		t.Fatal("authenticated default-model Worker startup failed")
	}
	defer func() {
		if session != nil {
			_ = session.Close()
		}
	}()
	id := session.ID()
	firstContent := ""
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := session.Close(); err != nil {
				t.Fatal("private Worker close failed")
			}
			session, err = runtime.Resume(ctx, request, id)
			if err != nil {
				for _, marker := range []string{"model", "mode", "effort", "not found", "not supported", "directory", "session", "deadline", "process stopped", "internal", "params", "method", "server"} {
					if strings.Contains(strings.ToLower(err.Error()), marker) {
						t.Logf("safe resume error marker: %s", marker)
					}
				}
				t.Fatal("native Worker resume startup failed")
			}
			if session.ID() != id {
				t.Fatal("native Worker resume did not preserve its session")
			}
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			t.Fatal(err)
		}
		current := hex.EncodeToString(nonce[:])
		expected := current + "|" + marker
		if attempt == 0 {
			firstContent = current
		} else {
			expected = current + "|" + firstContent + "|" + marker
		}
		if err := os.WriteFile(filepath.Join(workspace, "acceptance.txt"), []byte(current), 0o600); err != nil {
			t.Fatal("private current file unavailable")
		}
		done := make(chan error, 1)
		go func() {
			done <- session.Prompt(ctx, "Read acceptance.txt now, even if it was read earlier. Follow your system instructions for the answer format.")
		}()
		completed := false
		for !completed {
			select {
			case activity, ok := <-session.Activity():
				if ok && activity.Kind == ActivityPermission {
					t.Fatal("unexpected read permission request")
				}
			case result, ok := <-session.Result():
				metadata := inspectOpenCodeWorkerAnswer(result.Summary, current, firstContent, marker)
				t.Logf("safe Worker metadata: attempt=%d present=%t status=%s summary_bytes=%d expected_bytes=%d expected_content=%t actual_current_present=%t original_first_present=%t profile_marker_present=%t literal_CURRENT=%t literal_FIRST=%t separator_count=%d", attempt, ok, result.Status, metadata.SummaryBytes, len(expected), result.Summary == expected, metadata.ActualCurrentPresent, metadata.OriginalFirstPresent, metadata.ProfileMarkerPresent, metadata.LiteralCurrent, metadata.LiteralFirst, metadata.SeparatorCount)
				if !ok || result.Status != "succeeded" || result.Summary != expected {
					t.Fatal("private Worker did not return actual current file contents")
				}
				t.Logf("safe Worker acceptance: attempt=%d actual_read=true managed_instruction=true original_history=%t", attempt, attempt > 0)
				completed = true
			case err := <-done:
				if err != nil {
					t.Fatal("private Worker prompt failed")
				}
			case <-ctx.Done():
				t.Fatal("private Worker acceptance timed out")
			}
		}
	}
	if err := session.Close(); err != nil {
		t.Fatal("private Worker final close failed")
	}
}
