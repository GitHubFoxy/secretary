package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

// Protocol fixture follows v2.0.22 acp/config-option.ts and error.ts. It models
// the catalog race without a native binary or model call. Native tests separately
// prove prompt/permission delivery; a fake protocol response is not that proof.
func TestOpenCodeConfigCatalogProcess(t *testing.T) {
	if record := os.Getenv("TEST_NATIVE_XDG_DATA_HOME_RECORD"); record != "" {
		_ = os.WriteFile(record, []byte(os.Getenv("XDG_DATA_HOME")), 0o600)
	}
	helper := false
	for _, arg := range os.Args {
		helper = helper || arg == "-test.run=^TestOpenCodeConfigCatalogProcess$"
	}
	if !helper {
		return
	}
	if os.Getenv("TEST_NATIVE_XDG_DATA_HOME_RECORD") != "" {
		for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "SECRETARY_CAPABILITY", "CODEX_CONFIG"} {
			if os.Getenv(key) != "" {
				os.Exit(3)
			}
		}
	}
	var config struct {
		DefaultAgent string `json:"default_agent"`
		Agents       map[string]struct {
			Description string `json:"description"`
		} `json:"agents"`
	}
	data, err := os.ReadFile(os.Getenv("OPENCODE_CONFIG"))
	if err != nil || json.Unmarshal(data, &config) != nil {
		os.Exit(2)
	}
	if readyURL := os.Getenv("TEST_NATIVE_CONFIG_READY_URL"); readyURL != "" {
		response, err := http.Get(readyURL)
		if err != nil {
			os.Exit(4)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			os.Exit(4)
		}
	}
	counts := map[string]int{}
	writeCounts := func() {
		if record := os.Getenv("TEST_NATIVE_CONFIG_RECORD"); record != "" {
			encoded, _ := json.Marshal(counts)
			_ = os.WriteFile(record, encoded, 0o600)
		}
	}
	writeCounts()
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.ID) == 0 {
			continue
		}
		counts[request.Method]++
		if request.Method == "session/set_config_option" {
			counts[request.Params.ConfigID]++
		}
		writeCounts()
		var result any = map[string]any{}
		var rpcError any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			result = map[string]any{"sessionId": "private-original-session"}
		case "session/load":
			if os.Getenv("TEST_NATIVE_CONFIG_SCENARIO") == "long-replay" {
				for i := 0; i < 256; i++ {
					_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "historical-replay"}}}})
				}
			}
			if os.Getenv("TEST_NATIVE_CONFIG_SCENARIO") == "lost-session" {
				rpcError = map[string]any{"code": -32602, "message": "Invalid params: session not found: private-original-session"}
			}
		case "session/set_config_option":
			id, value := request.Params.ConfigID, request.Params.Value
			scenario := os.Getenv("TEST_NATIVE_CONFIG_SCENARIO")
			if scenario == "unsupported-method" {
				rpcError = map[string]any{"code": -32601, "message": "Method not found"}
				break
			}
			if scenario == "missing-mode" || scenario == "delayed" && counts[id] <= 2 {
				rpcError = map[string]any{"code": -32602, "message": "Invalid params: " + id + " not found: " + value}
				break
			}
			mode, marker := config.DefaultAgent, config.Agents[config.DefaultAgent].Description
			if scenario == "wrong-marker" {
				marker = "not-the-profile"
			}
			model, effort := "fixture/fixture-model", "low"
			if scenario == "wrong-effort" {
				effort = "high"
			}
			result = map[string]any{"configOptions": []any{
				map[string]any{"id": "mode", "currentValue": mode, "options": []any{map[string]string{"value": mode, "description": marker}}},
				map[string]any{"id": "model", "currentValue": model},
				map[string]any{"id": "effort", "currentValue": effort},
			}}
		case "session/prompt":
			if os.Getenv("TEST_NATIVE_CONFIG_SCENARIO") == "long-replay" {
				for _, text := range []string{"fresh-one", "fresh-two"} {
					_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "fresh-answer", "content": map[string]string{"type": "text", "text": text}}}})
				}
				result = map[string]any{"stopReason": "end_turn"}
			} else {
				result = map[string]any{"summary": "fake task completed"}
			}
		}
		response := map[string]any{"id": request.ID, "result": result}
		if rpcError != nil {
			delete(response, "result")
			response["error"] = rpcError
		}
		if encoder.Encode(response) != nil {
			os.Exit(2)
		}
	}
}

