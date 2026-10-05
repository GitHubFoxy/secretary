package node

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Opt-in native OpenCode v2 ACP round-trip. The only model endpoint is a local
// synthetic HTTP fixture; no user provider credentials or paid requests are used.
func TestOpenCodeACPNativeHTTPFixture(t *testing.T) {
	runOpenCodeACPNativeHTTPFixture(t, false)
}

func TestOpenCodeACPProgressFinalNativeHTTPFixture(t *testing.T) {
	runOpenCodeACPNativeHTTPFixture(t, true)
}

func runOpenCodeACPNativeHTTPFixture(t *testing.T, progressFixture bool) {
	t.Helper()
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("set SECRETARY_OPENCODE_ACP_E2E=1 for native OpenCode v2 ACP check")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("native OpenCode v2 binary is unavailable")
	}
	versionCtx, cancelVersion := context.WithTimeout(context.Background(), 5*time.Second)
	version, err := exec.CommandContext(versionCtx, binary, "--version").Output()
	cancelVersion()
	if err != nil || !strings.Contains(string(version), "v2.") {
		t.Fatal("native ACP fixture requires OpenCode v2")
	}

	t.Setenv("FIXTURE_API_KEY", "fixture-key-never-log")
	privateRoot := t.TempDir()
	userHome := filepath.Join(privateRoot, "user-home")
	for _, dir := range []string{
		filepath.Join(userHome, ".config", "opencode", "skills", "leak-sentinel"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal("could not prepare isolated fixture directories")
		}
	}
	if err := os.WriteFile(filepath.Join(userHome, ".config", "opencode", "AGENTS.md"), []byte("SECRETARY_USER_GLOBAL_INSTRUCTION_SENTINEL"), 0o600); err != nil {
		t.Fatal("could not prepare isolated fixture instructions")
	}
	if err := os.WriteFile(filepath.Join(userHome, ".config", "opencode", "skills", "leak-sentinel", "SKILL.md"), []byte("SECRETARY_USER_GLOBAL_SKILL_SENTINEL"), 0o600); err != nil {
		t.Fatal("could not prepare isolated fixture skill")
	}
	t.Setenv("HOME", userHome)
	dataHome := filepath.Join(privateRoot, "secretary-native-data")
	personalDataHome := filepath.Join(privateRoot, "personal-data")
	if err := os.MkdirAll(filepath.Join(personalDataHome, "opencode"), 0o700); err != nil {
		t.Fatal("could not prepare personal-store canary")
	}
	personalCanary := filepath.Join(personalDataHome, "opencode", "opencode.db")
	if err := os.WriteFile(personalCanary, []byte("untouched-personal-store"), 0o600); err != nil {
		t.Fatal("could not write personal-store canary")
	}
	t.Setenv("XDG_DATA_HOME", personalDataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(privateRoot, "cache"))

	workspace := filepath.Join(privateRoot, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal("could not prepare fixture workspace")
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("SECRETARY_PROJECT_INSTRUCTION_SENTINEL"), 0o600); err != nil {
		t.Fatal("could not prepare project instruction sentinel")
	}
	if err := os.WriteFile(filepath.Join(workspace, "opencode.json"), []byte(`{"instructions":["SECRETARY_PROJECT_CONFIG_SENTINEL"]}`), 0o600); err != nil {
		t.Fatal("could not prepare project config sentinel")
	}
	if err := os.WriteFile(filepath.Join(workspace, "fixture.txt"), []byte("fixture file contents"), 0o600); err != nil {
		t.Fatal("could not prepare harmless fixture data")
	}

	var calls atomic.Int32
	var checkedModel atomic.Bool
	var checkedReasoning atomic.Bool
	var checkedSystem atomic.Bool
	var checkedNoLeak atomic.Bool
	var checkedToolPolicy atomic.Bool
	var checkedToolResult atomic.Bool
	var hasAnyToolDefinition atomic.Bool
	var allowedReadTool atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		messages, _ := body["messages"].([]any)
		tools, _ := body["tools"].([]any)
		messageText := fixtureMessageText(messages)
		if body["model"] == "fixture-model" {
			checkedModel.Store(true)
		}
		requestKeys := make([]string, 0, len(body))
		for key := range body {
			requestKeys = append(requestKeys, key)
		}
		t.Logf("synthetic request keys=%v", requestKeys)
		if body["reasoning_effort"] == "low" {
			checkedReasoning.Store(true)
		}
		if strings.Contains(messageText, "managed-secretary-fixture-system") {
			checkedSystem.Store(true)
		}
		checkedNoLeak.Store(!strings.Contains(messageText, "SECRETARY_USER_GLOBAL_INSTRUCTION_SENTINEL") &&
			!strings.Contains(messageText, "SECRETARY_USER_GLOBAL_SKILL_SENTINEL") &&
			!strings.Contains(messageText, "SECRETARY_PROJECT_INSTRUCTION_SENTINEL") &&
			!strings.Contains(messageText, "SECRETARY_PROJECT_CONFIG_SENTINEL"))
		hasAllowed, hasBuiltin := false, false
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			name, _ := function["name"].(string)
			if name != "" {
				hasAnyToolDefinition.Store(true)
			}
			hasAllowed = hasAllowed || name == "read"
			hasBuiltin = hasBuiltin || name != "" && name != "read"
		}
		if hasAllowed && !hasBuiltin {
			allowedReadTool.Store(true)
			checkedToolPolicy.Store(true)
		}
		for _, raw := range messages {
			message, _ := raw.(map[string]any)
			if message["role"] == "tool" && strings.Contains(fixtureMessageText([]any{message}), "fixture file contents") {
				checkedToolResult.Store(true)
			}
		}

		calls.Add(1)
		switch {
		case hasAllowed && !checkedToolResult.Load():
			if progressFixture {
				writeOpenAIStream(w, map[string]any{
					"id": "fixture-progress", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "fixture progress before tool\n"}, "finish_reason": nil}},
				})
			}
			writeOpenAIStream(w, map[string]any{
				"id": "fixture-call", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
					"index": 0, "id": "fixture-tool-call", "type": "function", "function": map[string]string{"name": "read", "arguments": `{"path":"fixture.txt"}`},
				}}}, "finish_reason": nil}},
			})
			writeOpenAIStream(w, map[string]any{
				"id": "fixture-call", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
			})
		case hasAllowed && checkedToolResult.Load():
			// Final deltas immediately precede terminal response. No sleeps.
			for _, delta := range []string{"fixture ", "ACP ", "completed"} {
				writeOpenAIStream(w, map[string]any{
					"id": "fixture-final", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
					"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": delta}, "finish_reason": nil}},
				})
			}
			writeOpenAIStream(w, map[string]any{
				"id": "fixture-final", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			})
		default:
			// Native ACP also makes a tool-free session-title request. It is not
			// the managed agent turn and must not consume the tool-call response.
			writeOpenAIStream(w, map[string]any{
				"id": "fixture-title", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "Fixture title"}, "finish_reason": nil}},
			})
			writeOpenAIStream(w, map[string]any{
				"id": "fixture-title", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model",
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	fixtureProfile := ManagedProfile{
		Name: "worker", Content: "managed-secretary-fixture-system", Model: "fixture/fixture-model", Reasoning: "low",
		AllowTools: []string{"read"},
	}
	fixtureProfile.Hash = HashProfile(fixtureProfile.Content, fixtureProfile.Skills, fixtureProfile.Model, fixtureProfile.Reasoning)
	t.Setenv("TEST_OPENCODE_WRAPPER", "1")
	t.Setenv("TEST_NATIVE_OPENCODE", binary)
	t.Setenv("TEST_FIXTURE_URL", server.URL+"/v1")
	mcpCommand, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal("could not prepare private test command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	session, err := (OpenCodeRuntime{Command: mcpCommand, Arguments: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "acp"}, DataHome: dataHome}).Start(ctx, StartRequest{
		WorkerRef: "private-opencode-fixture", Workspace: workspace, Profile: fixtureProfile, DeferInitialPrompt: true,
	})
	if err != nil {
		t.Fatalf("native OpenCode ACP session startup failed: %v", err)
	}
	defer session.Close()
	if session.ID() == "" {
		t.Fatal("native OpenCode did not create a private local session")
	}

	promptDone := make(chan error, 1)
	go func() {
		promptDone <- session.Prompt(ctx, "Read fixture.txt and report its contents.")
	}()
	var final Result
	sawPermission := false
	sawProgress := false
	for final.Summary == "" {
		select {
		case activity, ok := <-session.Activity():
			if !ok {
				continue
			}
			sawProgress = sawProgress || activity.Kind == ActivityText && strings.Contains(activity.Text, "fixture progress before tool")
			if activity.Kind == ActivityPermission {
				sawPermission = true
				if responder, ok := session.(Responder); ok && activity.RequestID != "" {
					responseCtx, responseCancel := context.WithTimeout(ctx, 3*time.Second)
					_ = responder.Respond(responseCtx, activity.RequestID, "denied")
					responseCancel()
				}
			}
		case result, ok := <-session.Result():
			if ok {
				final = result
			}
		case err := <-promptDone:
			if err != nil {
				t.Fatal("native OpenCode ACP prompt failed")
			}
		case <-ctx.Done():
			t.Fatal("native OpenCode ACP round-trip timed out")
		}
	}
	// select may choose Result while prior Activity is still buffered.
	for len(session.Activity()) > 0 {
		activity := <-session.Activity()
		sawProgress = sawProgress || activity.Kind == ActivityText && strings.Contains(activity.Text, "fixture progress before tool")
	}
	if progressFixture && !sawProgress {
		t.Fatal("native progress was not retained as Activity")
	}
	if final.Status != "succeeded" || final.Summary != "fixture ACP completed" || sawPermission {
		t.Fatalf("native OpenCode ACP checks failed: status=%q expected_summary=%t permission=%t requests=%d model=%t tool_policy=%t any_tool=%t read_tool=%t fixture_result=%t", final.Status, final.Summary == "fixture ACP completed", sawPermission, calls.Load(), checkedModel.Load(), checkedToolPolicy.Load(), hasAnyToolDefinition.Load(), allowedReadTool.Load(), checkedToolResult.Load())
	}
	if calls.Load() < 2 || !checkedModel.Load() || !checkedReasoning.Load() || !checkedSystem.Load() || !checkedNoLeak.Load() || !checkedToolPolicy.Load() || !checkedToolResult.Load() {
		t.Fatalf("native OpenCode profile checks failed: calls=%d model=%t reasoning=%t system=%t no_leak=%t tool_policy=%t tool_result=%t", calls.Load(), checkedModel.Load(), checkedReasoning.Load(), checkedSystem.Load(), checkedNoLeak.Load(), checkedToolPolicy.Load(), checkedToolResult.Load())
	}
	id := session.ID()
	if err := session.Close(); err != nil {
		t.Fatal("native fixture close failed")
	}
	resumed, err := (OpenCodeRuntime{Command: mcpCommand, Arguments: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "acp"}, DataHome: dataHome}).Resume(ctx, StartRequest{
		WorkerRef: "private-opencode-fixture", Workspace: workspace, Profile: fixtureProfile, DeferInitialPrompt: true,
	}, id)
	if err != nil {
		t.Fatalf("native fixture Resume failed: %v", err)
	}
	defer resumed.Close()
	if resumed.ID() != id {
		t.Fatal("native fixture Resume replaced its session")
	}
	canary, err := os.ReadFile(personalCanary)
	if err != nil || string(canary) != "untouched-personal-store" {
		t.Fatal("private native ACP fixture touched the personal OpenCode store")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "opencode", "opencode.db")); err != nil {
		t.Fatal("private target store did not retain native history")
	}
}

