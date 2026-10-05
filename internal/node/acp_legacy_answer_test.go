package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Wire-shape regression, NOT native fx acceptance. The pinned fx0.0.8 source
// types.zig emits 32-character lowercase hex messageId, text-only chunks, and
// stopReason/usage without summary. prompt.zig:pushText sends assistant and
// operational text under different IDs without publishing their phase.
func TestACPLegacyFXWireShapeQueuedAndSecondAttempts(t *testing.T) {
	for _, fx := range []bool{false, true} {
		for _, queued := range []bool{false, true} {
			for _, explicitFirst := range []bool{false, true} {
				name := fmt.Sprintf("fx=%t/queued=%t/explicit-first=%t", fx, queued, explicitFirst)
				t.Run(name, func(t *testing.T) {
					scenario := "wire"
					if explicitFirst {
						scenario = "explicit-first"
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					session, base := startLegacyAnswerFixture(t, ctx, scenario, fx)
					defer session.Close()
					promptDone := make(chan error, 1)
					go func() { promptDone <- session.Prompt(ctx, "first") }()
					// Native reverse-request handshake holds prompt1 open while
					// Queue is admitted. No sleeps or guessed idle boundaries.
					for {
						select {
						case activity := <-session.Activity():
							if activity.Kind != ActivityPermission {
								continue
							}
							if queued {
								if err := session.(Queueer).Queue(ctx, "second"); err != nil {
									t.Fatal(err)
								}
							}
							if err := session.(Responder).Respond(ctx, activity.RequestID, "approved"); err != nil {
								t.Fatal(err)
							}
						case <-ctx.Done():
							t.Fatal("first prompt gate timed out")
						}
						break
					}
					results := make([]Result, 0, 2)
					if explicitFirst {
						// The 128 preceding activity events fill Activity + transport
						// buffers. Summary must still arrive before draining them.
						select {
						case result := <-session.Result():
							results = append(results, result)
						case <-ctx.Done():
							t.Fatal("explicit summary lost legacy early Result behavior")
						}
						if queued {
							base.turnMu.Lock()
							pending := len(base.queued)
							base.turnMu.Unlock()
							if pending != 1 {
								t.Fatal("queued prompt reset the collector before prior late deltas drained")
							}
						}
					}
					for len(results) < 2 {
						select {
						case <-session.Activity():
						case result := <-session.Result():
							results = append(results, result)
							if !queued && len(results) == 1 {
								// Keep consuming Activity until Prompt itself reaches
								// the FIFO idle boundary, then start attempt2.
								waitLegacyPrompt(t, ctx, session, promptDone)
								go func() { promptDone <- session.Prompt(ctx, "second") }()
							}
						case <-ctx.Done():
							t.Fatal("legacy queued/second Result timed out")
						}
						// With an early explicit Result the first append happened
						// above, so direct attempt2 needs the same FIFO boundary.
						if explicitFirst && !queued && len(results) == 1 {
							waitLegacyPrompt(t, ctx, session, promptDone)
							go func() { promptDone <- session.Prompt(ctx, "second") }()
							explicitFirst = false
						}
					}
					for index, result := range results {
						want := legacyWireReport(index + 1)
						if index == 0 && scenario == "explicit-first" {
							want = "explicit terminal summary"
						}
						if result.Status != "succeeded" || result.Summary != want || strings.Contains(result.Summary, "Terminal answer unavailable") {
							t.Fatalf("legacy metadata: attempt=%d status=%s expected_report=%t bytes=%d", index+1, result.Status, result.Summary == want, len(result.Summary))
						}
					}
				})
			}
		}
	}
}

func TestACPLegacyStopReasonsErrorsAndSummaryExtension(t *testing.T) {
	for _, fx := range []bool{false, true} {
		for _, tc := range []struct{ scenario, status, report string }{
			{"wire", "succeeded", legacyWireReport(1)},
			{"cancelled", "canceled", legacyWireReport(1)},
			{"canceled", "canceled", legacyWireReport(1)},
			{"empty", "succeeded", "completed"},
			{"summary", "succeeded", "explicit terminal summary"},
			{"summary-cancelled", "canceled", "explicit terminal summary"},
			{"rpc-error", "failed", "acp rpc -32000: synthetic failure"},
		} {
			t.Run(fmt.Sprintf("fx=%t/%s", fx, tc.scenario), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				session, _ := startLegacyAnswerFixture(t, ctx, tc.scenario, fx)
				defer session.Close()
				promptDone := make(chan error, 1)
				go func() { promptDone <- session.Prompt(ctx, "first") }()
				var result Result
				for result.Summary == "" {
					select {
					case activity := <-session.Activity():
						if activity.Kind == ActivityPermission {
							if err := session.(Responder).Respond(ctx, activity.RequestID, "approved"); err != nil {
								t.Fatal(err)
							}
						}
					case result = <-session.Result():
					case <-ctx.Done():
						t.Fatal("legacy terminal report timed out")
					}
				}
				waitLegacyPrompt(t, ctx, session, promptDone)
				if result.Status != tc.status || result.Summary != tc.report {
					t.Fatalf("legacy terminal metadata: status=%s expected_report=%t", result.Status, result.Summary == tc.report)
				}
			})
		}
	}
}

