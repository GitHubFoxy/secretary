package node

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestACPRuntimeShowsNativeCodexMCPActivity(t *testing.T) {
	frames := `[{"sessionUpdate":"tool_call","toolCallId":"native-mcp-call","kind":"execute","title":"private display title token=hidden-title","status":"completed","_meta":{"is_mcp_tool_call":true},"rawInput":{"server":"acceptance","tool":"read_acceptance_marker","arguments":{"query":"marker token=hidden-argument"}},"rawOutput":{"result":{"content":[{"type":"text","text":"marker token=hidden-output"}]},"error":null}}]`
	activities := nativeMCPFixtureActivities(t, frames)
	if len(activities) != 1 || activities[0].Kind != ActivityToolResult || activities[0].Tool != "mcp.acceptance.read_acceptance_marker" || activities[0].Status != "completed" {
		t.Fatalf("native completed MCP activity=%#v", activities)
	}
	metadata := core.ActivityMetadata{EventID: "mcp-event", Node: "macbook", HarnessInstanceID: "macbook/codex", WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	activity, ok := NormalizeRuntimeActivity(activities[0], metadata, core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityToolResult}})
	if !ok || activity.ToolResult == nil || activity.ToolResult.Preview != "marker token=[redacted]" {
		t.Fatalf("normalized MCP result=%#v ok=%v", activity, ok)
	}
	encoded, err := json.Marshal(activity)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"hidden-title", "hidden-argument", "hidden-output", "private display title"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("private content leaked into activity: %s", encoded)
		}
	}
}

func nativeMCPFixtureActivities(t *testing.T, frames string) []Activity {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	runtime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestFakeACPNativeMCPActivityProcess$"}, Environment: []string{"ACP_NATIVE_MCP_FRAMES=" + frames}}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var activities []Activity
	for {
		select {
		case item, ok := <-session.Activity():
			if !ok {
				t.Fatal("native activity closed before wire sentinel")
			}
			if item.Kind == ActivityText && item.Text == "wire-finished" {
				return activities
			}
			activities = append(activities, item)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestFakeACPNativeMCPActivityProcess(t *testing.T) {
	frames := os.Getenv("ACP_NATIVE_MCP_FRAMES")
	if frames == "" {
		return
	}
	var updates []json.RawMessage
	if err := json.Unmarshal([]byte(frames), &updates); err != nil {
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result["protocolVersion"] = 1
		case "session/new":
			result["sessionId"] = "native-mcp-session"
		case "session/prompt":
			for _, update := range updates {
				_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": "native-mcp-session", "update": update}})
			}
			_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": "native-mcp-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "wire-finished"}}}})
			result["summary"] = "done"
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
	os.Exit(0)
}

