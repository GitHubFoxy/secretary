package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

)

func createFakeAntigravityServer(t *testing.T, scenario string) (string, string) {
	t.Helper()
	dir := t.TempDir()

	companionName := "localharness_external"
	execName := "agy_acp_server"
	if runtime.GOOS == "windows" {
		companionName = "localharness_external.exe"
		execName = "agy_acp_server.bat"
	}
	execPath := filepath.Join(dir, execName)
	companionPath := filepath.Join(dir, companionName)

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		script := fmt.Sprintf("@echo off\r\nset TEST_ANTIGRAVITY_FIXTURE=%s\r\n\"%s\" -test.run=^TestAntigravityFixtureProcess$ %%*\r\n", scenario, self)
		if err := os.WriteFile(execPath, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(companionPath, []byte(""), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := fmt.Sprintf("#!/bin/sh\nexport TEST_ANTIGRAVITY_FIXTURE=%q\nexec %q -test.run=^TestAntigravityFixtureProcess$ \"$@\"\n", scenario, self)
		if err := os.WriteFile(execPath, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(companionPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	return execPath, companionPath
}

func TestAntigravityRuntime_Configured(t *testing.T) {
	execPath, companionPath := createFakeAntigravityInstall(t)
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
		ACPRuntime: ACPRuntime{
			Environment: []string{"GEMINI_API_KEY=leak", "FOO=bar"},
		},
	}

	req := StartRequest{
		WorkerRef: "worker-1",
		Workspace: t.TempDir(),
		Profile: ManagedProfile{
			Name:      "test-prof",
			Model:     "gemini-3.8-flash-high",
			Reasoning: "high",
		},
	}

	configured, err := rt.configured(req)
	if err != nil {
		t.Fatalf("configured failed: %v", err)
	}

	if configured.Command != execPath {
		t.Fatalf("expected Command %q, got %q", execPath, configured.Command)
	}
	if !configured.ExactEnvironment {
		t.Fatal("expected ExactEnvironment to be true")
	}
	if !configured.PreferResumeSession {
		t.Fatal("expected PreferResumeSession to be true")
	}
	if configured.ModelSelection != "gemini-3.8-flash-high" {
		t.Fatalf("expected ModelSelection gemini-3.8-flash-high, got %q", configured.ModelSelection)
	}
	if configured.ReasoningSelection != "high" {
		t.Fatalf("expected ReasoningSelection high, got %q", configured.ReasoningSelection)
	}

	foundCompanion := false
	for _, env := range configured.Environment {
		if strings.HasPrefix(env, "GEMINI_API_KEY=") {
			t.Fatal("ambient GEMINI_API_KEY was not stripped")
		}
		if env == "ANTIGRAVITY_HARNESS_PATH="+companionPath {
			foundCompanion = true
		}
	}
	if !foundCompanion {
		t.Fatalf("ANTIGRAVITY_HARNESS_PATH not set to companion path %q", companionPath)
	}
}

func TestAntigravityRuntime_Configured_MissingBinaryOrCompanion(t *testing.T) {
	rt := AntigravityRuntime{
		AntigravityCommand: "/nonexistent/agy_acp_server",
	}
	_, err := rt.configured(StartRequest{Workspace: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestAntigravityRuntime_Start_NewSessionAndMCP(t *testing.T) {
	execPath, _ := createFakeAntigravityServer(t, "basic")
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mcpServers := []MCPServer{
		{Name: "test-tool", Command: "echo"},
	}

	session, err := rt.Start(ctx, StartRequest{
		WorkerRef:          "worker-start",
		Workspace:          t.TempDir(),
		DeferInitialPrompt: true,
		MCPServers:         mcpServers,
		Profile: ManagedProfile{
			Name:      "test-profile",
			Model:     "gemini-3.8-flash-high",
			Reasoning: "high",
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer session.Close()

	if session.ID() != "agy-sess-1" {
		t.Fatalf("expected session ID agy-sess-1, got %q", session.ID())
	}

	if err := session.Prompt(ctx, "Hello Antigravity"); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	select {
	case res := <-session.Result():
		if res.Status != "succeeded" {
			t.Fatalf("expected succeeded, got %q", res.Status)
		}
		if res.Summary != "Hello from Antigravity!" {
			t.Fatalf("expected 'Hello from Antigravity!', got %q", res.Summary)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for result")
	}
}

func TestAntigravityRuntime_Start_RejectsUnknownModelPin(t *testing.T) {
	execPath, _ := createFakeAntigravityServer(t, "basic")
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := rt.Start(ctx, StartRequest{
		WorkerRef:          "worker-pin",
		Workspace:          t.TempDir(),
		DeferInitialPrompt: true,
		Profile: ManagedProfile{
			Name:  "test-profile",
			Model: "unknown-super-gemini",
		},
	})
	if err == nil {
		t.Fatal("expected error rejecting unknown model pin")
	}
	if !strings.Contains(err.Error(), "unknown model pin") {
		t.Fatalf("expected 'unknown model pin' in error, got %v", err)
	}
}

func TestAntigravityRuntime_Resume_UsesSessionResume(t *testing.T) {
	execPath, _ := createFakeAntigravityServer(t, "resume")
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := rt.Resume(ctx, StartRequest{
		WorkerRef: "worker-resume",
		Workspace: t.TempDir(),
		Profile: ManagedProfile{
			Name: "test-profile",
		},
	}, "agy-sess-1")
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	defer session.Close()

	if session.ID() != "agy-sess-1" {
		t.Fatalf("expected session ID agy-sess-1, got %q", session.ID())
	}

	if err := session.Prompt(ctx, "Continue conversation"); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	select {
	case res := <-session.Result():
		if res.Status != "succeeded" {
			t.Fatalf("expected succeeded, got %q", res.Status)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for result")
	}
}

func TestAntigravityRuntime_InteractionRequest_NormalizedToUserInput(t *testing.T) {
	execPath, _ := createFakeAntigravityServer(t, "interaction")
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := rt.Start(ctx, StartRequest{
		WorkerRef:          "worker-interaction",
		Workspace:          t.TempDir(),
		DeferInitialPrompt: true,
		Profile: ManagedProfile{
			Name: "test-profile",
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer session.Close()

	promptErr := make(chan error, 1)
	go func() {
		promptErr <- session.Prompt(ctx, "Deploy please")
	}()

	var reqActivity Activity
	for {
		select {
		case act := <-session.Activity():
			if act.Kind == ActivityUserInput {
				reqActivity = act
				goto Answer
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for ActivityUserInput")
		}
	}

Answer:
	if reqActivity.Summary != "Select target deployment stage" {
		t.Fatalf("unexpected summary: %q", reqActivity.Summary)
	}

	// Try an invalid answer first -> must fail and not auto-approve
	invalidErr := session.(Responder).Respond(ctx, reqActivity.RequestID, "invalid_stage")
	if invalidErr == nil {
		t.Fatal("expected error for invalid interaction choice")
	}

	// Valid answer matching option name
	if err := session.(Responder).Respond(ctx, reqActivity.RequestID, "Staging"); err != nil {
		t.Fatalf("Respond failed: %v", err)
	}

	select {
	case res := <-session.Result():
		if res.Status != "succeeded" {
			t.Fatalf("expected succeeded, got %q", res.Status)
		}
		if res.Summary != "Deployed to staging successfully" {
			t.Fatalf("unexpected summary: %q", res.Summary)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for final result")
	}
}

func TestAntigravityRuntime_OrdinaryPermission_PreservesNativeOptionIDs(t *testing.T) {
	execPath, _ := createFakeAntigravityServer(t, "permission")
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := rt.Start(ctx, StartRequest{
		WorkerRef:          "worker-perm",
		Workspace:          t.TempDir(),
		DeferInitialPrompt: true,
		Profile: ManagedProfile{
			Name: "test-profile",
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer session.Close()

	promptErr := make(chan error, 1)
	go func() {
		promptErr <- session.Prompt(ctx, "Do something needing permission")
	}()

	var reqActivity Activity
	for {
		select {
		case act := <-session.Activity():
			if act.Kind == ActivityPermission {
				reqActivity = act
				goto AnswerPerm
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for ActivityPermission")
		}
	}

AnswerPerm:
	// Answer with exact native option ID "allow_once"
	if err := session.(Responder).Respond(ctx, reqActivity.RequestID, "allow_once"); err != nil {
		t.Fatalf("Respond failed: %v", err)
	}

	select {
	case res := <-session.Result():
		if res.Status != "succeeded" {
			t.Fatalf("expected succeeded, got %q", res.Status)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for final result")
	}
}

func TestAntigravityRuntime_ConfigOptionUpdate(t *testing.T) {
	execPath, _ := createFakeAntigravityServer(t, "config_update")
	rt := AntigravityRuntime{
		AntigravityCommand: execPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := rt.Start(ctx, StartRequest{
		WorkerRef:          "worker-cfg",
		Workspace:          t.TempDir(),
		DeferInitialPrompt: true,
		Profile: ManagedProfile{
			Name: "test-profile",
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer session.Close()

	acpSess, ok := session.(*acpSession)
	if !ok {
		t.Fatal("expected session to be *acpSession")
	}

	if err := session.Prompt(ctx, "Trigger update"); err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	select {
	case res := <-session.Result():
		if res.Status != "succeeded" {
			t.Fatalf("expected succeeded, got %q", res.Status)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for result")
	}

	opts := acpSess.ConfigOptions()
	foundUpdated := false
	for _, opt := range opts {
		if opt.ID == "model" && opt.CurrentValue == "gemini-3.8-pro" {
			foundUpdated = true
			break
		}
	}
	if !foundUpdated {
		t.Fatalf("expected updated model option gemini-3.8-pro, got %#v", opts)
	}
}

func TestAntigravityFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_ANTIGRAVITY_FIXTURE")
	if scenario == "" {
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	emit := func(v any) {
		data, _ := json.Marshal(v)
		_, _ = os.Stdout.Write(append(data, '\n'))
	}

	sampleConfigOptions := []map[string]any{
		{
			"id":           "model",
			"currentValue": "gemini-3.8-flash-high",
			"options": []map[string]string{
				{"value": "gemini-3.8-flash-high", "description": "Gemini 3.8 Flash"},
				{"value": "gemini-3.8-pro", "description": "Gemini 3.8 Pro"},
			},
		},
		{
			"id":           "reasoning_effort",
			"currentValue": "default",
			"options": []map[string]string{
				{"value": "default", "description": "Default"},
				{"value": "high", "description": "High"},
			},
		},
	}

	var sessionPromptID any
	for scanner.Scan() {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			emit(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": 1,
					"agentInfo": map[string]string{
						"name":    "antigravity-acp",
						"version": "1.3.0",
					},
					"agentCapabilities": map[string]any{
						"loadSession": true,
						"resume":      true,
					},
				},
			})

		case "session/new":
			emit(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"sessionId":     "agy-sess-1",
					"configOptions": sampleConfigOptions,
				},
			})

		case "session/resume":
			if scenario != "resume" {
				os.Exit(3)
			}
			emit(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"sessionId":     "agy-sess-1",
					"configOptions": sampleConfigOptions,
				},
			})

		case "session/set_config_option":
			var p struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			_ = json.Unmarshal(req.Params, &p)
			emit(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"configOptions": []map[string]string{
						{"id": p.ConfigID, "currentValue": p.Value},
					},
				},
			})

		case "session/cancel":
			// cancellation handled

		case "session/prompt":
			sessionPromptID = req.ID

			if scenario == "interaction" {
				// Server requests permission with interaction_ tool call ID
				emit(map[string]any{
					"jsonrpc": "2.0",
					"id":      999,
					"method":  "session/request_permission",
					"params": map[string]any{
						"toolCall": map[string]any{
							"toolCallId": "interaction_deploy",
							"title":      "Select target deployment stage",
						},
						"options": []map[string]string{
							{"optionId": "stage_staging", "name": "Staging", "kind": "stage"},
							{"optionId": "stage_prod", "name": "Production", "kind": "stage"},
						},
					},
				})
				continue
			}

			if scenario == "permission" {
				emit(map[string]any{
					"jsonrpc": "2.0",
					"id":      888,
					"method":  "session/request_permission",
					"params": map[string]any{
						"options": []map[string]string{
							{"optionId": "allow_once", "kind": "allow_once"},
							{"optionId": "deny", "kind": "reject_once"},
						},
					},
				})
				continue
			}

			if scenario == "config_update" {
				updatedOptions := []map[string]any{
					{
						"id":           "model",
						"currentValue": "gemini-3.8-pro",
						"options": []map[string]string{
							{"value": "gemini-3.8-flash-high", "description": "Gemini 3.8 Flash"},
							{"value": "gemini-3.8-pro", "description": "Gemini 3.8 Pro"},
						},
					},
				}
				emit(map[string]any{
					"jsonrpc": "2.0",
					"method":  "session/update",
					"params": map[string]any{
						"sessionId": "agy-sess-1",
						"update": map[string]any{
							"sessionUpdate": "config_option_update",
							"configOptions": updatedOptions,
						},
					},
				})
				emit(map[string]any{
					"jsonrpc": "2.0",
					"method":  "session/update",
					"params": map[string]any{
						"sessionId": "agy-sess-1",
						"update": map[string]any{
							"sessionUpdate": "agent_message_chunk",
							"content": map[string]string{
								"type": "text",
								"text": "Config updated.",
							},
						},
					},
				})
				emit(map[string]any{
					"jsonrpc": "2.0",
					"id":      sessionPromptID,
					"result":  map[string]string{"stopReason": "end_turn"},
				})
				continue
			}

			emit(map[string]any{
				"jsonrpc": "2.0",
				"method":  "session/update",
				"params": map[string]any{
					"sessionId": "agy-sess-1",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content": map[string]string{
							"type": "text",
							"text": "Hello from Antigravity!",
						},
					},
				},
			})
			emit(map[string]any{
				"jsonrpc": "2.0",
				"id":      sessionPromptID,
				"result":  map[string]string{"stopReason": "end_turn"},
			})

		default:
			// Check if this is a response to our server request
			if req.ID != nil {
				var resp struct {
					Result struct {
						Outcome struct {
							Outcome  string `json:"outcome"`
							OptionID string `json:"optionId"`
						} `json:"outcome"`
					} `json:"result"`
				}
				if json.Unmarshal(scanner.Bytes(), &resp) == nil {
					if scenario == "interaction" && resp.Result.Outcome.OptionID == "stage_staging" {
						emit(map[string]any{
							"jsonrpc": "2.0",
							"method":  "session/update",
							"params": map[string]any{
								"sessionId": "agy-sess-1",
								"update": map[string]any{
									"sessionUpdate": "agent_message_chunk",
									"content": map[string]string{
										"type": "text",
										"text": "Deployed to staging successfully",
									},
								},
							},
						})
						emit(map[string]any{
							"jsonrpc": "2.0",
							"id":      sessionPromptID,
							"result":  map[string]string{"stopReason": "end_turn"},
						})
					} else if scenario == "permission" && resp.Result.Outcome.OptionID == "allow_once" {
						emit(map[string]any{
							"jsonrpc": "2.0",
							"method":  "session/update",
							"params": map[string]any{
								"sessionId": "agy-sess-1",
								"update": map[string]any{
									"sessionUpdate": "agent_message_chunk",
									"content": map[string]string{
										"type": "text",
										"text": "Permission granted, work completed.",
									},
								},
							},
						})
						emit(map[string]any{
							"jsonrpc": "2.0",
							"id":      sessionPromptID,
							"result":  map[string]string{"stopReason": "end_turn"},
						})
					}
				}
			}
		}
	}
	os.Exit(0)
}