func TestOpenCodeRuntimeUsesSelectedPersistentStore(t *testing.T) {
	root := t.TempDir()
	personalHome := filepath.Join(root, "personal-data")
	if err := os.MkdirAll(filepath.Join(personalHome, "opencode"), 0o700); err != nil {
		t.Fatal("personal store fixture unavailable")
	}
	personalCanary := filepath.Join(personalHome, "opencode", "opencode.db")
	if err := os.WriteFile(personalCanary, []byte("personal-db-must-remain-untouched"), 0o600); err != nil {
		t.Fatal("personal store canary unavailable")
	}
	installationRoot := filepath.Join(root, "installation")
	if err := os.MkdirAll(installationRoot, 0o700); err != nil {
		t.Fatal("installation data directory unavailable")
	}
	dataHome := filepath.Join(installationRoot, "opencode-native")
	record := filepath.Join(root, "observed-xdg-data-home")
	t.Setenv("XDG_DATA_HOME", personalHome)
	t.Setenv("OPENAI_API_KEY", "fixture-private-provider-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-private-auth-token")
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-private-access-key-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-private-secret-key")
	t.Setenv("SECRETARY_CAPABILITY", "fixture-private-capability")
	t.Setenv("CODEX_CONFIG", "fixture-private-config")
	t.Setenv("TEST_NATIVE_XDG_DATA_HOME_RECORD", record)
	runtime := OpenCodeRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestOpenCodeConfigCatalogProcess$"}, DataHome: dataHome}
	request := StartRequest{WorkerRef: "private-store", Workspace: filepath.Join(root, "workspace"), Profile: ManagedProfile{Name: "worker", Content: "managed instructions"}, DeferInitialPrompt: true}
	for _, restart := range []bool{false, true} {
		session, err := runtime.Start(context.Background(), request)
		if err != nil {
			t.Fatal("OpenCode failed to start from the selected native store")
		}
		id := session.ID()
		if restart {
			if err := session.Close(); err != nil {
				t.Fatal("OpenCode fixture close failed")
			}
			session, err = runtime.Resume(context.Background(), request, id)
			if err != nil || session.ID() != id {
				t.Fatal("OpenCode Resume did not retain the existing native session ID")
			}
		}
		if err := session.Close(); err != nil {
			t.Fatal("OpenCode fixture close failed")
		}
		observed, err := os.ReadFile(record)
		if err != nil || string(observed) != dataHome {
			t.Fatal("runtime did not use the selected persistent native store")
		}
	}
	canary, err := os.ReadFile(personalCanary)
	if err != nil || string(canary) != "personal-db-must-remain-untouched" {
		t.Fatal("runtime modified the personal OpenCode database")
	}
	for _, dir := range []string{dataHome, filepath.Join(dataHome, "opencode")} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatal("private native store directory is not mode 0700")
		}
	}
}

func TestOpenCodeProbeEnvironmentProcess(t *testing.T) {
	record := os.Getenv("TICKET33_PROBE_ENV_RECORD")
	if record == "" {
		return
	}
	values := map[string]any{
		"home": os.Getenv("HOME"), "data_home": os.Getenv("XDG_DATA_HOME"),
		"config_home": os.Getenv("XDG_CONFIG_HOME"), "state_home": os.Getenv("XDG_STATE_HOME"),
		"cache_home": os.Getenv("XDG_CACHE_HOME"), "config": os.Getenv("OPENCODE_CONFIG"),
		"config_content": os.Getenv("OPENCODE_CONFIG_CONTENT"), "disable_project_config": os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG"),
		"openai_key_present": os.Getenv("OPENAI_API_KEY") != "", "anthropic_token_present": os.Getenv("ANTHROPIC_AUTH_TOKEN") != "",
		"aws_access_key_id_present": os.Getenv("AWS_ACCESS_KEY_ID") != "", "aws_secret_present": os.Getenv("AWS_SECRET_ACCESS_KEY") != "", "secretary_capability_present": os.Getenv("SECRETARY_CAPABILITY") != "",
	}
	encoded, _ := json.Marshal(values)
	if os.WriteFile(record, encoded, 0o600) != nil {
		os.Exit(2)
	}
}

