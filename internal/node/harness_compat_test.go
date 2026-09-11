package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedHarnessCompatibilitySuite(t *testing.T) {
	tests := []struct {
		name    string
		runtime Runtime
		harness string
	}{
		{name: "codex", runtime: ACPRuntime{}, harness: "codex"},
		{name: "fx", runtime: FXRuntime{}, harness: "fx"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capture := filepath.Join(t.TempDir(), "requests.jsonl")
			command := exec.Command(os.Args[0], "-test.run=TestSharedHarnessACPProcess")
			runtime := test.runtime
			switch selected := runtime.(type) {
			case ACPRuntime:
				selected.Command, selected.Arguments, selected.Environment = command.Path, command.Args[1:], []string{"HARNESS_CAPTURE=" + capture}
				runtime = selected
			case FXRuntime:
				selected.Command, selected.Arguments, selected.Environment = command.Path, command.Args[1:], []string{"HARNESS_CAPTURE=" + capture}
				runtime = selected
			}
			local := NewLocal(runtime)
			workspace := t.TempDir()
			profile := ManagedProfile{
				Name: "worker", Version: "v-test", Hash: "hash-test", Runtime: test.harness,
				Delivery: "workspace_instructions", Content: "Follow the managed worker profile.",
				AllowTools: []string{"read"}, Model: "provider/model",
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := local.Dispatch(ctx, StartRequest{WorkerRef: test.name, Task: "inspect", Workspace: workspace, Profile: profile, MCPServers: []MCPServer{{Name: "secretary", Command: "secretary-mcp"}}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			instructions, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
			if err != nil {
				t.Fatalf("managed instructions: %v", err)
			}
			if !strings.Contains(string(instructions), profile.Content) {
				t.Fatalf("AGENTS.md=%q", instructions)
			}
			select {
			case activity := <-session.Activity():
				if activity.Kind != ActivityText || activity.Text != "shared activity" {
					t.Fatalf("activity=%#v", activity)
				}
			case <-ctx.Done():
				t.Fatal("shared harness did not publish activity")
			}
			select {
			case result := <-session.Result():
				if result.Status != "succeeded" || result.Summary != "shared result" {
					t.Fatalf("result=%#v", result)
				}
			case <-ctx.Done():
				t.Fatal("shared harness did not complete prompt")
			}
			injected, err := session.Steer(ctx, "continue")
			if err != nil || !injected {
				t.Fatalf("steer injected=%v err=%v", injected, err)
			}
			requests, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(requests), `"mcpServers"`) || !strings.Contains(string(requests), `"secretaryProfileDelivery":"workspace_instructions"`) {
				t.Fatalf("request capture does not include MCP/profile metadata: %s", requests)
			}
		})
	}
}

func TestRuntimeRouterFailsExplicitlyWhenHarnessUnavailable(t *testing.T) {
	router := RuntimeRouter{DefaultHarness: "fx", ACP: recordingRuntime{started: make(chan StartRequest, 1)}}
	_, err := router.Start(context.Background(), StartRequest{WorkerRef: "missing", Profile: ManagedProfile{Runtime: "fx"}})
	if err == nil || !strings.Contains(err.Error(), `harness "fx" is unavailable`) {
		t.Fatalf("err=%v", err)
	}
	_, err = (RuntimeRouter{DefaultHarness: "made-up"}).Start(context.Background(), StartRequest{WorkerRef: "missing"})
	if err == nil || !strings.Contains(err.Error(), `harness "made-up" is unavailable`) {
		t.Fatalf("unknown harness err=%v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("unavailable harness must not be reported as context cancellation")
	}
}

func TestSharedHarnessACPProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg != "-test.run=TestSharedHarnessACPProcess" {
			continue
		}
		capture := os.Getenv("HARNESS_CAPTURE")
		encoder := json.NewEncoder(os.Stdout)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			var request struct {
				ID     json.RawMessage `json:"id,omitempty"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params,omitempty"`
			}
			if json.Unmarshal(line, &request) != nil {
				continue
			}
			if capture != "" {
				file, _ := os.OpenFile(capture, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if file != nil {
					_, _ = file.Write(append(line, '\n'))
					_ = file.Close()
				}
			}
			if len(request.ID) == 0 {
				continue
			}
			result := map[string]any{}
			switch request.Method {
			case "initialize":
				result = map[string]any{}
			case "session/new":
				result = map[string]any{"sessionId": "shared-session"}
			case "session/load":
				result = map[string]any{}
			case "session/prompt":
				_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "shared activity"}}}})
				result = map[string]any{"summary": "shared result"}
			case "_session/steering":
				result = map[string]any{"outcome": "injected"}
			}
			_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
		}
		return
	}
}
