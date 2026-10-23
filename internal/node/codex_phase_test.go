package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func codexPhaseFixtureChunk(messageID, text, phase string) map[string]any {
	update := map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
	}
	if phase != "" {
		update["_meta"] = map[string]any{"jetbrains": map[string]any{"air": map[string]any{"version": 1, "phase": phase}}}
	}
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": update}}
}

func codexPhaseFixtureLegacyChunk(messageID, text, phase string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
		"_meta":         map[string]any{"codex": map[string]any{"phase": phase}},
	}}}
}

func codexPhaseFixtureContradictoryChunk(messageID, text string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
		"_meta": map[string]any{
			"codex":     map[string]any{"phase": "commentary"},
			"jetbrains": map[string]any{"air": map[string]any{"version": 1, "phase": "final_answer"}},
		},
	}}}
}

func codexPhaseFixtureToolCall() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "call-1",
		"title":         "read file",
		"kind":          "read",
		"status":        "in_progress",
	}}}
}

func TestCodexPhaseFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_CODEX_PHASE_FIXTURE")
	if scenario == "" {
		return
	}
	capture := os.Getenv("TEST_CODEX_PHASE_CAPTURE")
	scanner := bufio.NewScanner(os.Stdin)
	prompts := 0
	emit := func(v any) { data, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(data, '\n')) }
	for scanner.Scan() {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			if capture != "" {
				_ = os.WriteFile(capture, req.Params, 0600)
			}
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"version": "2.2.2"}}
		case "session/new":
			result = map[string]any{"sessionId": "fixture-codex-phase"}
		case "session/prompt":
			switch scenario {
			case "final-parts":
				emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готов", "final_answer"))
				emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "о", "final_answer"))
			case "commentary-then-final":
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "Нач", "commentary"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "ина", "commentary"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "ю проверку.", "commentary"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "готов", "final_answer"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "о", "final_answer"))
			case "tool-between":
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "Проверяю.", "commentary"))
				emit(codexPhaseFixtureToolCall())
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "готово", "final_answer"))
			case "final-then-tool":
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "готово", "final_answer"))
				emit(codexPhaseFixtureToolCall())
			case "no-phase":
				emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готово", ""))
			case "contradictory":
				emit(codexPhaseFixtureContradictoryChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готово"))
			case "legacy-namespace":
				emit(codexPhaseFixtureLegacyChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готово", "final_answer"))
			case "cancelled":
				result = map[string]any{"stopReason": "cancelled"}
			case "incomplete":
				continue
			case "two-turns":
				prompts++
				if prompts == 1 {
					emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "первый", "final_answer"))
				} else {
					emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae72", "второй", "final_answer"))
				}
			}
			if scenario != "cancelled" && scenario != "incomplete" {
				result = map[string]any{"stopReason": "end_turn"}
			}
		}
		emit(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func codexPhaseFixtureRuntime(t *testing.T, scenario string) (CodexRuntime, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "initialize.json")
	runtime := CodexRuntime{ACPRuntime: ACPRuntime{
		Command:     os.Args[0],
		Arguments:   []string{"-test.run=^TestCodexPhaseFixtureProcess$"},
		Environment: []string{"TEST_CODEX_PHASE_FIXTURE=" + scenario, "TEST_CODEX_PHASE_CAPTURE=" + capture},
	}}
	return runtime, capture
}

func TestCodexPhaseAcceptsPinnedAdapterFinalAnswer(t *testing.T) {
	for _, scenario := range []string{"final-parts", "commentary-then-final", "tool-between", "legacy-namespace"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			runtime, _ := codexPhaseFixtureRuntime(t, scenario)
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result():
				if result.Status != "succeeded" {
					t.Fatalf("scenario %s result=%+v", scenario, result)
				}
				if !strings.Contains(result.Summary, "готово") && !strings.Contains(result.Summary, "готов") {
					t.Fatalf("scenario %s summary=%q", scenario, result.Summary)
				}
				if scenario == "commentary-then-final" && strings.Contains(result.Summary, "Провер") {
					t.Fatalf("commentary was accepted as the final answer: %q", result.Summary)
				}
			case <-ctx.Done():
				t.Fatal("no terminal result")
			}
		})
	}
}

func TestCodexPhaseRejectsUnauthoritativeTerminalAnswer(t *testing.T) {
	for _, scenario := range []string{"no-phase", "contradictory", "final-then-tool"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			runtime, _ := codexPhaseFixtureRuntime(t, scenario)
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result():
				if result.Status != "failed" {
					t.Fatalf("scenario %s accepted a non-authoritative answer: %+v", scenario, result)
				}
				if !strings.Contains(result.Summary, "Terminal answer unavailable") {
					t.Fatalf("scenario %s summary=%q", scenario, result.Summary)
				}
			case <-ctx.Done():
				t.Fatal("no terminal result")
			}
		})
	}
}

func TestCodexPhaseCanceledAndIncompleteTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime, _ := codexPhaseFixtureRuntime(t, "cancelled")
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "canceled" {
			t.Fatalf("cancelled turn result=%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("no terminal result")
	}

	incompleteCtx, incompleteCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer incompleteCancel()
	incomplete, _ := codexPhaseFixtureRuntime(t, "incomplete")
	incompleteSession, err := incomplete.Start(incompleteCtx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer incompleteSession.Close()
	if err := incompleteSession.Prompt(incompleteCtx, "Ответь одним словом: готово"); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case result := <-incompleteSession.Result():
		if result.Status == "succeeded" {
			t.Fatalf("incomplete turn produced a final answer: %+v", result)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestCodexPhaseTwoTurnsInOneSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, _ := codexPhaseFixtureRuntime(t, "two-turns")
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for turn := 0; turn < 2; turn++ {
		if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-session.Result():
			if result.Status != "succeeded" {
				t.Fatalf("turn %d result=%+v", turn, result)
			}
		case <-ctx.Done():
			t.Fatalf("turn %d produced no terminal result", turn)
		}
	}
}

func TestCodexPhaseNegotiationDeclaresExtensionNamespace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime, capture := codexPhaseFixtureRuntime(t, "final-parts")
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	recorded, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		ClientCapabilities map[string]any `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(recorded, &request); err != nil {
		t.Fatal(err)
	}
	namespace, _ := request.ClientCapabilities["_meta"].(map[string]any)
	extension, _ := namespace[codexPhaseNamespace].(map[string]any)
	air, _ := extension[codexPhaseExtension].(map[string]any)
	if air == nil {
		t.Fatalf("Codex client did not declare the phase extension namespace: %s", recorded)
	}
	if version, _ := air["version"].(float64); version != 1 {
		t.Fatalf("phase extension version=%v: %s", air["version"], recorded)
	}
	if _, ok := request.ClientCapabilities["elicitation"]; !ok {
		t.Fatalf("Codex client lost the elicitation capability: %s", recorded)
	}
}

func TestNonCodexRuntimeDoesNotDeclarePhaseExtension(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capture := filepath.Join(t.TempDir(), "initialize.json")
	runtime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestCodexPhaseFixtureProcess$"}, Environment: []string{"TEST_CODEX_PHASE_FIXTURE=final-parts", "TEST_CODEX_PHASE_CAPTURE=" + capture}}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "plain", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "worker", Content: "MARKER"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	recorded, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		ClientCapabilities map[string]any `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(recorded, &request); err != nil {
		t.Fatal(err)
	}
	if _, ok := request.ClientCapabilities["_meta"]; ok {
		t.Fatalf("plain ACP client declared the Codex phase extension: %s", recorded)
	}
}
