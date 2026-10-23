package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestClaudeSessionPreservesWhitespaceTextDeltas(t *testing.T) {
	session := &claudeSession{activity: make(chan Activity, 1)}
	session.emitText(" ")
	select {
	case activity := <-session.activity:
		if activity.Kind != ActivityText || activity.Text != " " {
			t.Fatalf("activity=%#v, want a whitespace text delta", activity)
		}
	default:
		t.Fatal("whitespace text delta was discarded")
	}
}

func TestClaudeCodeRuntimeUsesNativeCLIWithAuthoritativePins(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "claude-request.json")
	launcher := filepath.Join(t.TempDir(), "claude")
	launcherScript := "#!/bin/sh\nprintf '%s' \"$*\" > \"$CLAUDE_CAPTURE\"\nexec \"$CLAUDE_TEST_BINARY\" -test.run=TestFakeClaudeCodeProcess\n"
	if err := os.WriteFile(launcher, []byte(launcherScript), 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := ClaudeCodeRuntime{Command: launcher, Environment: []string{"CLAUDE_CAPTURE=" + capture, "CLAUDE_TEST_BINARY=" + os.Args[0]}}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: t.TempDir(), HarnessInstance: instance, Model: "claude-sonnet", Reasoning: "extended"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" || result.Summary != "native Claude result" {
			t.Fatalf("result=%#v", result)
		}
	case <-ctx.Done():
		t.Fatal("Claude Code did not complete")
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--print", "--output-format", "stream-json", "--model", "claude-sonnet", "--effort", "extended", "--session-id"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("Claude CLI args=%s, missing %q", args, want)
		}
	}
}

func TestClaudeCodeRuntimeRejectsConfiguredPolicyAndUnknownArguments(t *testing.T) {
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	unsafe := []string{
		"--add-dir", "--add-dir=outside",
		"--permission-mode", "--permission-mode=acceptEdits", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "--permission-prompts",
		"--tools", "--tools=Bash", "--allowedTools", "--allowed-tools", "--disallowedTools", "--disallowed-tools", "--restricted", "--settings", "--settings=config.json",
		"--fallback-model", "--fallback-model=attacker", "--model", "--model=attacker", "-m", "-mattacker", "--effort", "--effort=low",
		"--session-id", "--session-id=attacker", "--resume", "--resume=attacker", "-r", "-rattacker", "--continue", "-c", "--fork-session", "--from-pr", "--no-session-persistence", "--",
		"--print", "-p", "-pattacker", "--verbose", "--output-format", "--output-format=text", "--input-format", "--stream-json", "--include-partial-messages", "--json-schema", "--replay-user-messages",
		"--permission-prompt-tool", "--thinking", "--max-thinking-tokens", "--mcp-config", "--plugin-dir", "--system-prompt", "--max-turns", "--unknown-flag", "--disable-slash-commands=true", "attacker-prompt",
	}
	for _, argument := range unsafe {
		t.Run(argument, func(t *testing.T) {
			runtime := ClaudeCodeRuntime{Arguments: []string{argument}}
			_, err := runtime.Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect", Model: "claude-sonnet", Reasoning: "high"})
			if !errors.Is(err, ErrClaudeCodeConfiguration) {
				t.Fatalf("argument %q err=%v, want configuration error", argument, err)
			}
		})
	}
}