func TestACPRuntimeNativeToolIdentityAndLifecycle(t *testing.T) {
	for _, test := range []struct {
		name   string
		frames string
		want   []Activity
	}{
		{
			name: "concurrent MCP and sparse completions",
			frames: `[
{"sessionUpdate":"tool_call","toolCallId":"first","kind":"execute","title":"mcp.acceptance.read_acceptance_marker","status":"in_progress","_meta":{"is_mcp_tool_call":true},"rawInput":{"server":"acceptance","tool":"read_acceptance_marker","arguments":{"query":"first"}}},
{"sessionUpdate":"tool_call","toolCallId":"second","kind":"execute","title":"mcp.acceptance.read_acceptance_marker","status":"in_progress","_meta":{"is_mcp_tool_call":true},"rawInput":{"server":"acceptance","tool":"read_acceptance_marker","arguments":{"query":"second"}}},
{"sessionUpdate":"tool_call_update","toolCallId":"second","status":"completed","rawInput":{"server":"acceptance","tool":"read_acceptance_marker","arguments":{"query":"second"}},"rawOutput":{"result":{"content":[]},"error":null}},
{"sessionUpdate":"tool_call_update","toolCallId":"first","status":"failed"},
{"sessionUpdate":"tool_call_update","toolCallId":"first","status":"failed"},
{"sessionUpdate":"tool_call","title":"mcp.acceptance.read_acceptance_marker","kind":"execute","status":"completed"}
]`,
			want: []Activity{
				{Kind: ActivityToolCall, Status: "in_progress", Tool: "mcp.acceptance.read_acceptance_marker", Arguments: json.RawMessage(`{"query":"first"}`)},
				{Kind: ActivityToolCall, Status: "in_progress", Tool: "mcp.acceptance.read_acceptance_marker", Arguments: json.RawMessage(`{"query":"second"}`)},
				{Kind: ActivityToolResult, Tool: "mcp.acceptance.read_acceptance_marker", Status: "completed", Arguments: json.RawMessage(`{"query":"second"}`)},
				{Kind: ActivityToolResult, Tool: "mcp.acceptance.read_acceptance_marker", Status: "failed", Arguments: json.RawMessage(`{"query":"first"}`)},
			},
		},
		{
			name: "concurrent idless MCP rejects ambiguous update",
			frames: `[
{"sessionUpdate":"tool_call","kind":"execute","status":"in_progress","_meta":{"is_mcp_tool_call":true},"rawInput":{"server":"acceptance","tool":"read_acceptance_marker","arguments":{"query":"first"}}},
{"sessionUpdate":"tool_call","kind":"execute","status":"in_progress","_meta":{"is_mcp_tool_call":true},"rawInput":{"server":"acceptance","tool":"read_acceptance_marker","arguments":{"query":"second"}}},
{"sessionUpdate":"tool_call_update","status":"completed"}
]`,
			want: []Activity{
				{Kind: ActivityToolCall, Status: "in_progress", Tool: "mcp.acceptance.read_acceptance_marker", Arguments: json.RawMessage(`{"query":"first"}`)},
				{Kind: ActivityToolCall, Status: "in_progress", Tool: "mcp.acceptance.read_acceptance_marker", Arguments: json.RawMessage(`{"query":"second"}`)},
			},
		},
		{
			name: "native exec and wait retain explicit names",
			frames: `[
{"sessionUpdate":"tool_call","toolCallId":"exec","kind":"execute","name":"exec_command","title":"sleep 40","status":"completed","rawInput":{"command":"sleep 40","cwd":"/workspace"},"content":[],"_meta":{"terminal_info":{}}},
{"sessionUpdate":"tool_call_update","toolCallId":"exec","status":"completed","rawOutput":{"exit_code":0,"formatted_output":""},"_meta":{"terminal_exit":{}}},
{"sessionUpdate":"tool_call","toolCallId":"wait","kind":"other","name":"wait","status":"in_progress","rawInput":{"name":"wait","arguments":{"session_id":"private-native-id"}}},
{"sessionUpdate":"tool_call_update","toolCallId":"wait","status":"completed"}
]`,
			want: []Activity{
				{Kind: ActivityToolResult, Tool: "exec_command", Status: "completed", Arguments: json.RawMessage(`{"command":"sleep 40","cwd":"/workspace"}`)},
				{Kind: ActivityToolCall, Status: "in_progress", Tool: "wait", Arguments: json.RawMessage(`{"arguments":{"session_id":"private-native-id"},"name":"wait"}`)},
				{Kind: ActivityToolResult, Tool: "wait", Status: "completed", Arguments: json.RawMessage(`{"arguments":{"session_id":"private-native-id"},"name":"wait"}`)},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := nativeMCPFixtureActivities(t, test.frames)
			if len(got) != len(test.want) {
				t.Fatalf("activities=%#v want=%#v", got, test.want)
			}
			for i, want := range test.want {
				if got[i].Kind != want.Kind || got[i].Tool != want.Tool || got[i].Status != want.Status || string(got[i].Arguments) != string(want.Arguments) {
					t.Fatalf("activity[%d]=%#v want=%#v", i, got[i], want)
				}
			}
		})
	}
}
