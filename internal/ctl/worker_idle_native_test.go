package ctl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func nativeIdleRuntime(t *testing.T, nonce string, instance *core.HarnessInstance) (node.Runtime, func() int32) {
	t.Helper()
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("pinned native executable unavailable")
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "opencode v2.0.22" {
		t.Fatal("fixture requires exact native v2.0.22")
	}
	root := t.TempDir()
	old := syscall.Umask(0077)
	t.Cleanup(func() { syscall.Umask(old) })
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "personal-canary"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT"} {
		t.Setenv(key, "")
	}
	t.Setenv("FIXTURE_API_KEY", "unpaid-synthetic-key")
	var calls atomic.Int32
	var failed atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		messages, _ := body["messages"].([]any)
		tools, _ := body["tools"].([]any)
		if len(tools) == 0 {
			writeIdleNativeCompletion(w, "Fixture title")
			return
		}
		valid := body["model"] == "model" && body["reasoning_effort"] == "low" && len(tools) == 1
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			fn, _ := tool["function"].(map[string]any)
			valid = valid && fn["name"] == "read"
		}
		encoded, _ := json.Marshal(messages)
		valid = valid && strings.Contains(string(encoded), "Synthetic instructions") && strings.Contains(string(encoded), nonce)
		if !valid {
			failed.Store(true)
		}
		summary := "initial complete"
		lastUser := ""
		for _, raw := range messages {
			message, _ := raw.(map[string]any)
			if message["role"] == "user" {
				content, _ := json.Marshal(message["content"])
				lastUser = string(content)
			}
		}
		if strings.Contains(lastUser, "recall") {
			summary = "remembered=false"
			if valid && strings.Contains(string(encoded), "initial complete") {
				summary = "remembered=true"
			}
		}
		calls.Add(1)
		writeIdleNativeCompletion(w, summary)
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() {
		if failed.Load() || calls.Load() < 3 {
			t.Errorf("safe native provider evidence: valid=%t calls=%d", !failed.Load(), calls.Load())
		}
	})
	t.Setenv("TEST_IDLE_NATIVE", binary)
	t.Setenv("TEST_IDLE_PROVIDER", provider.URL+"/v1")
	instance.ReasoningLevels = []core.ObservedReasoningLevel{"low"}
	return node.OpenCodeRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestIdleNativeWrapperProcess$"}, DataHome: filepath.Join(root, "native-data")}, calls.Load
}

func writeIdleNativeCompletion(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range []map[string]any{
		{"id": "synthetic", "object": "chat.completion.chunk", "created": 1, "model": "model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": nil}}},
		{"id": "synthetic", "object": "chat.completion.chunk", "created": 1, "model": "model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}},
	} {
		encoded, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", encoded)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestIdleNativeWrapperProcess(t *testing.T) {
	if !testProcessHasArgument("-test.run=^TestIdleNativeWrapperProcess$") {
		return
	}
	path := os.Getenv("OPENCODE_CONFIG")
	data, err := os.ReadFile(path)
	if err != nil {
		os.Exit(2)
	}
	var config map[string]any
	if json.Unmarshal(data, &config) != nil {
		os.Exit(2)
	}
	providers, _ := config["providers"].(map[string]any)
	provider, _ := providers["fixture"].(map[string]any)
	if provider == nil {
		os.Exit(2)
	}
	provider["package"] = "@opencode/ai/providers/openai-compatible"
	provider["env"] = []string{"FIXTURE_API_KEY"}
	provider["settings"] = map[string]string{"baseURL": os.Getenv("TEST_IDLE_PROVIDER")}
	models, _ := provider["models"].(map[string]any)
	for _, raw := range models {
		model, _ := raw.(map[string]any)
		model["capabilities"] = map[string]any{"tools": true, "reasoning": true, "input": []string{"text"}, "output": []string{"text"}}
	}
	encoded, err := json.Marshal(config)
	if err != nil || os.WriteFile(path, encoded, 0600) != nil {
		os.Exit(2)
	}
	os.Setenv("OPENCODE_CONFIG_CONTENT", "")
	binary := os.Getenv("TEST_IDLE_NATIVE")
	if syscall.Exec(binary, []string{binary, "acp"}, os.Environ()) != nil {
		os.Exit(2)
	}
}
