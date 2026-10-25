//go:build !windows

package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMAC1204CodexCancelResultAndSameSessionFollowup(t *testing.T) {
	if os.Getenv("MAC1204_RESULT_HELPER") == "1" {
		reader := bufio.NewScanner(os.Stdin)
		var pending json.RawMessage
		send := func(id json.RawMessage, result any) {
			data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
			if err != nil { os.Exit(2) }
			fmt.Fprintln(os.Stdout, string(data))
		}
		for reader.Scan() {
			var message struct {
				ID json.RawMessage `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(reader.Bytes(), &message) != nil { os.Exit(3) }
			switch message.Method {
			case "initialize":
				send(message.ID, map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{}})
			case "session/new":
				send(message.ID, map[string]any{"sessionId": "original-native-session"})
			case "session/prompt":
				if pending == nil {
					pending = append(json.RawMessage(nil), message.ID...)
					_ = os.WriteFile(os.Getenv("MAC1204_PROMPT_MARKER"), []byte("running"), 0600)
				} else {
					update := map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
						"sessionId": "original-native-session",
						"update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "followup-final",
							"content": map[string]string{"type": "text", "text": "followup-complete"},
							"_meta": map[string]any{"jetbrains": map[string]any{"air": map[string]any{"phase": "final_answer"}}}},
					}}
					frame, _ := json.Marshal(update)
					fmt.Fprintln(os.Stdout, string(frame))
					send(message.ID, map[string]any{"stopReason": "end_turn"})
				}
			case "session/cancel":
				if pending == nil { os.Exit(4) }
				send(pending, map[string]any{"stopReason": "cancelled"})
			}
		}
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	t.Setenv("MAC1204_RESULT_HELPER", "1")
	marker := filepath.Join(t.TempDir(), "prompt-started")
	t.Setenv("MAC1204_PROMPT_MARKER", marker)
	runtime := ACPRuntime{
		Command: os.Args[0],
		Arguments: []string{"-test.run=^TestMAC1204CodexCancelResultAndSameSessionFollowup$"},
		CodexMessagePhases: true,
		TerminalMessageGrouping: true,
		DrainPromptEvents: true,
	}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "mac12-result-worker", Workspace: t.TempDir(), DeferInitialPrompt: true})
	if err != nil { t.Fatal(err) }
	defer session.Close()
	originalID := session.ID()
	finished := make(chan error, 1)
	go func() { finished <- session.Prompt(ctx, "start") }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil { break }
		if time.Now().After(deadline) { t.Fatal("native prompt did not reach ACP") }
		time.Sleep(5 * time.Millisecond)
	}
	if err := session.Cancel(ctx); err != nil { t.Fatal(err) }
	select {
	case result := <-session.Result():
		if result.Status != "canceled" || result.CompletionEvidence == nil { t.Fatalf("cancel result=%+v", result) }
	case <-ctx.Done(): t.Fatal(ctx.Err())
	}
	select {
	case err := <-finished:
		if err != nil { t.Fatalf("native prompt return=%v", err) }
	case <-ctx.Done(): t.Fatal(ctx.Err())
	}
	select {
	case duplicate := <-session.Result(): t.Fatalf("duplicate canceled result=%+v", duplicate)
	case <-time.After(80 * time.Millisecond):
	}
	if session.ID() != originalID { t.Fatal("native session changed after cancellation") }
	if err := session.Prompt(ctx, "followup"); err != nil { t.Fatal(err) }
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" || result.CompletionEvidence == nil || !strings.Contains(result.Summary, "followup-complete") {
			t.Fatalf("followup result=%+v", result)
		}
	case <-ctx.Done(): t.Fatal(ctx.Err())
	}
	if session.ID() != originalID { t.Fatal("followup lost original native session identity") }
	select {
	case duplicate := <-session.Result(): t.Fatalf("late duplicate result=%+v", duplicate)
	case <-time.After(80 * time.Millisecond):
	}
}