func TestOpenCodeProbeCommandsUsePrivateConfigAndNoAmbientCredentials(t *testing.T) {
	root := t.TempDir()
	personalHome := filepath.Join(root, "personal-home")
	personalConfig := filepath.Join(root, "personal-config")
	dataHome := filepath.Join(root, "selected-native-store")
	record := filepath.Join(root, "observed-probe-environment.json")
	t.Setenv("HOME", personalHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "personal-data"))
	t.Setenv("XDG_CONFIG_HOME", personalConfig)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "personal-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "personal-cache"))
	t.Setenv("OPENCODE_CONFIG", filepath.Join(personalConfig, "opencode.json"))
	t.Setenv("OPENCODE_CONFIG_CONTENT", "personal config content")
	t.Setenv("OPENAI_API_KEY", "fixture-private-provider-key")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "fixture-private-auth-token")
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-private-access-key-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-private-secret-key")
	t.Setenv("SECRETARY_CAPABILITY", "fixture-private-capability")
	t.Setenv("TICKET33_PROBE_ENV_RECORD", record)
	result, err := (ExecCommandRunner{OpenCodeDataHome: dataHome}).Run(context.Background(), os.Args[0], "-test.run=^TestOpenCodeProbeEnvironmentProcess$")
	if err != nil || result.ExitCode != 0 {
		t.Fatal("OpenCode inventory command inherited an unsafe or unavailable environment")
	}
	encoded, err := os.ReadFile(record)
	if err != nil {
		t.Fatal("OpenCode probe environment record unavailable")
	}
	var observed map[string]any
	if json.Unmarshal(encoded, &observed) != nil || observed["data_home"] != dataHome || observed["home"] == personalHome || observed["config_home"] == personalConfig || observed["state_home"] == filepath.Join(root, "personal-state") || observed["cache_home"] == filepath.Join(root, "personal-cache") || observed["config"] != "" || observed["config_content"] != "" || observed["disable_project_config"] != "1" {
		t.Fatal("OpenCode probe did not use isolated HOME/config/state and the selected native store")
	}
	for _, key := range []string{"openai_key_present", "anthropic_token_present", "aws_access_key_id_present", "aws_secret_present", "secretary_capability_present"} {
		if observed[key] != false {
			t.Fatal("OpenCode probe inherited an ambient credential")
		}
	}
	if _, err := os.Stat(observed["home"].(string)); !os.IsNotExist(err) {
		t.Fatal("private OpenCode probe environment was not removed after the command")
	}
}