func writeOpenAIStream(w http.ResponseWriter, chunk map[string]any) {
	encoded, _ := json.Marshal(chunk)
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func fixtureMessageText(messages []any) string {
	var values []string
	var visit func(any)
	visit = func(value any) {
		switch current := value.(type) {
		case string:
			values = append(values, current)
		case []any:
			for _, item := range current {
				visit(item)
			}
		case map[string]any:
			for _, item := range current {
				visit(item)
			}
		}
	}
	visit(messages)
	return strings.Join(values, "\n")
}

// Opt-in native OpenCode v2 Secretary MCP round-trip. The MCP server and model
// endpoint are local synthetic fixtures; no Secretary service or credentials are used.
func TestOpenCodeSecretaryMCPNativeHTTPFixture(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("set SECRETARY_OPENCODE_ACP_E2E=1 for native OpenCode v2 MCP check")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("native OpenCode v2 binary is unavailable")
	}
	privateRoot := t.TempDir()
	userHome := filepath.Join(privateRoot, "user-home")
	if err := os.MkdirAll(userHome, 0o700); err != nil {
		t.Fatal("could not prepare isolated fixture home")
	}
	t.Setenv("HOME", userHome)
	dataHome := filepath.Join(privateRoot, "secretary-native-data")
	personalDataHome := filepath.Join(privateRoot, "personal-data")
	if err := os.MkdirAll(filepath.Join(personalDataHome, "opencode"), 0o700); err != nil {
		t.Fatal("could not prepare personal-store canary")
	}
	personalCanary := filepath.Join(personalDataHome, "opencode", "opencode.db")
	if err := os.WriteFile(personalCanary, []byte("untouched-personal-store"), 0o600); err != nil {
		t.Fatal("could not write personal-store canary")
	}
	t.Setenv("XDG_DATA_HOME", personalDataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(privateRoot, "cache"))
	t.Setenv("FIXTURE_API_KEY", "fixture-key-never-log")
	workspace := filepath.Join(privateRoot, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal("could not prepare fixture workspace")
	}
	mcpDataDir := filepath.Join(privateRoot, "mcp-state")
	if err := os.MkdirAll(mcpDataDir, 0o700); err != nil {
		t.Fatal("could not prepare fixture MCP state")
	}
	mcpBinary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal("could not prepare private MCP command")
	}
	mcpServer := MCPServer{Name: "secretary", Command: mcpBinary, Args: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "mcp-server"}, Env: []MCPEnv{
		{Name: "SECRETARY_MCP_DATA_DIR", Value: mcpDataDir},
		{Name: "SECRETARY_MCP_CAPABILITY", Value: "native-fixture-capability"},
	}}
	var requests atomic.Int32
	var sawNativeTool atomic.Bool
	var sawToolResult atomic.Bool
	var noBuiltinOrExtra atomic.Bool
	var noCredentialLeak atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		messages, _ := body["messages"].([]any)
		tools, _ := body["tools"].([]any)
		encoded, _ := json.Marshal(body)
		noCredentialLeak.Store(!strings.Contains(string(encoded), "native-fixture-capability"))
		toolResult := false
		for _, raw := range messages {
			message, _ := raw.(map[string]any)
			if message["role"] == "tool" && strings.Contains(fixtureMessageText([]any{message}), `"workers":[]`) {
				toolResult = true
			}
		}
		if toolResult {
			sawToolResult.Store(true)
		}
		toolNames := make([]string, 0, len(tools))
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			name, _ := function["name"].(string)
			toolNames = append(toolNames, name)
		}
		t.Logf("synthetic native MCP tool names=%v", toolNames)
		if len(toolNames) == 1 && toolNames[0] == "secretary_list_workers" {
			sawNativeTool.Store(true)
			noBuiltinOrExtra.Store(true)
		}
		requests.Add(1)
		switch {
		case len(toolNames) == 0:
			writeFixtureCompletion(w, "fixture-mcp-title", "Fixture title")
		case len(toolNames) == 1 && toolNames[0] == "secretary_list_workers" && !toolResult:
			writeOpenAIStream(w, map[string]any{"id": "fixture-mcp-call", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "fixture-list-workers", "type": "function", "function": map[string]string{"name": "secretary_list_workers", "arguments": `{}`}}}}, "finish_reason": nil}}})
			writeOpenAIStream(w, map[string]any{"id": "fixture-mcp-call", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}})
		case len(toolNames) == 1 && toolNames[0] == "secretary_list_workers" && toolResult:
			writeFixtureCompletion(w, "fixture-mcp-final", "fixture Secretary MCP completed")
		default:
			writeFixtureCompletion(w, "fixture-mcp-unexpected", "unexpected tool catalog")
		}
	}))
	defer server.Close()
	t.Setenv("TEST_OPENCODE_WRAPPER", "1")
	t.Setenv("TEST_NATIVE_OPENCODE", binary)
	t.Setenv("TEST_FIXTURE_URL", server.URL+"/v1")
	wrapper, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal("could not prepare private OpenCode command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	profile := ManagedProfile{Name: "secretary", Content: "managed-secretary-mcp-system", Model: "fixture/fixture-model", Reasoning: "low"}
	session, err := (OpenCodeRuntime{Command: wrapper, Arguments: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "acp"}, DataHome: dataHome}).Start(ctx, StartRequest{
		WorkerRef: "private-secretary-mcp-fixture", Workspace: workspace, Profile: profile, MCPServers: []MCPServer{mcpServer}, DeferInitialPrompt: true,
	})
	if err != nil {
		t.Fatalf("native OpenCode Secretary MCP session startup failed: %v", err)
	}
	defer session.Close()
	promptDone := make(chan error, 1)
	go func() {
		promptDone <- session.Prompt(ctx, "Call the server-owned list_workers tool and report the worker list.")
	}()
	var final Result
	sawPermission := false
	for final.Summary == "" {
		select {
		case activity, ok := <-session.Activity():
			if ok && activity.Kind == ActivityPermission {
				sawPermission = true
			}
		case result, ok := <-session.Result():
			if ok {
				final = result
			}
		case err := <-promptDone:
			if err != nil {
				t.Fatal("native OpenCode Secretary MCP prompt failed")
			}
		case <-ctx.Done():
			t.Fatal("native OpenCode Secretary MCP round-trip timed out")
		}
	}
	if statusBytes, err := os.ReadFile(filepath.Join(mcpDataDir, "fixture-status.json")); err == nil {
		t.Logf("synthetic MCP status=%s", statusBytes)
	}
	if final.Status != "succeeded" || final.Summary != "fixture Secretary MCP completed" || sawPermission {
		t.Fatalf("native Secretary MCP checks failed: status=%q expected_summary=%t permission=%t requests=%d tool=%t result=%t", final.Status, final.Summary == "fixture Secretary MCP completed", sawPermission, requests.Load(), sawNativeTool.Load(), sawToolResult.Load())
	}
	if !sawNativeTool.Load() || !sawToolResult.Load() || !noBuiltinOrExtra.Load() || !noCredentialLeak.Load() {
		t.Fatalf("native Secretary MCP provider checks failed: tool=%t tool_result=%t catalog=%t no_credential_leak=%t", sawNativeTool.Load(), sawToolResult.Load(), noBuiltinOrExtra.Load(), noCredentialLeak.Load())
	}
	statusBytes, err := os.ReadFile(filepath.Join(mcpDataDir, "fixture-status.json"))
	if err != nil {
		t.Fatal("native Secretary MCP fixture did not write status")
	}
	var status struct {
		ScopedEnvironment bool `json:"scoped_environment"`
		ToolsListed       bool `json:"tools_listed"`
		ToolCalled        bool `json:"tool_called"`
	}
	if json.Unmarshal(statusBytes, &status) != nil || !status.ScopedEnvironment || !status.ToolsListed || !status.ToolCalled {
		t.Fatal("native Secretary MCP lifecycle or per-session environment check failed")
	}
	canary, canaryErr := os.ReadFile(personalCanary)
	if canaryErr != nil || string(canary) != "untouched-personal-store" {
		t.Fatal("private native MCP fixture touched the personal OpenCode store")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "opencode", "opencode.db")); err != nil {
		t.Fatal("private target store did not retain native history")
	}
}

