package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OpenCodeRuntime uses OpenCode's native agent configuration. The effective
// Profile is written to a per-session opencode.json and selected as the
// default primary agent, so it becomes OpenCode's agent system instruction.
type OpenCodeRuntime struct {
	Command        string
	Arguments      []string
	DataHome       string
	LegacyDataHome bool
	RawLogDir      string
	RawLogMaxBytes int64
	RawLogFiles    int
}

func (r OpenCodeRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	prepared, err := r.prepare(request)
	if err != nil {
		return nil, err
	}
	// OpenCode consumes managed MCP definitions from its private native config;
	// do not send their credentials a second time in ACP session parameters.
	request.MCPServers = nil
	return prepared.Start(ctx, request)
}

func (r OpenCodeRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	prepared, err := r.prepare(request)
	if err != nil {
		return nil, err
	}
	request.MCPServers = nil
	return prepared.Resume(ctx, request, runtimeSessionID)
}

func (r OpenCodeRuntime) prepare(request StartRequest) (ACPRuntime, error) {
	if request.Workspace == "" {
		return ACPRuntime{}, fmt.Errorf("opencode: workspace is required")
	}
	if request.Profile.Name == "" || request.Profile.Content == "" {
		return ACPRuntime{}, fmt.Errorf("opencode: managed Profile is required")
	}
	dataHome := strings.TrimSpace(r.DataHome)
	if dataHome == "" {
		return ACPRuntime{}, fmt.Errorf("opencode: selected persistent native data home is required")
	}
	if !r.LegacyDataHome {
		if err := EnsurePrivateOpenCodeNativeDataHome(dataHome); err != nil {
			return ACPRuntime{}, err
		}
	}
	configPath, err := request.Profile.openCodeConfig(request.Workspace, request.MCPServers)
	if err != nil {
		return ACPRuntime{}, err
	}
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		return ACPRuntime{}, fmt.Errorf("read OpenCode config: %w", err)
	}
	environment, err := openCodeEnvironment(request.Workspace, configPath, string(configContent), request.MCPServers, dataHome, r.LegacyDataHome)
	if err != nil {
		return ACPRuntime{}, err
	}
	command := r.Command
	if command == "" {
		command = "opencode"
	}
	arguments := append([]string(nil), r.Arguments...)
	if len(arguments) == 0 {
		arguments = []string{"acp"}
	}
	model := strings.TrimSpace(request.Profile.Model)
	if isModelAlias(model) {
		model = ""
	} else if request.Profile.Reasoning != "" && request.Profile.Reasoning != "default" {
		model += "/" + request.Profile.Reasoning
	}
	// V2 loads configured MCP asynchronously and debounces tool registration.
	// ACP session/new acknowledges the binding before that registration ends.
	// Keep the first prompt behind a short, context-cancellable startup barrier.
	var startupDelay time.Duration
	if len(request.MCPServers) > 0 {
		startupDelay = 500 * time.Millisecond
	}
	permissions, err := request.Profile.openCodePermissions(request.MCPServers)
	if err != nil {
		return ACPRuntime{}, err
	}
	return ACPRuntime{Command: command, Arguments: arguments, RawLogDir: r.RawLogDir, RawLogMaxBytes: r.RawLogMaxBytes, RawLogFiles: r.RawLogFiles, Environment: environment, ExactEnvironment: true, ModelSelection: model, ModeSelection: request.Profile.openCodeAgentName(permissions), ConfigReadyTimeout: 5 * time.Second, ModeDescription: request.Profile.openCodeDeliveryMarker(permissions), DrainPromptEvents: true, TerminalMessageGrouping: true, StartupDelay: startupDelay}, nil
}

func probeOpenCodeACPWithDataHome(ctx context.Context, command, dataHome string, legacyDataHome bool, args ...string) error {
	workspace, err := os.MkdirTemp("", "secretary-opencode-acp-probe-")
	if err != nil {
		return fmt.Errorf("opencode ACP probe: temporary workspace unavailable")
	}
	defer os.RemoveAll(workspace)
	config := map[string]any{
		"$schema":     "https://opencode.ai/config.json",
		"permissions": []map[string]string{{"action": "*", "resource": "*", "effect": "deny"}},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("opencode ACP probe: configuration unavailable")
	}
	managedDir := filepath.Join(workspace, ".secretary")
	if err := os.MkdirAll(managedDir, 0o700); err != nil {
		return fmt.Errorf("opencode ACP probe: configuration unavailable")
	}
	configPath := filepath.Join(managedDir, "opencode.json")
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return fmt.Errorf("opencode ACP probe: configuration unavailable")
	}
	environment, err := openCodeEnvironment(workspace, configPath, string(encoded), nil, dataHome, legacyDataHome)
	if err != nil {
		return fmt.Errorf("opencode ACP probe: isolated environment unavailable")
	}
	client, err := (ACPRuntime{Command: command, Arguments: args, Environment: environment, ExactEnvironment: true}).connect(ctx, "opencode-probe", ManagedProfile{}, workspace)
	if err != nil {
		return fmt.Errorf("opencode ACP initialize failed")
	}
	_ = client.Close()
	return nil
}

