package node

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestManagedProfileDeliveryMarkerIncludesAddressedReplyVersion(t *testing.T) {
	legacy := ManagedProfile{Name: "secretary", Content: "same external profile", AllowTools: []string{"read"}}
	versioned := legacy
	versioned.ReplyContractVersion = core.SecretaryReplyContractAddressedV1
	if legacy.openCodeDeliveryMarker(nil) == versioned.openCodeDeliveryMarker(nil) {
		t.Fatal("profile delivery marker ignored explicit reply contract version")
	}
	if _, err := (ManagedProfile{Name: "secretary", Content: "profile", ReplyContractVersion: "addressed-reply-v2"}).openCodeConfig(t.TempDir(), nil); err == nil {
		t.Fatal("unsupported reply contract version was accepted")
	}
}

func TestProfileEnvironmentUsesConcreteCodexModelAndReasoning(t *testing.T) {
	environment, err := profileEnvironment([]string{"FOO=bar", `CODEX_CONFIG={"service_tier":"fast"}`}, ManagedProfile{Model: "gpt-5.6-terra", Reasoning: "low"})
	if err != nil {
		t.Fatal(err)
	}
	var found map[string]any
	for _, value := range environment {
		if strings.HasPrefix(value, "CODEX_CONFIG=") {
			found = map[string]any{}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(value, "CODEX_CONFIG=")), &found); err != nil {
				t.Fatal(err)
			}
		}
	}
	if found["model"] != "gpt-5.6-terra" || found["model_reasoning_effort"] != "low" || found["service_tier"] != "fast" {
		t.Fatalf("environment=%#v", environment)
	}
	unchanged, err := profileEnvironment([]string{"FOO=bar"}, ManagedProfile{Model: "default", Reasoning: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(unchanged, "\x00") != "FOO=bar" {
		t.Fatalf("alias environment=%#v", unchanged)
	}
}

func TestManagedProfileWritesNativeOpenCodeV2AgentConfig(t *testing.T) {
	workspace := t.TempDir()
	projectConfig := filepath.Join(workspace, "opencode.json")
	if err := os.WriteFile(projectConfig, []byte("project-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := ManagedProfile{Name: "worker", Content: "Follow the managed worker contract.", Hash: strings.Repeat("a", 64), Model: "openai/gpt-5", Reasoning: "high", AllowTools: []string{"bash", "read"}}
	path, err := profile.openCodeConfig(workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	if config["default_agent"] == "" || config["model"] != profile.Model || config["agent"] != nil || config["permission"] != nil {
		t.Fatalf("config is not OpenCode v2: %#v", config)
	}
	agents, ok := config["agents"].(map[string]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("agents=%#v", config["agents"])
	}
	agent, ok := agents[config["default_agent"].(string)].(map[string]any)
	if !ok || agent["system"] != profile.Content || agent["prompt"] != nil || agent["mode"] != "primary" || agent["model"] != profile.Model || agent["variant"] != nil {
		t.Fatalf("agent=%#v", agent)
	}
	permissions, ok := agent["permissions"].([]any)
	if !ok || len(permissions) != 2 {
		t.Fatalf("agent permissions=%#v", agent["permissions"])
	}
	firstPermission := permissions[0].(map[string]any)
	secondPermission := permissions[1].(map[string]any)
	if firstPermission["action"] != "shell" || firstPermission["effect"] != "allow" || secondPermission["action"] != "read" || secondPermission["effect"] != "allow" {
		t.Fatalf("agent permissions=%#v", agent["permissions"])
	}
	rootPermissions := config["permissions"].([]any)
	rootPermission := rootPermissions[0].(map[string]any)
	if len(rootPermissions) != 1 || rootPermission["action"] != "*" || rootPermission["effect"] != "deny" {
		t.Fatalf("root permissions=%#v", config["permissions"])
	}
	providers := config["providers"].(map[string]any)
	models := providers["openai"].(map[string]any)["models"].(map[string]any)
	variants := models["gpt-5"].(map[string]any)["variants"].([]any)
	variant := variants[0].(map[string]any)
	if variant["id"] != "high" || variant["settings"].(map[string]any)["reasoningEffort"] != "high" {
		t.Fatalf("variant=%#v", variant)
	}
	if filepath.Base(path) != "opencode.json" {
		t.Fatalf("path=%q", path)
	}
	if content, err := os.ReadFile(projectConfig); err != nil || string(content) != "project-config" {
		t.Fatalf("project config changed: %q, %v", content, err)
	}
}

func TestOpenCodeSecretaryProfileOnlyAllowsServerOwnedMCPTools(t *testing.T) {
	workspace := t.TempDir()
	profile := ManagedProfile{Name: "secretary", Content: "Secretary policy", AllowTools: []string{"bash", "read"}}
	mcpServer := MCPServer{Name: "secretary", Command: "/private/secretary-mcp", Args: []string{"stdio"}, Env: []MCPEnv{{Name: "SECRETARY_MCP_DATA_DIR", Value: "/private/state"}, {Name: "SECRETARY_MCP_CAPABILITY", Value: "private-capability"}}}
	path, err := profile.openCodeConfig(workspace, []MCPServer{mcpServer})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(encoded, &config); err != nil {
		t.Fatal(err)
	}
	agent := config["agents"].(map[string]any)[config["default_agent"].(string)].(map[string]any)
	permissions := agent["permissions"].([]any)
	if len(permissions) != 1 {
		t.Fatalf("Secretary MCP permissions=%d", len(permissions))
	}
	for _, raw := range permissions {
		rule := raw.(map[string]any)
		if rule["action"] != "secretary_*" || rule["effect"] != "allow" {
			t.Fatalf("unexpected Secretary permission=%#v", rule)
		}
	}
	if config["permissions"].([]any)[0].(map[string]any)["effect"] != "deny" {
		t.Fatal("OpenCode root permissions are not deny-by-default")
	}
	mcpConfig := config["mcp"].(map[string]any)["servers"].(map[string]any)["secretary"].(map[string]any)
	if mcpConfig["type"] != "local" || mcpConfig["codemode"] != false {
		t.Fatalf("Secretary MCP config=%#v", mcpConfig)
	}
	if got := mcpConfig["command"].([]any); len(got) != 2 || got[0] != "/private/secretary-mcp" || got[1] != "stdio" {
		t.Fatalf("Secretary MCP command=%#v", got)
	}
	environment := mcpConfig["environment"].(map[string]any)
	if environment["SECRETARY_MCP_CAPABILITY"] != "{env:SECRETARY_MCP_CAPABILITY}" {
		t.Fatalf("Secretary MCP env=%#v", environment)
	}
	if strings.Contains(string(encoded), "private-capability") || strings.Contains(string(encoded), "/private/state") {
		t.Fatal("MCP credentials or runtime paths were serialized into OpenCode config")
	}
	if _, err := profile.openCodeConfig(workspace, []MCPServer{{Name: "other", Command: "other-mcp"}}); err == nil {
		t.Fatal("Secretary accepted a non-server-owned MCP")
	}
	if _, err := (ManagedProfile{Name: "worker", Content: "worker", AllowTools: []string{"unknown"}}).openCodeConfig(workspace, nil); err == nil {
		t.Fatal("unknown managed tool silently passed through")
	}
}

func TestOpenCodeEnvironmentIsolatesUserConfiguration(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".secretary", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"permissions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dataHome := filepath.Join(t.TempDir(), "data")
	originalHome := filepath.Join(t.TempDir(), "original-home")
	t.Setenv("HOME", originalHome)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("OPENCODE_CONFIG_DIR", "/user/global/opencode")
	t.Setenv("OPENCODE_DISABLE_PROJECT_CONFIG", "0")
	t.Setenv("SECRETARY_UNSCOPED_TEST_TOKEN", "must-not-reach-opencode")
	mcpServers := []MCPServer{{Name: "secretary", Command: "secretary-mcp", Env: []MCPEnv{{Name: "SECRETARY_MCP_CAPABILITY", Value: "only-session-capability"}}}}
	environment, err := openCodeEnvironment(workspace, configPath, `{"permissions":[]}`, mcpServers, dataHome, false)
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["OPENCODE_CONFIG_DIR"] == "/user/global/opencode" || values["OPENCODE_DISABLE_PROJECT_CONFIG"] != "1" || values["OPENCODE_CONFIG"] != configPath || values["OPENCODE_CONFIG_CONTENT"] != `{"permissions":[]}` {
		t.Fatalf("OpenCode config environment is not isolated")
	}
	if values["XDG_DATA_HOME"] != dataHome || values["HOME"] == originalHome || values["XDG_CONFIG_HOME"] == "" {
		t.Fatal("OpenCode isolation did not preserve provider data while replacing user config paths")
	}
	if values["SECRETARY_UNSCOPED_TEST_TOKEN"] != "" || values["SECRETARY_MCP_CAPABILITY"] != "only-session-capability" {
		t.Fatal("OpenCode did not restrict Secretary environment to per-session MCP values")
	}
}

func TestOpenCodeRuntimeWritesManagedConfigBeforeACPStart(t *testing.T) {
	workspace := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestOpenCodeConfigCatalogProcess$")
	runtime := OpenCodeRuntime{Command: command.Path, Arguments: command.Args[1:], DataHome: filepath.Join(t.TempDir(), "native-data")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: workspace, Profile: ManagedProfile{Name: "worker", Content: "native instructions", Hash: strings.Repeat("a", 64)}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	configPath := filepath.Join(workspace, ".secretary", "opencode.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("managed OpenCode config: %v", err)
	}
}

func TestManagedProfileMaterializesPortableInstructions(t *testing.T) {
	path, err := (ManagedProfile{Name: "worker", Content: "worker"}).MaterializeInstructions(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "worker") {
		t.Fatalf("body=%q", body)
	}
}