func TestClaudeCodeRuntimeAllowsOnlyDocumentedHarmlessStaticArgument(t *testing.T) {
	runtime := ClaudeCodeRuntime{Arguments: []string{"--disable-slash-commands"}}
	request := StartRequest{Task: "inspect", Model: "claude-sonnet", Reasoning: "extended"}
	got, err := runtime.authoritativeArguments(request, "session-id", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--disable-slash-commands", "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages", "--strict-mcp-config", "--permission-mode", "dontAsk", "--json-schema", `{"type":"object","properties":{"task_outcome":{"type":"string","enum":["succeeded","failed"]},"reason":{"type":"string"},"recovered_tool_use_ids":{"type":"array","items":{"type":"string"}},"user_response":{"type":"string","minLength":1}},"required":["task_outcome","reason","recovered_tool_use_ids","user_response"],"additionalProperties":false}`, "--append-system-prompt", instructionClaudeUserResponse, "--mcp-config", `{"mcpServers":{}}`, "--model", "claude-sonnet", "--effort", "extended", "--session-id", "session-id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude CLI args=%#v, want %#v", got, want)
	}
	got, err = runtime.authoritativeArguments(request, "saved-session", true)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--disable-slash-commands", "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages", "--strict-mcp-config", "--permission-mode", "dontAsk", "--json-schema", `{"type":"object","properties":{"task_outcome":{"type":"string","enum":["succeeded","failed"]},"reason":{"type":"string"},"recovered_tool_use_ids":{"type":"array","items":{"type":"string"}},"user_response":{"type":"string","minLength":1}},"required":["task_outcome","reason","recovered_tool_use_ids","user_response"],"additionalProperties":false}`, "--append-system-prompt", instructionClaudeUserResponse, "--mcp-config", `{"mcpServers":{}}`, "--model", "claude-sonnet", "--effort", "extended", "--resume", "saved-session"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claude resume args=%#v, want %#v", got, want)
	}
}

func TestClaudeCodeRuntimeRejectsConfigurationBeforeStartingProcess(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	launcher := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\ntouch \"$CLAUDE_STARTED\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	_, err := (ClaudeCodeRuntime{Command: launcher, Arguments: []string{"--permission-mode", "acceptEdits"}, Environment: []string{"CLAUDE_STARTED=" + started}}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
	if !errors.Is(err, ErrClaudeCodeConfiguration) {
		t.Fatalf("err=%v, want configuration error", err)
	}
	if _, err := os.Stat(started); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Claude process started, stat err=%v", err)
	}
}

func TestClaudeCodeRuntimeRejectsCapabilityMismatch(t *testing.T) {
	for _, capability := range []core.ExecutionCapability{core.CapabilitySteering, core.CapabilityApprovals} {
		t.Run(string(capability), func(t *testing.T) {
			instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{capability}}}
			_, err := (ClaudeCodeRuntime{}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
			if !errors.Is(err, ErrClaudeCodeConfiguration) {
				t.Fatalf("capability %q err=%v, want configuration error", capability, err)
			}
		})
	}
}

func TestClaudeCodeSessionSteeringIsExplicitlyUnsupported(t *testing.T) {
	session := &claudeSession{id: "synthetic-claude-native"}
	injected, err := session.Steer(context.Background(), "continue")
	if injected || !errors.Is(err, ErrClaudeCodeSteeringUnsupported) {
		t.Fatalf("injected=%v err=%v, want explicit unsupported error", injected, err)
	}

	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("macbook", &approvalRuntime{session: session}, store)
	dispatch := dispatchFixture("unsupported-steering-dispatch")
	if outcome, err := execution.HandleCommand(context.Background(), dispatch); err != nil || outcome.State != CommandAccepted {
		t.Fatal("unsupported-steering fixture dispatch failed")
	}
	metadata := dispatch.Metadata()
	metadata.CommandID = "steer"
	outcome, err := execution.HandleCommand(context.Background(), Command{Kind: CommandSteering, Steering: &SteeringCommand{Metadata: metadata, Text: "continue"}})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != CommandFailed || outcome.ErrorCode != "runtime_not_steerable" {
		t.Fatalf("steering outcome=%#v, want explicit unsupported failure", outcome)
	}
}

