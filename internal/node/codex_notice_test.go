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

const codexNoticeText = "Warning: Code Mode is unavailable because failed to spawn code-mode host /private/tmp/codex-code-mode-host: host executable was not found. Code mode will fail closed; enable `features.code_mode_host` and install `codex-code-mode-host`.\n\n"

func codexNoticeFrame(text string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-notice", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": text},
	}}}
}

func codexFinalFrame(messageID, text string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-notice", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
		"_meta":         map[string]any{"jetbrains": map[string]any{"air": map[string]any{"version": 1, "phase": "final_answer"}}},
	}}}
}

func codexIdlessFinalFrame(text string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-notice", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content":       map[string]any{"type": "text", "text": text},
		"_meta":         map[string]any{"jetbrains": map[string]any{"air": map[string]any{"version": 1, "phase": "final_answer"}}},
	}}}
}

func codexNoticeToolCall() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-notice", "update": map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "call-notice-1",
		"title":         "run code mode host",
		"kind":          "execute",
		"status":        "failed",
		"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "code-mode host executable was not found"}}},
	}}}
}

func TestCodexNoticeFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_CODEX_NOTICE_FIXTURE")
	if scenario == "" {
		return
	}
	prompts := 0
	scanner := bufio.NewScanner(os.Stdin)
	emit := func(v any) { data, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(data, '\n')) }
	for scanner.Scan() {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"version": "2.2.2"}}
		case "session/new":
			result = map[string]any{"sessionId": "fixture-codex-notice"}
		case "session/prompt":
			prompts++
			switch scenario {
			case "notice-then-final":
				emit(codexNoticeFrame(codexNoticeText))
				emit(codexFinalFrame("msg_0bdb5fcb53d05ef8016ad862f1da788191a7df11f6ca525aff", "готов"))
			case "notices-then-final":
				emit(codexNoticeFrame(codexNoticeText))
				emit(codexNoticeFrame("Warning: another notice without identity.\n\n"))
				emit(codexFinalFrame("msg_0bdb5fcb53d05ef8016ad862f1da788191a7df11f6ca525aff", "готов"))
			case "notice-only":
				emit(codexNoticeFrame(codexNoticeText))
				emit(codexNoticeFrame("Warning: another notice without identity.\n\n"))
			case "idless-final":
				emit(codexIdlessFinalFrame("готов"))
			case "notice-tool-final":
				emit(codexNoticeFrame(codexNoticeText))
				emit(codexNoticeToolCall())
				emit(codexFinalFrame("msg_0bdb5fcb53d05ef8016ad862f1da788191a7df11f6ca525aff", "готов"))
			case "final-then-notice":
				emit(codexFinalFrame("msg_0bdb5fcb53d05ef8016ad862f1da788191a7df11f6ca525aff", "готов"))
				emit(codexNoticeFrame(codexNoticeText))
			case "two-turns":
				if prompts == 1 {
					emit(codexNoticeFrame(codexNoticeText))
					emit(codexFinalFrame("msg_first", "первый"))
				} else {
					emit(codexNoticeFrame(codexNoticeText))
					emit(codexFinalFrame("msg_second", "второй"))
				}
			}
			result = map[string]any{"stopReason": "end_turn"}
		}
		emit(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func codexNoticeRuntime(t *testing.T, scenario string) CodexRuntime {
	t.Helper()
	return CodexRuntime{ACPRuntime: ACPRuntime{
		Command:     os.Args[0],
		Arguments:   []string{"-test.run=^TestCodexNoticeFixtureProcess$"},
		Environment: []string{"TEST_CODEX_NOTICE_FIXTURE=" + scenario},
	}}
}

func codexNoticeResult(t *testing.T, scenario string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := codexNoticeRuntime(t, scenario).Start(ctx, StartRequest{
		WorkerRef: "notice", Workspace: t.TempDir(), DeferInitialPrompt: true,
		Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		return result
	case <-ctx.Done():
		t.Fatal("no terminal result")
	}
	return Result{}
}

func TestCodexAdapterNoticeDoesNotInvalidateFinalAnswer(t *testing.T) {
	for _, scenario := range []string{"notice-then-final", "notices-then-final", "notice-tool-final", "final-then-notice"} {
		t.Run(scenario, func(t *testing.T) {
			result := codexNoticeResult(t, scenario)
			if result.Status != "succeeded" {
				t.Fatalf("scenario %s result=%+v", scenario, result)
			}
			if result.Summary != "готов" {
				t.Fatalf("scenario %s summary=%q", scenario, result.Summary)
			}
			if strings.Contains(result.Summary, "Warning") || strings.Contains(result.Summary, "Code Mode") {
				t.Fatalf("scenario %s leaked the adapter notice into the final answer: %q", scenario, result.Summary)
			}
		})
	}
}

func TestCodexAdapterNoticeWithoutFinalAnswerFails(t *testing.T) {
	for _, scenario := range []string{"notice-only", "idless-final"} {
		t.Run(scenario, func(t *testing.T) {
			result := codexNoticeResult(t, scenario)
			if result.Status != "failed" {
				t.Fatalf("scenario %s accepted an unattributed answer: %+v", scenario, result)
			}
			if !strings.Contains(result.Summary, "Terminal answer unavailable") {
				t.Fatalf("scenario %s summary=%q", scenario, result.Summary)
			}
		})
	}
}

func TestCodexAdapterNoticeKeepsTwoTurnsUsable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	session, err := codexNoticeRuntime(t, "two-turns").Start(ctx, StartRequest{
		WorkerRef: "notice", Workspace: t.TempDir(), DeferInitialPrompt: true,
		Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	want := []string{"первый", "второй"}
	for turn := 0; turn < len(want); turn++ {
		if err := session.Prompt(ctx, "Ответь одним словом"); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-session.Result():
			if result.Status != "succeeded" || result.Summary != want[turn] {
				t.Fatalf("turn %d result=%+v", turn, result)
			}
		case <-ctx.Done():
			t.Fatalf("turn %d produced no terminal result", turn)
		}
	}
}