func waitLegacyPrompt(t *testing.T, ctx context.Context, session Session, done <-chan error) {
	t.Helper()
	for {
		select {
		case <-session.Activity():
		case <-done:
			return // RPC error is asserted through the terminal Result.
		case <-ctx.Done():
			t.Fatal("legacy Prompt FIFO boundary timed out")
		}
	}
}

func startLegacyAnswerFixture(t *testing.T, ctx context.Context, scenario string, fx bool) (Session, *acpSession) {
	t.Helper()
	baseRuntime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestACPLegacyAnswerFixtureProcess$"}, Environment: []string{"TEST_ACP_LEGACY_ANSWER=" + scenario}}
	var runtime Runtime = baseRuntime
	if fx {
		runtime = FXRuntime{ACPRuntime: baseRuntime}
	}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "legacy-fixture", Workspace: t.TempDir(), DeferInitialPrompt: true})
	if err != nil {
		t.Fatal(err)
	}
	var base *acpSession
	if fx {
		base = session.(*fxSession).acpSession
	} else {
		base = session.(*acpSession)
	}
	if base.terminalMessageGrouping || base.drainPromptEvents {
		t.Fatal("non-opt-in fixture unexpectedly enabled native OpenCode contract")
	}
	return session, base
}

func legacyWireReport(attempt int) string {
	return fmt.Sprintf("assistant-%d\noperational-%d\nassistant-final-%d\nlate-delta-%d", attempt, attempt, attempt, attempt)
}

func TestACPLegacyAnswerFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_ACP_LEGACY_ANSWER")
	if scenario == "" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	update := func(kind, id, text string) {
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "legacy-session", "update": map[string]any{"sessionUpdate": kind, "messageId": id, "content": map[string]string{"type": "text", "text": text}}}})
	}
	complete := func(id json.RawMessage, attempt int) {
		if scenario != "empty" {
			update("user_message_chunk", fmt.Sprintf("%032x", attempt*10), "user echo must not be assistant report")
			update("agent_thought_chunk", "", "synthetic discarded thought")
			if scenario == "explicit-first" && attempt == 1 {
				for i := 0; i < 123; i++ {
					update("agent_message_chunk", fmt.Sprintf("%032x", attempt*10+1), "late preceding text")
				}
			}
			update("agent_message_chunk", fmt.Sprintf("%032x", attempt*10+1), fmt.Sprintf("assistant-%d\n", attempt))
			update("agent_message_chunk", fmt.Sprintf("%032x", attempt*10+2), fmt.Sprintf("operational-%d\n", attempt))
			update("agent_message_chunk", fmt.Sprintf("%032x", attempt*10+3), fmt.Sprintf("assistant-final-%d\n", attempt))
			// An older messageId does not mean its late delta may be dropped.
			update("agent_message_chunk", fmt.Sprintf("%032x", attempt*10+1), fmt.Sprintf("late-delta-%d", attempt))
		}
		if scenario == "rpc-error" {
			_ = encoder.Encode(map[string]any{"id": id, "error": map[string]any{"code": -32000, "message": "synthetic failure"}})
			return
		}
		result := map[string]any{"stopReason": "end_turn"}
		if scenario == "cancelled" || scenario == "canceled" {
			result["stopReason"] = scenario
		}
		if scenario == "summary" || scenario == "summary-cancelled" || scenario == "explicit-first" && attempt == 1 {
			result["summary"] = "explicit terminal summary"
		}
		if scenario == "summary-cancelled" {
			result["stopReason"] = "cancelled"
		}
		_ = encoder.Encode(map[string]any{"id": id, "result": result})
	}
	var firstID json.RawMessage
	attempt := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage
			Method string
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		if request.Method == "" && string(request.ID) == "700" {
			complete(firstID, 1)
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		switch request.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]string{"sessionId": "legacy-session"}})
		case "session/prompt":
			attempt++
			if attempt == 1 {
				firstID = append(json.RawMessage(nil), request.ID...)
				_ = encoder.Encode(map[string]any{"id": 700, "method": "session/request_permission", "params": map[string]any{"options": []map[string]string{{"optionId": "allow_once", "kind": "allow_once"}}}})
			} else {
				complete(request.ID, attempt)
			}
		}
	}
	os.Exit(0)
}