func TestClaudeBindingRejectsHarnessKindChangeAfterInventoryRefresh(t *testing.T) {
	claude := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1.0.0", Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady}
	envelope := WorkerEnvelope{WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", OriginalUserIntent: "inspect", HarnessInstance: claude, Profile: workerTemplateFixture(claude, "", "")}
	oldInventory := core.HarnessInventorySnapshot{Node: "node", Instances: []core.HarnessInstance{claude}, ObservedAt: time.Now().UTC()}
	if err := envelope.ValidateAgainstInventory("node", oldInventory); err != nil {
		t.Fatal(err)
	}
	refreshed := claude
	refreshed.Kind = core.HarnessCodex
	newInventory := core.HarnessInventorySnapshot{Node: "node", Instances: []core.HarnessInstance{refreshed}, ObservedAt: time.Now().UTC()}
	if err := envelope.ValidateAgainstInventory("node", newInventory); err == nil {
		t.Fatal("immutable Claude binding changed when inventory reused its HarnessInstance ID")
	}
}

func TestClaudeCodeRuntimeUnauthenticatedIsExplicit(t *testing.T) {
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady}
	_, err := (ClaudeCodeRuntime{}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
	if err == nil || !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestClaudeCodeRuntimeMissingExecutableIsExplicit(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-claude")
	codex := recordingRuntime{started: make(chan StartRequest, 1)}
	instance := core.HarnessInstance{ID: "node/claude", Node: "node", Kind: core.HarnessClaudeCode, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	_, err := (RuntimeRouter{ACP: codex, Claude: ClaudeCodeRuntime{Command: missing}}).Start(context.Background(), StartRequest{HarnessInstance: instance, Workspace: t.TempDir(), Task: "inspect"})
	if err == nil || !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("err=%v", err)
	}
	select {
	case <-codex.started:
		t.Fatal("missing Claude executable fell back to Codex")
	default:
	}
}

func TestFakeClaudeCodeProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg != "-test.run=TestFakeClaudeCodeProcess" {
			continue
		}
		encoder := json.NewEncoder(os.Stdout)
		var input map[string]any
		_ = json.NewDecoder(os.Stdin).Decode(&input)
		_ = encoder.Encode(map[string]any{"type": "system", "session_id": input["session_id"]})
		_ = encoder.Encode(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "native activity"}}})
		_ = encoder.Encode(map[string]any{"type": "result", "subtype": "success", "result": "native Claude result", "structured_output": map[string]any{"task_outcome": "succeeded", "reason": "Native CLI fixture completed", "recovered_tool_use_ids": []string{}, "user_response": "native Claude result"}})
		return
	}
}

func TestClaudeDeferredSessionAcceptsIdleFollowup(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec \"$CLAUDE_TEST_BINARY\" -test.run=TestFakeClaudeInteractiveProcess\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := (ClaudeCodeRuntime{Command: launcher, Environment: []string{"CLAUDE_TEST_BINARY=" + os.Args[0]}}).Start(ctx, StartRequest{Workspace: t.TempDir(), DeferInitialPrompt: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case r := <-session.Result():
		t.Fatalf("deferred Start executed: %#v", r)
	case <-time.After(30 * time.Millisecond):
	}
	for _, text := range []string{"first marker", "recall marker"} {
		if err := session.Prompt(ctx, text); err != nil {
			t.Fatal(err)
		}
		select {
		case r := <-session.Result():
			if r.Status != "succeeded" || r.Summary != text {
				t.Fatalf("result=%#v", r)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
func TestFakeClaudeInteractiveProcess(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, " "), "-test.run=TestFakeClaudeInteractiveProcess") {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var input map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &input)
		if input["type"] == "control_request" {
			_ = enc.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": input["request_id"]}})
		}
		if input["type"] != "user" {
			continue
		}
		if path := os.Getenv("CLAUDE_IMAGE_RECORD"); path != "" { _ = os.WriteFile(path, scanner.Bytes(), 0o600) }
		message, _ := input["message"].(map[string]any)
		_ = enc.Encode(map[string]any{"type": "result", "subtype": "success", "result": message["content"], "user_message_uuid": input["uuid"], "structured_output": map[string]any{"task_outcome": "succeeded", "reason": "Interactive fixture completed requested prompt", "recovered_tool_use_ids": []string{}, "user_response": message["content"]}})
	}
	os.Exit(0)
}

