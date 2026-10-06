package node

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestACPTurnAnswerUsesIdentityNotWords(t *testing.T) {
	a := newACPTurnAnswer()
	a.text("progress", "Final answer: misleading progress")
	a.tool() // also unnamed tools
	a.text("answer", "Попробую получить — legitimate final answer.\n")
	a.text("answer", "[source](https://example.org)")
	a.text("progress", "late delta")
	got, ok := a.final("end_turn")
	if !ok || got != "Попробую получить — legitimate final answer.\n[source](https://example.org)" {
		t.Fatal("answer was classified by words or late old message delta")
	}
	for _, reason := range []string{"", "max_tokens", "cancelled", "refusal"} {
		if _, ok := a.final(reason); ok {
			t.Fatal("incomplete answer declared final")
		}
	}
	a.tool()
	if _, ok := a.final("end_turn"); ok {
		t.Fatal("pre-tool text declared final")
	}
	a.text("answer", "same identity cannot prove a new answer")
	if _, ok := a.final("end_turn"); ok {
		t.Fatal("split a message without metadata")
	}
	a.text("fresh", "complete")
	a.text("", "unknown identity")
	if _, ok := a.final("end_turn"); ok {
		t.Fatal("missing metadata was guessed")
	}
}

func TestACPAuthoritativeAnswerFIFOAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name             string
		grouping         bool
		wantStatus, want string
	}{
		{"native", true, "succeeded", "answer-one\nanswer-two"},
		{"summary", false, "succeeded", "explicit terminal summary"},
		{"idless", true, "failed", "Terminal answer unavailable"},
		// Approved backward compatibility, not native fx/final-answer acceptance.
		{"unverified-adapter", false, "succeeded", strings.Repeat("progress", 80) + "answer-one\nanswer-twochild output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runtime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestACPAnswerFixtureProcess$"}, Environment: []string{"TEST_ACP_ANSWER=" + tc.name}, TerminalMessageGrouping: tc.grouping, DrainPromptEvents: true}
			session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "answer-fixture", Workspace: t.TempDir(), DeferInitialPrompt: true}, "answer-session")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			for attempt := 0; attempt < 2; attempt++ {
				prompt := make(chan error, 1)
				go func() { prompt <- session.Prompt(ctx, "synthetic user") }()
				progress := 0
				var result Result
				for result.Summary == "" {
					select {
					case activity := <-session.Activity():
						if strings.Contains(activity.Text, "progress") {
							progress++
						}
						if strings.Contains(activity.Text, "historical") {
							t.Fatal("replay exposed as new Activity")
						}
					case result = <-session.Result():
					case err := <-prompt:
						if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal("FIFO test timed out")
					}
				}
				for len(session.Activity()) > 0 {
					if strings.Contains((<-session.Activity()).Text, "progress") {
						progress++
					}
				}
				if result.Status != tc.wantStatus || (tc.wantStatus == "succeeded" && result.Summary != tc.want) || !strings.HasPrefix(result.Summary, tc.want) || progress != 80 {
					t.Fatalf("metadata checks: status=%s expected_answer=%t progress_chunks=%d", result.Status, result.Summary == tc.want, progress)
				}
				// Result arrives before Prompt releases its busy latch. Wait for
				// that boundary via the call, never a sleep before next Attempt.
				if session.(*acpSession).busyNow() {
					select {
					case err := <-prompt:
						if err != nil {
							t.Fatal(err)
						}
					case <-ctx.Done():
						t.Fatal("Prompt boundary timed out")
					}
				}
			}
		})
	}
}

func TestACPRuntimeDrainKeepsProgressAvailableAndTurnScoped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestACPAnswerFixtureProcess$"}, Environment: []string{"TEST_ACP_ANSWER=drain-boundary"}, TerminalMessageGrouping: true}
	session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "addressed-answer-fixture", Workspace: t.TempDir(), DeferInitialPrompt: true, DrainOutputBeforeResult: true}, "answer-session")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// The ACP FIFO fence orders watcher processing, not reads from the separate
	// public Activity and Result channels. Check eventual Activity availability
	// and exact Result/turn boundaries instead of asserting select order.
	progress := map[string]int{"1": 0, "2": 0}
	finalDeltas := map[string]int{"1": 0, "2": 0}
	recordActivity := func(activity Activity) {
		for _, turnID := range []string{"1", "2"} {
			switch activity.Text {
			case "progress-turn-" + turnID:
				progress[turnID]++
			case "final-turn-" + turnID + "-part-one\n", "final-turn-" + turnID + "-part-two":
				finalDeltas[turnID]++
			}
		}
	}
	for turn := 1; turn <= 2; turn++ {
		promptDone := make(chan error, 1)
		go func() { promptDone <- session.Prompt(ctx, "synthetic user") }()
		var result Result
		promptReturned := false
		for result.Summary == "" || !promptReturned {
			select {
			case activity := <-session.Activity():
				recordActivity(activity)
			case result = <-session.Result():
			case err := <-promptDone:
				if err != nil {
					t.Fatal(err)
				}
				promptReturned = true
			case <-ctx.Done():
				t.Fatal("addressed output drain timed out")
			}
		}
		turnID := strconv.Itoa(turn)
		wantSummary := "final-turn-" + turnID + "-part-one\nfinal-turn-" + turnID + "-part-two"
		if result.Status != "succeeded" || result.Summary != wantSummary || strings.Contains(result.Summary, "progress") {
			t.Fatalf("turn=%d terminal result contract mismatch: succeeded=%t exact_summary=%t contains_progress=%t", turn, result.Status == "succeeded", result.Summary == wantSummary, strings.Contains(result.Summary, "progress"))
		}
	}
	// The fixture's preceding notifications are available by the Result/fence
	// boundary, though this consumer may receive Result first.
	for progress["1"] != 80 || progress["2"] != 80 || finalDeltas["1"] != 2 || finalDeltas["2"] != 2 {
		select {
		case activity := <-session.Activity():
			recordActivity(activity)
		case <-ctx.Done():
			t.Fatalf("public activity availability incomplete: turn1_progress=%d turn1_final_deltas=%d turn2_progress=%d turn2_final_deltas=%d", progress["1"], finalDeltas["1"], progress["2"], finalDeltas["2"])
		}
	}
}

func TestACPAnswerFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_ACP_ANSWER")
	if scenario == "" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	update := func(kind, id, text string, meta map[string]any) {
		payload := map[string]any{"sessionUpdate": kind, "content": map[string]string{"type": "text", "text": text}, "messageId": id}
		if meta != nil {
			payload["_meta"] = meta
		}
		_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": "answer-session", "update": payload}})
	}
	scanner := bufio.NewScanner(os.Stdin)
	promptCount := 0
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage
			Method string
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil || len(req.ID) == 0 {
			continue
		}
		result := map[string]any{}
		switch req.Method {
		case "initialize":
			result["protocolVersion"] = 1
		case "session/new":
			result["sessionId"] = "answer-session"
		case "session/load":
			for i := 0; i < 100; i++ {
				update("agent_message_chunk", "old", "historical", nil)
			}
		case "session/prompt":
			if scenario == "mcp-only" || scenario == "missing-stop" || scenario == "malformed-stop" || scenario == "max-tokens" || scenario == "refusal" || scenario == "progress-only" || scenario == "empty-chunk" || scenario == "unknown-stop" || scenario == "canceled" || scenario == "assistant-final" || scenario == "summary-only" || scenario == "rpc-error" {
				result := map[string]any{}
				switch scenario {
				case "mcp-only":
					update("tool_call", "", "", nil)
					result["stopReason"] = "end_turn"
				case "missing-stop":
					update("tool_call", "", "", nil)
				case "malformed-stop":
					update("tool_call", "", "", nil)
					result["stopReason"] = []string{"end_turn"}
				case "max-tokens", "refusal":
					update("tool_call", "", "", nil)
					if scenario == "max-tokens" {
						result["stopReason"] = "max_tokens"
					} else {
						result["stopReason"] = "refusal"
					}
				case "progress-only":
					update("agent_message_chunk", "progress", "nonterminal progress", nil)
					update("tool_call", "", "", nil)
					result["stopReason"] = "end_turn"
				case "empty-chunk":
					update("agent_message_chunk", "empty", "", nil)
					result["stopReason"] = "end_turn"
				case "unknown-stop":
					update("tool_call", "", "", nil)
					result["stopReason"] = "future_reason"
				case "canceled":
					update("tool_call", "", "", nil)
					result["stopReason"] = "canceled"
				case "assistant-final":
					update("agent_message_chunk", "final", "actual assistant final text", nil)
					result["stopReason"] = "end_turn"
				case "summary-only":
					result["stopReason"] = "end_turn"
					result["summary"] = "unverified terminal response summary"
				case "rpc-error":
					update("tool_call", "", "", nil)
					_ = encoder.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": -32000, "message": "synthetic provider failure"}})
					continue
				}
				_ = encoder.Encode(map[string]any{"id": req.ID, "result": result})
			} else if scenario == "drain-boundary" {
				promptCount++
				turnID := strconv.Itoa(promptCount)
				update("user_message_chunk", "user", "user echo", nil)
				update("agent_thought_chunk", "thought", "private reasoning fixture", nil)
				for i := 0; i < 80; i++ {
					update("agent_message_chunk", "progress", "progress-turn-"+turnID, nil)
				}
				update("tool_call", "", "", nil) // no name: never invent tool cards
				id := "answer-" + turnID
				update("agent_message_chunk", id, "final-turn-"+turnID+"-part-one\n", nil)
				update("agent_message_chunk", id, "final-turn-"+turnID+"-part-two", nil)
				result["stopReason"] = "end_turn"
				if promptCount == 1 {
					result["summary"] = "explicit terminal summary turn-1"
				}
			} else {
				update("user_message_chunk", "user", "user echo", nil)
				update("agent_thought_chunk", "thought", "private reasoning fixture", nil)
				for i := 0; i < 80; i++ {
					update("agent_message_chunk", "progress", "progress", nil)
				}
				update("tool_call", "", "", nil) // no name: never invent tool cards
				id := "answer"
				if scenario == "idless" {
					id = ""
				}
				update("agent_message_chunk", id, "answer-one\n", nil)
				update("agent_message_chunk", id, "answer-two", nil)
				update("agent_message_chunk", "child", "child output", map[string]any{"opencode/child-session": map[string]any{"id": "child"}})
				result["stopReason"] = "end_turn"
				if scenario == "summary" {
					result["summary"] = "explicit terminal summary"
				}
			}
		}
		_ = encoder.Encode(map[string]any{"id": req.ID, "result": result})
	}
	os.Exit(0)
}
