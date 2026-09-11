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
)

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

func TestManagedProfileWritesNativeOpenCodeAgentConfig(t *testing.T) {
	workspace := t.TempDir()
	projectConfig := filepath.Join(workspace, "opencode.json")
	if err := os.WriteFile(projectConfig, []byte("project-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := ManagedProfile{Name: "worker", Content: "Follow the managed worker contract.", Hash: strings.Repeat("a", 64), Model: "openai/gpt-5", Reasoning: "high", AllowTools: []string{"bash", "read"}}
	path, err := profile.openCodeConfig(workspace)
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
	if config["default_agent"] == "" || config["model"] != profile.Model {
		t.Fatalf("config=%#v", config)
	}
	agents, ok := config["agent"].(map[string]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("agents=%#v", config["agent"])
	}
	for _, raw := range agents {
		agent := raw.(map[string]any)
		if agent["prompt"] != profile.Content || agent["permission"] != "allow" || agent["model"] != profile.Model || agent["reasoningEffort"] != profile.Reasoning {
			t.Fatalf("agent=%#v", agent)
		}
		tools := agent["tools"].(map[string]any)
		if tools["bash"] != true || tools["read"] != true {
			t.Fatalf("tools=%#v", tools)
		}
	}
	if filepath.Base(path) != "opencode.json" {
		t.Fatalf("path=%q", path)
	}
	if content, err := os.ReadFile(projectConfig); err != nil || string(content) != "project-config" {
		t.Fatalf("project config changed: %q, %v", content, err)
	}
}

func TestOpenCodeRuntimeWritesManagedConfigBeforeACPStart(t *testing.T) {
	workspace := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := OpenCodeRuntime{Command: command.Path, Arguments: command.Args[1:]}
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