func TestClaudeSessionIgnoresPartialAndRejectsEmptyTerminal(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
read input
printf '%s\n' '{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}}' '{"type":"result","subtype":"success","result":""}'
read input
`
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := (ClaudeCodeRuntime{Command: launcher}).Start(ctx, StartRequest{Workspace: t.TempDir(), Task: "answer"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case r := <-session.Result():
		if r.Status != "failed" {
			t.Fatalf("empty terminal=%#v", r)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case a := <-session.Activity():
		t.Fatalf("partial duplicated as final activity: %#v", a)
	default:
	}
}
func TestClaudeCancelDrainsTerminalBeforeNextPrompt(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
read input
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'
read interrupt
printf '%s\n' '{"type":"result","subtype":"success","result":"interrupted"}'
read input
printf '%s\n' '{"type":"assistant","uuid":"final-text","message":{"content":[{"type":"text","text":"done"}]}}' '{"type":"assistant","uuid":"final-text","message":{"content":[{"type":"text","text":"done"}]}}' '{"type":"result","subtype":"success","result":"done"}'
read input
`
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := (ClaudeCodeRuntime{Command: launcher}).Start(ctx, StartRequest{Workspace: t.TempDir(), Task: "work"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case <-session.Activity():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := session.Prompt(ctx, "too soon"); err == nil {
		t.Fatal("active Attempt accepted another Prompt")
	}
	if err := session.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-session.Result():
		if r.Status != "canceled" {
			t.Fatalf("cancel result=%#v", r)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := session.Prompt(ctx, "follow up"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-session.Result():
		if r.Status != "succeeded" || r.Summary != "done" {
			t.Fatalf("followup result=%#v", r)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	a := <-session.Activity()
	if a.Text != "done" {
		t.Fatalf("activity=%#v", a)
	}
	select {
	case a := <-session.Activity():
		t.Fatalf("duplicate assistant=%#v", a)
	default:
	}
}

func TestClaudeProfileAndMCPReachNativeProcess(t *testing.T) {
	capture := filepath.Join(t.TempDir(), "arguments.json")
	launcher := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
python3 -c 'import json,sys,os;json.dump(sys.argv[1:],open(os.environ["CLAUDE_CAPTURE"],"w"))' "$@"
exec "$CLAUDE_TEST_BINARY" -test.run=TestFakeClaudeInteractiveProcess
`
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := (ClaudeCodeRuntime{Command: launcher, Environment: []string{"CLAUDE_CAPTURE=" + capture, "CLAUDE_TEST_BINARY=" + os.Args[0]}}).Start(ctx, StartRequest{Workspace: t.TempDir(), Task: "inspect", Profile: ManagedProfile{Content: "PROFILE_MARKER_624", AllowTools: []string{"read"}}, MCPServers: []MCPServer{{Name: "secretary", Command: "secretaryctl", Args: []string{"mcp"}, Env: []MCPEnv{{Name: "MCP_ENDPOINT", Value: "http://127.0.0.1:8080"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case <-session.Result():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(body, &args); err != nil {
		t.Fatal(err)
	}
	flag := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
		}
		return "missing"
	}
	if strings.Contains(string(body), "http://127.0.0.1:8080") {
		t.Fatal("MCP environment value leaked into process arguments")
	}
	if flag("--append-system-prompt") != instructionClaudeUserResponse+"\nPROFILE_MARKER_624" || flag("--tools") != "Read" || flag("--permission-mode") != "dontAsk" {
		t.Fatalf("profile/policy not delivered: %v", args)
	}
	var mcp struct {
		Servers map[string]struct {
			Command string
			Env     map[string]string
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(flag("--mcp-config")), &mcp); err != nil {
		t.Fatal(err)
	}
	if mcp.Servers["secretary"].Command != "secretaryctl" || mcp.Servers["secretary"].Env["MCP_ENDPOINT"] != "${SECRETARY_CLAUDE_MCP_0_MCP_ENDPOINT}" {
		t.Fatalf("MCP config=%#v", mcp)
	}
}

func TestClaudeResumeContinuesTheNativeIdentity(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec \"$CLAUDE_TEST_BINARY\" -test.run=TestFakeClaudeInteractiveProcess\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := ClaudeCodeRuntime{Command: launcher, Environment: []string{"CLAUDE_TEST_BINARY=" + os.Args[0]}}
	request := StartRequest{Workspace: t.TempDir(), DeferInitialPrompt: true}
	session, err := runtime.Resume(ctx, request, "42fb55f7-c238-42d4-a456-b299998f289e")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if session.ID() != "42fb55f7-c238-42d4-a456-b299998f289e" {
		t.Fatal("native identity changed")
	}
	if err := session.Prompt(ctx, "recall earlier marker"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-session.Result():
		if r.Status != "succeeded" {
			t.Fatalf("result=%#v", r)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestClaudeMissingResumeFailsBeforePrompt(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"is_error\":true}'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := (ClaudeCodeRuntime{Command: launcher}).Resume(ctx, StartRequest{Workspace: t.TempDir(), DeferInitialPrompt: true}, "missing")
	if !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("missing native session err=%v", err)
	}
}

func TestClaudeDeferredStartupFailureIsVisible(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nprintf '%s\\n' 'native configuration rejected' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := (ClaudeCodeRuntime{Command: launcher}).Start(ctx, StartRequest{Workspace: t.TempDir(), DeferInitialPrompt: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case r, ok := <-session.Result():
		if !ok || r.Status != "failed" || !strings.Contains(r.Summary, "configuration rejected") {
			t.Fatalf("startup failure lost: %#v open=%v", r, ok)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClaudeStructuredBashTaskOutcome(t *testing.T) {
	cases := []struct {
		name string
		events []string
		wantStatus string
		wantCode string
		wantTechnical string
	}{
		{
			name: "failed command without recovery",
			events: []string{
				`{"type":"assistant","uuid":"call1","message":{"content":[{"type":"tool_use","id":"bash1","name":"Bash","input":{"command":"/bin/sh -c 'exit 7'"}}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash1","content":"failed"}]},"toolUseResult":{"exitCode":7}}`,
				`{"type":"result","subtype":"success","result":"Задача не выполнена"}`,
			},
			wantStatus: "failed",
			wantCode: "tool_execution_failed",
			wantTechnical: "exit code 7",
		},
		{
			name: "recovered intermediate error",
			events: []string{
				`{"type":"assistant","uuid":"call1","message":{"content":[{"type":"tool_use","id":"bash1","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash1","is_error":true}]}}`,
				`{"type":"assistant","uuid":"call2","message":{"content":[{"type":"tool_use","id":"bash2","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash2","is_error":false}]},"toolUseResult":{"exitCode":0}}`,
				`{"type":"result","subtype":"success","result":"Recovered","structured_output":{"task_outcome":"succeeded","reason":"Recovered by alternate verification","recovered_tool_use_ids":["bash1"],"user_response":"Recovered"}}`,
			},
			wantStatus: "succeeded",
		},
		{
			name: "unrelated successful Bash does not erase failure",
			events: []string{
				`{"type":"assistant","uuid":"call1","message":{"content":[{"type":"tool_use","id":"bash1","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash1","is_error":true}]}}`,
				`{"type":"assistant","uuid":"call2","message":{"content":[{"type":"tool_use","id":"bash2","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash2","is_error":false}]},"toolUseResult":{"exitCode":0}}`,
				`{"type":"result","subtype":"success","result":"Not actually recovered"}`,
			},
			wantStatus: "failed",
			wantCode: "tool_execution_failed",
		},
		{
			name: "unrelated attestation cannot recover Bash",
			events: []string{
				`{"type":"assistant","uuid":"call1","message":{"content":[{"type":"tool_use","id":"bash1","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash1","is_error":true}]}}`,
				`{"type":"result","subtype":"success","result":"Done","structured_output":{"task_outcome":"succeeded","reason":"Another command completed","recovered_tool_use_ids":["other-call"],"user_response":"Done"}}`,
			},
			wantStatus: "failed", wantCode: "tool_execution_failed",
		},
		{
			name: "invalid attestation cannot recover Bash",
			events: []string{
				`{"type":"assistant","uuid":"call1","message":{"content":[{"type":"tool_use","id":"bash1","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash1","is_error":true}]}}`,
				`{"type":"result","subtype":"success","result":"Done","structured_output":{"task_outcome":"unknown","reason":"not valid","recovered_tool_use_ids":["bash1"],"user_response":"Recovered"}}`,
			},
			wantStatus: "failed", wantCode: "tool_execution_failed", wantTechnical: "invalid structured task outcome",
		},
		{
			name: "structured failed without Bash",
			events: []string{`{"type":"result","subtype":"success","result":"Failed","structured_output":{"task_outcome":"failed","reason":"Could not complete task","recovered_tool_use_ids":[],"user_response":"Failed"}}`},
			wantStatus: "failed", wantCode: "task_outcome_failed", wantTechnical: "Could not complete task",
		},
		{
			name: "structured succeeded without Bash",
			events: []string{`{"type":"result","subtype":"success","result":"Done","structured_output":{"task_outcome":"succeeded","reason":"Completed","recovered_tool_use_ids":[],"user_response":"Done"}}`},
			wantStatus: "succeeded",
		},
		{
			name: "native failure overrides structured success",
			events: []string{`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Provider failed","structured_output":{"task_outcome":"succeeded","reason":"Claimed success","recovered_tool_use_ids":[],"user_response":"Provider failed"}}`},
			wantStatus: "failed",
		},
		{
			name: "invalid structured task outcome without Bash",
			events: []string{`{"type":"result","subtype":"success","result":"Unverified","structured_output":{"task_outcome":"unknown","reason":"Unknown","recovered_tool_use_ids":[]}}`},
			wantStatus: "failed", wantCode: "task_outcome_failed",
		},
		{
			name: "missing structured outcome without evidence",
			events: []string{`{"type":"result","subtype":"success","result":"Unverified"}`},
			wantStatus: "failed", wantCode: "task_outcome_failed", wantTechnical: "no structured task outcome",
		},
		{
			name: "normal text-only answer fallback",
			events: []string{
				`{"type":"assistant","uuid":"text-answer","message":{"content":[{"type":"text","text":"A direct answer"}]}}`,
				`{"type":"result","subtype":"success","result":"Done"}`,
			},
			wantStatus: "succeeded",
		},
		{
			name: "tool use forbids text-only fallback",
			events: []string{
				`{"type":"assistant","uuid":"tool-answer","message":{"content":[{"type":"text","text":"Attempted task"},{"type":"tool_use","id":"other-tool","name":"Read"}]}}`,
				`{"type":"result","subtype":"success","result":"Done"}`,
			},
			wantStatus: "failed", wantCode: "task_outcome_failed", wantTechnical: "no structured task outcome",
		},
		{
			name: "Bash exit seven and valid structured failure",
			events: []string{
				`{"type":"assistant","uuid":"bash-call","message":{"content":[{"type":"tool_use","id":"bash-seven","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash-seven","is_error":true}]},"toolUseResult":{"exitCode":7}}`,
				`{"type":"result","subtype":"success","result":"Task failed","structured_output":{"task_outcome":"failed","reason":"Original task could not complete","recovered_tool_use_ids":[],"user_response":"Task failed"}}`,
			},
			wantStatus: "failed", wantCode: "tool_execution_failed", wantTechnical: "Original task could not complete",
		},
		{
			name: "native failure",
			events: []string{`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Native failed"}`},
			wantStatus: "failed",
		},
		{
			name: "cancellation wins",
			events: []string{
				`{"type":"assistant","uuid":"call1","message":{"content":[{"type":"tool_use","id":"bash1","name":"Bash"}]}}`,
				`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"bash1","is_error":true}]}}`,
				`{"type":"result","subtype":"success","result":"Interrupted"}`,
			},
			wantStatus: "canceled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := &claudeSession{
				id: "native-id",
				active: true,
				inputID: "input-id",
				seen: make(map[string]bool),
				bashCalls: make(map[string]bool),
				result: make(chan Result, 2),
				activity: make(chan Activity, 16),
			}
			if tc.name == "cancellation wins" {
				session.cancel = true
			}
			for _, event := range tc.events {
				session.handleLine([]byte(event))
			}
			select {
			case result := <-session.Result():
				if result.Status != tc.wantStatus || result.FailureCode != tc.wantCode {
					t.Fatalf("terminal=%#v, want status=%q failure_code=%q", result, tc.wantStatus, tc.wantCode)
				}
				if tc.wantTechnical != "" && !strings.Contains(result.Technical, tc.wantTechnical) {
					t.Fatalf("technical=%q, want %q", result.Technical, tc.wantTechnical)
				}
			default:
				t.Fatal("terminal result missing")
			}
			select {
			case duplicate := <-session.Result():
				t.Fatalf("duplicate terminal result: %#v", duplicate)
			default:
			}
		})
	}
}

func TestClaudeUserResponseSeparatedFromNativeVerdict(t *testing.T) {
	cases := []struct {
		name        string
		assistant   string
		terminal    string
		wantStatus  string
		wantSummary string
		wantCode    string
	}{
		{
			name:      "worker question",
			assistant: `{"type":"assistant","uuid":"response","message":{"content":[{"type":"text","text":"{\"reason\":\"Internal\",\"recovered_tool_use_ids\":[]}"}]}}`,
			terminal: `{"type":"result","subtype":"success","result":"{\"reason\":\"Internal\",\"recovered_tool_use_ids\":[]}","structured_output":{"task_outcome":"succeeded","reason":"Internal","recovered_tool_use_ids":[],"user_response":"Какой стиль выбрать?"}}`,
			wantStatus: "succeeded",
			wantSummary: "Какой стиль выбрать?",
		},
		{
			name:     "secretary direct recall",
			terminal: `{"type":"result","subtype":"success","result":"{\"task_outcome\":\"succeeded\",\"reason\":\"Answered\",\"recovered_tool_use_ids\":[]}","structured_output":{"task_outcome":"succeeded","reason":"Answered","recovered_tool_use_ids":[],"user_response":"ЛИСТ-6783"}}`,
			wantStatus: "succeeded",
			wantSummary: "ЛИСТ-6783",
		},
		{
			name: "missing user response with plain result fails closed",
			terminal: `{"type":"result","subtype":"success","result":"I checked the task and responded earlier.","structured_output":{"task_outcome":"succeeded","reason":"Completed","recovered_tool_use_ids":[]}}`,
			wantStatus: "failed",
			wantSummary: "Claude Code returned invalid structured task outcome",
			wantCode: "task_outcome_failed",
		},
		{
			name: "empty user response with plain result fails closed",
			terminal: `{"type":"result","subtype":"success","result":"I answered.","structured_output":{"task_outcome":"succeeded","reason":"Completed","recovered_tool_use_ids":[],"user_response":"  "}}`,
			wantStatus: "failed",
			wantSummary: "Claude Code returned invalid structured task outcome",
			wantCode: "task_outcome_failed",
		},
		{
			name: "wrapped internal verdict is not activity",
			assistant: "{\"type\":\"assistant\",\"uuid\":\"wrapped\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"Internal verdict:\\n```json\\n{\\\"task_outcome\\\":\\\"succeeded\\\",\\\"reason\\\":\\\"Internal\\\",\\\"recovered_tool_use_ids\\\":[]}\\n```\"}]}}",
			terminal: `{"type":"result","subtype":"success","result":"Internal verdict","structured_output":{"task_outcome":"succeeded","reason":"Internal","recovered_tool_use_ids":[],"user_response":"Готово"}}`,
			wantStatus: "succeeded",
			wantSummary: "Готово",
		},
		{
			name:     "failed native provider precedence",
			terminal: `{"type":"result","subtype":"error_during_execution","result":"Provider error","structured_output":{"task_outcome":"succeeded","reason":"Reported success","recovered_tool_use_ids":[],"user_response":"Done"}}`,
			wantStatus: "failed",
			wantSummary: "Done",
		},
		{
			name:     "service JSON without user response fails closed",
			terminal: `{"type":"result","subtype":"success","result":"{\"task_outcome\":\"succeeded\",\"reason\":\"Internal\",\"recovered_tool_use_ids\":[]}","structured_output":{"task_outcome":"succeeded","reason":"Internal","recovered_tool_use_ids":[]}}`,
			wantStatus: "failed",
			wantSummary: "Claude Code returned invalid structured task outcome",
			wantCode: "task_outcome_failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &claudeSession{id: "native-id", active: true, inputID: "input-id", seen: map[string]bool{}, bashCalls: map[string]bool{}, result: make(chan Result, 2), activity: make(chan Activity, 16)}
			if tc.assistant != "" {
				s.handleLine([]byte(tc.assistant))
			}
			s.handleLine([]byte(tc.terminal))
			select {
			case result := <-s.Result():
				if result.Status != tc.wantStatus || result.Summary != tc.wantSummary || result.FailureCode != tc.wantCode {
					t.Fatalf("result=%#v", result)
				}
			default:
				t.Fatal("missing result")
			}
			select {
			case activity := <-s.Activity():
				if claudeServiceJSON(activity.Text) {
					t.Fatalf("leaked service activity: %#v", activity)
				}
			default:
			}
			select {
			case duplicate := <-s.Result():
				t.Fatalf("duplicate result: %#v", duplicate)
			default:
			}
		})
	}
}
