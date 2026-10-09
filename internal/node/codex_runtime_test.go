package node

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCodexSessionReturnsOnlyFinalAndRejectsEmptyTerminal(t *testing.T) {
	for _, scenario := range []string{"final", "empty", "commentary", "max_tokens"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runtime := CodexRuntime{ACPRuntime: ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestCodexFixtureProcess$"}, Environment: []string{"TEST_CODEX_FIXTURE=" + scenario}}}
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "fixture", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "UNIQUE_PROFILE_MARKER", Runtime: "codex", Model: "fixture-model", Reasoning: "high"}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Prompt(ctx, "hello"); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result():
				if scenario == "final" {
					if result.Status != "succeeded" || result.Summary != "Краткий ответ." {
						t.Fatalf("result=%+v", result)
					}
				} else if result.Status != "failed" {
					t.Fatalf("nonfinal accepted: %+v", result)
				}
			case <-ctx.Done():
				t.Fatal("no terminal result")
			}
		})
	}
}

func TestCodexFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_CODEX_FIXTURE")
	if scenario == "" {
		return
	}
	var overrides map[string]any
	if json.Unmarshal([]byte(os.Getenv("CODEX_CONFIG")), &overrides) != nil || !strings.Contains(overrides["developer_instructions"].(string), "UNIQUE_PROFILE_MARKER") {
		os.Exit(2)
	}
	var pendingPrompt any
	scanner := bufio.NewScanner(os.Stdin)
	emit := func(v any) { data, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(data, '\n')) }
	for scanner.Scan() {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			} `json:"params"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "fixture-codex"}
		case "session/set_config_option":
			result = map[string]any{"configOptions": []map[string]string{{"id": req.Params.ConfigID, "currentValue": req.Params.Value}}}
		case "_session/steering":
			outcome := "injected"
			if scenario == "steer-new-turn" {
				outcome = "startedNewTurn"
			}
			emit(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]string{"outcome": outcome}})
			emit(map[string]any{"jsonrpc": "2.0", "id": pendingPrompt, "result": map[string]string{"stopReason": "end_turn"}})
			continue
		case "session/cancel":
			continue
		case "session/prompt":
			if strings.HasPrefix(scenario, "steer-") {
				pendingPrompt = req.ID
				emit(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "gate", "content": map[string]string{"type": "text", "text": "gate"}, "_meta": map[string]any{"codex": map[string]string{"phase": "commentary"}}}}})
				continue
			}
			chunk := func(id, phase, text string) {
				emit(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": id, "content": map[string]string{"type": "text", "text": text}, "_meta": map[string]any{"codex": map[string]string{"phase": phase}}}}})
			}
			if scenario != "empty" {
				chunk("progress", "commentary", "Сейчас проверю.")
			}
			if scenario == "final" || scenario == "max_tokens" {
				chunk("answer", "final_answer", "Краткий ответ.")
			}
			reason := "end_turn"
			if scenario == "max_tokens" {
				reason = "max_tokens"
			}
			result = map[string]string{"stopReason": reason}
		}
		emit(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func TestCodexNativeProfileFinalResume(t *testing.T) {
	if os.Getenv("SECRETARY_NATIVE_CODEX_TEST") != "1" {
		t.Skip("explicit native model call opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	workspace := t.TempDir()
	profile := ManagedProfile{Name: "worker", Content: "When asked for the profile marker, reply exactly PHASE5_CODEX_NATIVE_73. Keep answers short. Do not call tools unless explicitly asked.", Runtime: "codex"}
	runtime := CodexRuntime{ACPRuntime: ACPRuntime{Command: "codex-acp"}}
	request := StartRequest{WorkerRef: "native-proof", Workspace: workspace, Profile: profile, DeferInitialPrompt: true}
	session, err := runtime.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	id := session.ID()
	if id == "" {
		t.Fatal("native identity missing")
	}
	for phase := 0; phase < 2; phase++ {
		prompt := "What is the profile marker? Remember the additional marker FOLLOWUP_91."
		if phase == 1 {
			prompt = "What was the additional marker in my previous message? Reply only the marker."
		}
		done := make(chan error, 1)
		go func() { done <- session.Prompt(ctx, prompt) }()
		var result Result
		returned := false
		for result.Status == "" || !returned {
			select {
			case <-session.Activity():
			case result = <-session.Result():
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
				returned = true
			case <-ctx.Done():
				t.Fatal("native prompt timed out")
			}
		}
		want := "PHASE5_CODEX_NATIVE_73"
		if phase == 1 {
			want = "FOLLOWUP_91"
		}
		if result.Status != "succeeded" || !strings.Contains(result.Summary, want) {
			t.Fatalf("native phase=%d status=%s marker=%t", phase, result.Status, strings.Contains(result.Summary, want))
		}
		t.Logf("native phase=%d session=%s marker=%s final=true", phase, id, want)
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		if phase == 0 {
			session, err = runtime.Resume(ctx, request, id)
			if err != nil {
				t.Fatal(err)
			}
			if session.ID() != id {
				t.Fatal("native resume changed identity")
			}
		}
	}
}

func TestCodexSteeringRejectsIdleAndNativeNewTurn(t *testing.T) {
	for _, scenario := range []string{"steer-injected", "steer-new-turn", "steer-idle"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runtime := CodexRuntime{ACPRuntime: ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestCodexFixtureProcess$"}, Environment: []string{"TEST_CODEX_FIXTURE=" + scenario}}}
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "steer", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "worker", Content: "UNIQUE_PROFILE_MARKER"}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if scenario != "steer-idle" {
				go func() { _ = session.Prompt(ctx, "work") }()
				select {
				case <-session.Activity():
				case <-ctx.Done():
					t.Fatal("prompt gate missing")
				}
			}
			injected, err := session.Steer(ctx, "marker")
			if scenario == "steer-injected" {
				if !injected || err != nil {
					t.Fatalf("injected=%t err=%v", injected, err)
				}
			} else if injected || err == nil {
				t.Fatalf("idle/new turn reported injected=%t err=%v", injected, err)
			}
		})
	}
}

func TestCodexNativeSteeringDuringSafeTool(t *testing.T) {
	if os.Getenv("SECRETARY_NATIVE_CODEX_TEST") != "1" {
		t.Skip("explicit native model call opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtime := CodexRuntime{ACPRuntime: ACPRuntime{Command: "codex-acp"}}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "native-steering-proof", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "worker", Runtime: "codex", Content: "Follow the user's instructions precisely. Use a terminal tool when asked. Do not read or modify any files. Keep the final answer to one marker."}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	done := make(chan error, 1)
	go func() {
		done <- session.Prompt(ctx, "Run the safe terminal command sleep 12. After it completes, reply ORIGINAL_NATIVE_42.")
	}()
	injected := false
	var result Result
	returned := false
	for result.Status == "" || !returned {
		select {
		case activity := <-session.Activity():
			if activity.Kind == ActivityPermission {
				if err := session.(Responder).Respond(ctx, activity.RequestID, "approved"); err != nil {
					t.Fatal(err)
				}
			}
			if !injected && activity.Kind == ActivityToolCall && activity.Status == "in_progress" {
				var steerErr error
				injected, steerErr = session.Steer(ctx, "Change the final answer to STEER_APPLIED_NATIVE_87. Do not start a second command.")
				if steerErr != nil || !injected {
					t.Fatalf("native steering injected=%t err=%v", injected, steerErr)
				}
			}
		case result = <-session.Result():
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			returned = true
		case <-ctx.Done():
			t.Fatal("native steering timed out")
		}
	}
	if !injected || result.Status != "succeeded" || !strings.Contains(result.Summary, "STEER_APPLIED_NATIVE_87") {
		t.Fatalf("native steering injected=%t status=%s marker=%t", injected, result.Status, strings.Contains(result.Summary, "STEER_APPLIED_NATIVE_87"))
	}
	t.Logf("native session=%s injected=true applied_before_terminal=true prompt_calls=1 marker=STEER_APPLIED_NATIVE_87", session.ID())
}