func TestOpenCodeConfigurationRegistrationAndFailClosed(t *testing.T) {
	for _, scenario := range []string{"delayed", "missing-mode", "wrong-marker", "wrong-effort", "unsupported-method", "lost-session"} {
		for _, resume := range []bool{false, true} {
			if scenario == "lost-session" && !resume {
				continue
			}
			t.Run(fmt.Sprintf("%s/resume=%t", scenario, resume), func(t *testing.T) {
				t.Setenv("TEST_NATIVE_CONFIG_SCENARIO", scenario)
				record := filepath.Join(t.TempDir(), "safe-counts.json")
				t.Setenv("TEST_NATIVE_CONFIG_RECORD", record)
				ready := make(chan struct{}, 1)
				release := make(chan struct{})
				var releaseOnce sync.Once
				releaseFixture := func() { releaseOnce.Do(func() { close(release) }) }
				t.Cleanup(releaseFixture)
				fixtureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					select {
					case ready <- struct{}{}:
					default:
					}
					select {
					case <-release:
						w.WriteHeader(http.StatusNoContent)
					case <-r.Context().Done():
					}
				}))
				defer fixtureServer.Close()
				t.Setenv("TEST_NATIVE_CONFIG_READY_URL", fixtureServer.URL)
				runtime := OpenCodeRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestOpenCodeConfigCatalogProcess$"}, DataHome: filepath.Join(t.TempDir(), "native-data")}
				profile := ManagedProfile{Name: "worker", Content: "managed instructions", AllowTools: []string{"read"}, Model: "fixture/fixture-model", Reasoning: "low"}
				request := StartRequest{WorkerRef: "private", Workspace: t.TempDir(), Profile: profile, DeferInitialPrompt: true}
				// The fixture's HTTP barrier proves the child loaded its managed config
				// before ACP traffic. This outer context only bounds process startup;
				// OpenCodeRuntime's independent 5s ConfigReadyTimeout is unchanged.
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				type startResult struct {
					session Session
					err     error
				}
				started := make(chan startResult, 1)
				go func() {
					var result startResult
					if resume {
						result.session, result.err = runtime.Resume(ctx, request, "private-original-session")
					} else {
						result.session, result.err = runtime.Start(ctx, request)
					}
					started <- result
				}()
				select {
				case <-ready:
					releaseFixture()
				case <-ctx.Done():
					t.Fatal("fixture helper did not reach the managed-config ready barrier")
				}
				var session Session
				var err error
				select {
				case result := <-started:
					session, err = result.session, result.err
				case <-ctx.Done():
					t.Fatal("OpenCode public fixture operation did not finish")
				}
				outerContextExpired := ctx.Err() != nil
				if session != nil {
					defer session.Close()
				}
				runtimeErr := err
				if scenario == "delayed" {
					if runtimeErr != nil || session.ID() != "private-original-session" {
						t.Fatal("delayed exact catalog configuration failed")
					}
				} else if runtimeErr == nil {
					t.Fatal("unconfirmed configuration was accepted")
				}
				data, readErr := os.ReadFile(record)
				var counts map[string]int
				if readErr != nil || json.Unmarshal(data, &counts) != nil {
					t.Fatal("safe protocol counts unavailable")
				}
				if counts["initialize"] != 1 {
					t.Fatalf("fixture initialize RPC count: got=%d want=1", counts["initialize"])
				}
				if resume {
					if counts["session/load"] != 1 {
						t.Fatalf("fixture session/load RPC count: got=%d want=1", counts["session/load"])
					}
					if counts["session/new"] != 0 {
						t.Fatal("Resume silently replaced a missing native session")
					}
				} else if counts["session/new"] != 1 {
					t.Fatalf("fixture session/new RPC count: got=%d want=1", counts["session/new"])
				}
				if scenario != "lost-session" && (counts["mode"] == 0 || counts["session/set_config_option"] == 0) {
					t.Fatal("fixture did not observe managed-mode selection")
				}
				if scenario == "missing-mode" && (!errors.Is(runtimeErr, context.DeadlineExceeded) || outerContextExpired) {
					var rpcErr *acp.RPCError
					hasRPCError := errors.As(runtimeErr, &rpcErr)
					rpcCode := 0
					if rpcErr != nil {
						rpcCode = rpcErr.Code
					}
					t.Fatalf("missing-mode deadline classification: config_deadline=%t context_canceled=%t outer_context_expired=%t error_type=%T rpc_error=%t rpc_code=%d initialize=%d load=%d new=%d mode=%d config_rpc=%d prompt=%d", errors.Is(runtimeErr, context.DeadlineExceeded), errors.Is(runtimeErr, context.Canceled), outerContextExpired, runtimeErr, hasRPCError, rpcCode, counts["initialize"], counts["session/load"], counts["session/new"], counts["mode"], counts["session/set_config_option"], counts["session/prompt"])
				}
				if counts["session/prompt"] != 0 {
					t.Fatal("prompt sent before native configuration confirmation")
				}
				if scenario == "delayed" && (counts["mode"] != 3 || counts["model"] != 3) {
					t.Fatalf("configuration retry counts: mode=%d model=%d", counts["mode"], counts["model"])
				}
				if scenario == "unsupported-method" && counts["mode"] != 1 {
					t.Fatal("non-transient RPC failure was retried")
				}
			})
		}
	}
}

