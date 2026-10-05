package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in: настоящий OpenCode, но только локальный HTTP provider.
// Реальные credentials и платные model requests не нужны.
func TestOpenCodeTitleNativeHTTPFixture(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_TITLE_E2E") != "1" {
		t.Skip("set SECRETARY_OPENCODE_TITLE_E2E=1 for native OpenCode v2 check")
	}
	t.Setenv("TITLE_FIXTURE_API_KEY", "fixture-key")
	var calls atomic.Int32
	var effort, noTools, correctTask, correctSystem atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		calls.Add(1)
		effort.Store(body["reasoning_effort"] == "minimal")
		tools, _ := body["tools"].([]any)
		noTools.Store(len(tools) == 0)
		messages, _ := body["messages"].([]any)
		for _, raw := range messages {
			message, _ := raw.(map[string]any)
			text, _ := message["content"].(string)
			if message["role"] == "user" && strings.Contains(text, "task_prompt") && strings.Contains(text, "мохито") {
				correctTask.Store(true)
			}
			if message["role"] == "system" && strings.Contains(text, "fixture-system") {
				correctSystem.Store(true)
			}
		}
		if body["model"] != "fixture-model" {
			http.Error(w, "unexpected fixture model", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []map[string]any{
			{"id": "fixture", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"role": "assistant", "content": "Безалкогольный мохито"}, "finish_reason": nil}}},
			{"id": "fixture", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}},
		} {
			encoded, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", encoded)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	generator := OpenCodeTitleGenerator{Model: "title-fixture/fixture-model", Reasoning: "minimal", Prompt: "fixture-system: Верни только название задачи.", DataHome: filepath.Join(t.TempDir(), "secretary-native-data"),
		run: func(ctx context.Context, command titleCommand) ([]byte, error) {
			// Добавляется только fake endpoint; весь harness/config/stdio путь
			// совпадает с генератором. Пользовательские configs не читаются.
			path := ""
			for _, entry := range command.Environment {
				if strings.HasPrefix(entry, "OPENCODE_CONFIG=") {
					path = strings.TrimPrefix(entry, "OPENCODE_CONFIG=")
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var cfg map[string]any
			if err := json.Unmarshal(data, &cfg); err != nil {
				return nil, err
			}
			provider := cfg["providers"].(map[string]any)["title-fixture"].(map[string]any)
			provider["package"] = "@opencode/ai/providers/openai-compatible"
			provider["env"] = []string{"TITLE_FIXTURE_API_KEY"}
			provider["settings"] = map[string]string{"baseURL": server.URL + "/v1"}
			data, err = json.Marshal(cfg)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return nil, err
			}
			return runOpenCodeTitleCommand(ctx, command)
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := generator.Generate(ctx, "Найди рецепт мохито без алкоголя.")
	if err != nil || got != "Безалкогольный мохито" {
		t.Fatalf("native title=%q err=%v", got, err)
	}
	if calls.Load() != 1 || !effort.Load() || !noTools.Load() || !correctTask.Load() || !correctSystem.Load() {
		t.Fatalf("native request checks: calls=%d effort=%v noTools=%v task=%v system=%v", calls.Load(), effort.Load(), noTools.Load(), correctTask.Load(), correctSystem.Load())
	}
}