func environmentWithXDGDataHome(environment []string, dataHome, sandboxRoot string) []string {
	result := make([]string, 0, len(environment)+12)
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if key == "HOME" || strings.HasPrefix(key, "XDG_") || key == "CODEX_CONFIG" || strings.HasPrefix(key, "OPENCODE_") || strings.HasPrefix(key, "SECRETARY_") || isOpenCodeCredentialEnvironmentVariable(key) {
			continue
		}
		result = append(result, entry)
	}
	result = append(result,
		"HOME="+filepath.Join(sandboxRoot, "home"),
		"XDG_DATA_HOME="+dataHome,
		"XDG_CONFIG_HOME="+filepath.Join(sandboxRoot, "config"),
		"XDG_STATE_HOME="+filepath.Join(sandboxRoot, "state"),
		"XDG_CACHE_HOME="+filepath.Join(sandboxRoot, "cache"),
		"OPENCODE_CONFIG_DIR="+filepath.Join(sandboxRoot, "config", "opencode"),
		"OPENCODE_CONFIG=",
		"OPENCODE_CONFIG_CONTENT=",
		"OPENCODE_DISABLE_PROJECT_CONFIG=1",
		"OPENCODE_SERVER_URL=",
		"OPENCODE_SERVER_PASSWORD=",
		"OPENCODE_SERVER_USERNAME=",
	)
	return result
}

func isOpenCodeCredentialEnvironmentVariable(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "AWS_ACCESS_KEY_ID", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_SHARED_CREDENTIALS_FILE", "AWS_CONFIG_FILE", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "GOOGLE_APPLICATION_CREDENTIALS", "AZURE_CONFIG_DIR":
		return true
	}
	for _, suffix := range []string{"_API_KEY", "_AUTH_TOKEN", "_ACCESS_TOKEN", "_REFRESH_TOKEN", "_CLIENT_SECRET", "_SECRET_ACCESS_KEY", "_SESSION_TOKEN", "_PASSWORD", "_CREDENTIALS_FILE", "_CREDENTIALS", "_TOKEN"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	return false
}

func openCodeEnvironment(workspace, configPath, configContent string, mcpServers []MCPServer, dataHome string, legacyDataHome bool) ([]string, error) {
	managedDir := filepath.Join(workspace, ".secretary")
	privateHome := filepath.Join(managedDir, "home")
	configHome := filepath.Join(managedDir, "config")
	configDir := filepath.Join(configHome, "opencode")
	stateHome := filepath.Join(managedDir, "state")
	for _, directory := range []string{privateHome, configDir, stateHome} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, fmt.Errorf("create isolated OpenCode directory: %w", err)
		}
	}
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OPENCODE_") || strings.HasPrefix(key, "SECRETARY_") || key == "HOME" || strings.HasPrefix(key, "XDG_") || key == "CODEX_CONFIG" || isOpenCodeCredentialEnvironmentVariable(key) {
			continue
		}
		values[key] = value
	}
	if strings.TrimSpace(dataHome) == "" {
		return nil, fmt.Errorf("opencode: selected persistent native data home is required")
	}
	values["XDG_DATA_HOME"] = dataHome
	if !legacyDataHome {
		if err := EnsurePrivateOpenCodeNativeDataHome(dataHome); err != nil {
			return nil, err
		}
	}
	if values["XDG_CACHE_HOME"] == "" {
		values["XDG_CACHE_HOME"] = filepath.Join(privateHome, ".cache")
	}
	for _, server := range mcpServers {
		for _, variable := range server.Env {
			if !validEnvironmentName(variable.Name) {
				return nil, fmt.Errorf("opencode: MCP server %q has invalid environment name", server.Name)
			}
			if _, duplicate := values[variable.Name]; duplicate {
				return nil, fmt.Errorf("opencode: MCP server %q environment conflicts with inherited variable", server.Name)
			}
			values[variable.Name] = variable.Value
		}
	}
	values["HOME"] = privateHome
	values["XDG_CONFIG_HOME"] = configHome
	values["XDG_STATE_HOME"] = stateHome
	values["OPENCODE_CONFIG_DIR"] = configDir
	values["OPENCODE_CONFIG"] = configPath
	values["OPENCODE_CONFIG_CONTENT"] = configContent
	values["OPENCODE_DISABLE_PROJECT_CONFIG"] = "1"
	values["OPENCODE_SERVER_URL"] = ""
	values["OPENCODE_SERVER_PASSWORD"] = ""
	values["OPENCODE_SERVER_USERNAME"] = ""
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(values))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment, nil
}