func TestOpenCodeDeliveryMarkerCoversContentAndPolicy(t *testing.T) {
	base := ManagedProfile{Name: "worker", Content: "one", Hash: "domain-hash", AllowTools: []string{"read"}, Model: "fixture/model", Reasoning: "low"}
	for _, changed := range []ManagedProfile{
		{Name: "worker", Content: "two", Hash: base.Hash, AllowTools: base.AllowTools, Model: base.Model, Reasoning: base.Reasoning},
		{Name: "worker", Content: base.Content, Hash: base.Hash, AllowTools: []string{"shell"}, Model: base.Model, Reasoning: base.Reasoning},
	} {
		if changed.openCodeAgentName(nil) == base.openCodeAgentName(nil) || changed.openCodeDeliveryMarker(nil) == base.openCodeDeliveryMarker(nil) {
			t.Fatal("native delivery marker ignored changed effective content or policy")
		}
	}
}

func TestOpenCodeDeliveryMarkerCoversGeneratedPermissionRules(t *testing.T) {
	profile := ManagedProfile{Name: "secretary", Content: "same system"}
	denied, err := profile.openCodePermissions(nil)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := profile.openCodePermissions([]MCPServer{{Name: "secretary", Command: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if profile.openCodeDeliveryMarker(denied) == profile.openCodeDeliveryMarker(allowed) {
		t.Fatal("native delivery marker ignored generated MCP policy")
	}
}

func TestOpenCodeInventoryUsesSettingsNotVariantNames(t *testing.T) {
	var catalog openCodeModelCatalog
	input := `{"data":[{"providerID":"openai","id":"observed","enabled":true,"variants":[{"id":"custom","settings":{"reasoningEffort":"xhigh"}},{"id":"high"}]},{"providerID":"openai","id":"disabled","enabled":false,"variants":[{"id":"low","settings":{"reasoningEffort":"low"}}]}]}`
	if json.Unmarshal([]byte(input), &catalog) != nil {
		t.Fatal("fixture invalid")
	}
	models, efforts := catalog.observed()
	if len(models) != 1 || models[0] != "openai/observed" || len(efforts) != 1 || efforts[0] != "xhigh" || !catalog.supportsEffort("openai/observed", "xhigh") || catalog.supportsEffort("openai/disabled", "low") || catalog.supportsEffort("openai/observed", "high") {
		t.Fatal("native metadata guessed variant effort or published disabled model")
	}
	if len(parseObservedReasoning("openai/gpt-6-luna\nopenai/gpt-6.1-sol\n")) != 0 {
		t.Fatal("plaintext model IDs imply unobserved reasoning")
	}
	if containsActivityCapability(DefaultOpenCodeProbeSpec().ActivityCapabilities, core.ActivityToolCall) {
		t.Fatal("OpenCode inventory promises unsupported explicit tool identity")
	}
}

func TestOpenCodeModelConfirmationRequiresExactEcho(t *testing.T) {
	for _, test := range []struct {
		value, model, effort string
		valid                bool
	}{
		{"provider/nested/model/xhigh", "provider/nested/model", "xhigh", true},
		{"provider/model/xhigh", "provider/model", "high", false},
		{"provider/model/xhigh", "provider/other", "xhigh", false},
	} {
		data := fmt.Sprintf(`{"configOptions":[{"id":"model","currentValue":%q},{"id":"effort","currentValue":%q}]}`, test.model, test.effort)
		var response nativeConfigResponse
		_ = json.Unmarshal([]byte(data), &response)
		if nativeModelConfirmed(response, test.value) != test.valid {
			t.Fatal("native model confirmation accepted wrong echo")
		}
	}
	if strings.Contains((ManagedProfile{Name: "worker", Content: "secret body"}).openCodeDeliveryMarker(nil), "secret body") {
		t.Fatal("profile marker leaks prompt")
	}
}