func writeFixtureCompletion(w http.ResponseWriter, id, content string) {
	writeOpenAIStream(w, map[string]any{"id": id, "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": content}, "finish_reason": nil}}})
	writeOpenAIStream(w, map[string]any{"id": id, "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func serveSyntheticSecretaryMCP(input io.Reader, output io.Writer) error {
	dataDir := os.Getenv("SECRETARY_MCP_DATA_DIR")
	capability := os.Getenv("SECRETARY_MCP_CAPABILITY")
	scopedEnvironment := dataDir != "" && capability == "native-fixture-capability"
	secretaryVariables := 0
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "SECRETARY_") {
			secretaryVariables++
			if key != "SECRETARY_MCP_DATA_DIR" && key != "SECRETARY_MCP_CAPABILITY" {
				scopedEnvironment = false
			}
		}
	}
	if secretaryVariables != 2 {
		scopedEnvironment = false
	}
	listed, called := false, false
	writeStatus := func() error {
		status, err := json.Marshal(map[string]bool{"scoped_environment": scopedEnvironment, "tools_listed": listed, "tool_called": called})
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dataDir, "fixture-status.json"), status, 0o600)
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var request struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  map[string]any  `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return err
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "secretary-fixture", "version": "1"}}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			listed = true
			result = map[string]any{"tools": []map[string]any{{"name": "list_workers", "description": "List server-owned Workers", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}}}}
		case "tools/call":
			if request.Params["name"] != "list_workers" {
				return fmt.Errorf("unexpected synthetic tool name")
			}
			called = true
			result = map[string]any{"content": []map[string]string{{"type": "text", "text": `{"workers":[]}`}}, "structuredContent": map[string]any{"workers": []any{}}, "isError": false}
		default:
			return fmt.Errorf("unexpected synthetic MCP method")
		}
		if err := writeStatus(); err != nil {
			return err
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}
		if err := json.NewEncoder(output).Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func testWrapperArguments() []string {
	for index, arg := range os.Args {
		if arg == "--" {
			return append([]string(nil), os.Args[index+1:]...)
		}
	}
	return nil
}

// This wrapper injects only the local HTTP provider into the per-session
// configuration, then replaces itself with the real native OpenCode binary.
func TestOpenCodeConfigWrapperProcess(t *testing.T) {
	if os.Getenv("TEST_OPENCODE_WRAPPER") != "1" {
		return
	}
	nativeArgs := testWrapperArguments()
	if len(nativeArgs) == 0 {
		t.Fatal("private fixture command is missing")
	}
	if nativeArgs[0] == "mcp-server" {
		if err := serveSyntheticSecretaryMCP(os.Stdin, os.Stdout); err != nil {
			t.Fatal("synthetic Secretary MCP fixture stopped")
		}
		return
	}
	configPath := os.Getenv("OPENCODE_CONFIG")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal("could not load private V2 config")
	}
	var config map[string]any
	if json.Unmarshal(data, &config) != nil {
		t.Fatal("could not decode private V2 config")
	}
	providers, _ := config["providers"].(map[string]any)
	provider, _ := providers["fixture"].(map[string]any)
	if provider == nil {
		t.Fatal("private V2 config has no fixture model")
	}
	provider["package"] = "@opencode/ai/providers/openai-compatible"
	provider["env"] = []string{"FIXTURE_API_KEY"}
	provider["settings"] = map[string]string{"baseURL": os.Getenv("TEST_FIXTURE_URL")}
	models, _ := provider["models"].(map[string]any)
	if len(models) == 0 {
		t.Fatal("private V2 config has no fixture model entry")
	}
	for _, configured := range models {
		model, _ := configured.(map[string]any)
		if model == nil {
			t.Fatal("private V2 config has an invalid fixture model entry")
		}
		model["capabilities"] = map[string]any{"tools": true, "reasoning": true, "input": []string{"text"}, "output": []string{"text"}}
	}
	encoded, err := json.Marshal(config)
	if err != nil || os.WriteFile(configPath, encoded, 0o600) != nil {
		t.Fatal("could not update private V2 config")
	}
	if os.Setenv("OPENCODE_CONFIG_CONTENT", "") != nil {
		t.Fatal("could not update private V2 environment")
	}
	binary := os.Getenv("TEST_NATIVE_OPENCODE")
	if binary == "" {
		t.Fatal("native OpenCode command is missing")
	}
	if err := syscall.Exec(binary, append([]string{binary}, nativeArgs...), os.Environ()); err != nil {
		t.Fatal("could not exec native OpenCode")
	}
}
