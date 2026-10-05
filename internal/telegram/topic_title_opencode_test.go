package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func titleTextFrame(id, message, text string, end int64) []byte {
	encoded, _ := json.Marshal(map[string]any{"type": "text", "sessionID": "fixture-session", "part": map[string]any{
		"id": id, "messageID": message, "type": "text", "text": text, "time": map[string]int64{"end": end},
	}})
	return append(encoded, '\n')
}

func TestOpenCodeTitleUsesExplicitConfigAndOnlyTaskInput(t *testing.T) {
	originalHome := t.TempDir()
	t.Setenv("HOME", originalHome)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("SECRETARY_TITLE_TEST_SECRET", "fixture-private")
	t.Setenv("CODEX_CONFIG", "fixture-private")
	t.Setenv("OPENAI_API_KEY", "fixture-private-provider-key")
	t.Setenv("AWS_ACCESS_KEY_ID", "fixture-private-access-key-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fixture-private-cloud-key")
	t.Setenv("GITHUB_TOKEN", "fixture-private-token")
	t.Setenv("OPENCODE_CONFIG_CONTENT", "fixture-private")
	prompt := "Составь только короткое название задачи."
	task := "Найди рецепт мохито без алкоголя."
	dataHome := filepath.Join(t.TempDir(), "secretary-native-data")
	calls := 0
	var root string
	g := OpenCodeTitleGenerator{Model: "gpt-6-luna", Reasoning: "minimal", Prompt: prompt, DataHome: dataHome,
		run: func(_ context.Context, command titleCommand) ([]byte, error) {
			calls++
			root = command.Directory
			env := map[string]string{}
			for _, entry := range command.Environment {
				key, value, _ := strings.Cut(entry, "=")
				env[key] = value
			}
			for key := range env {
				if strings.HasPrefix(key, "SECRETARY_") || key == "CODEX_CONFIG" || key == "OPENAI_API_KEY" || key == "AWS_ACCESS_KEY_ID" || key == "AWS_SECRET_ACCESS_KEY" || key == "GITHUB_TOKEN" {
					t.Error("server configuration or provider credentials reached title process")
				}
			}
			if env["HOME"] == originalHome || env["XDG_DATA_HOME"] != dataHome || env["OPENCODE_CONFIG_CONTENT"] != "" || env["OPENCODE_DISABLE_PROJECT_CONFIG"] != "1" {
				t.Error("invalid title isolation")
			}
			if reflect.DeepEqual(command.Arguments, []string{"--version"}) {
				return []byte("opencode v2.0.22\n"), nil
			}
			want := []string{"run", "--standalone", "--agent", titleAgent, "--model", "openai/gpt-6-luna#" + titleVariant, "--format", "json", "--title", "Secretary topic title", "--log-level", "none"}
			if !reflect.DeepEqual(command.Arguments, want) {
				t.Fatalf("args=%#v", command.Arguments)
			}
			var input map[string]string
			if json.Unmarshal([]byte(command.Input), &input) != nil || !reflect.DeepEqual(input, map[string]string{"task_prompt": task}) {
				t.Error("unexpected task input")
			}
			content, err := os.ReadFile(env["OPENCODE_CONFIG"])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(content), "fixture-private") || strings.Contains(string(content), task) {
				t.Error("private data or task copied into harness config")
			}
			var cfg map[string]any
			if err := json.Unmarshal(content, &cfg); err != nil {
				t.Fatal(err)
			}
			agent := cfg["agents"].(map[string]any)[titleAgent].(map[string]any)
			if agent["system"] != prompt || agent["steps"] != float64(1) {
				t.Error("external prompt or one-step limit not applied")
			}
			permission := agent["permissions"].([]any)[0].(map[string]any)
			if permission["action"] != "*" || permission["resource"] != "*" || permission["effect"] != "deny" {
				t.Error("agent can use tools")
			}
			model := cfg["providers"].(map[string]any)["openai"].(map[string]any)["models"].(map[string]any)["gpt-6-luna"].(map[string]any)
			variant := model["variants"].([]any)[0].(map[string]any)
			if variant["id"] != titleVariant || variant["settings"].(map[string]any)["reasoningEffort"] != "minimal" || model["capabilities"].(map[string]any)["tools"] != false {
				t.Error("explicit effort or disabled tools missing")
			}
			return titleTextFrame("text-1", "message-1", "Безалкогольный мохито", 1), nil
		},
	}
	got, err := g.Generate(context.Background(), task)
	if err != nil || got != "Безалкогольный мохито" || calls != 2 {
		t.Fatalf("title=%q calls=%d err=%v", got, calls, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("title workspace retained")
	}
}

func TestOpenCodeTitlePreservesLegacyStorePermissions(t *testing.T) {
	legacyHome := t.TempDir()
	legacyApp := filepath.Join(legacyHome, "opencode")
	if err := os.Mkdir(legacyApp, 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(legacyApp, "opencode.db")
	if err := os.WriteFile(canary, []byte("existing legacy sessions"), 0o644); err != nil {
		t.Fatal(err)
	}
	generator := OpenCodeTitleGenerator{
		Model: "openai/title-model", Reasoning: "none", Prompt: "Составь название.", DataHome: legacyHome, LegacyDataHome: true,
		run: func(_ context.Context, command titleCommand) ([]byte, error) {
			for _, entry := range command.Environment {
				if strings.HasPrefix(entry, "XDG_DATA_HOME=") && entry != "XDG_DATA_HOME="+legacyHome {
					t.Fatal("title runtime selected a different legacy store")
				}
			}
			if reflect.DeepEqual(command.Arguments, []string{"--version"}) {
				return []byte("opencode v2.0.22\n"), nil
			}
			return titleTextFrame("title", "title-message", "Заголовок", 1), nil
		},
	}
	if _, err := generator.Generate(context.Background(), "Задача"); err != nil {
		t.Fatal("title runtime rejected pinned legacy store")
	}
	appInfo, appErr := os.Stat(legacyApp)
	dbInfo, dbErr := os.Stat(canary)
	data, readErr := os.ReadFile(canary)
	if appErr != nil || appInfo.Mode().Perm() != 0o755 || dbErr != nil || dbInfo.Mode().Perm() != 0o644 || readErr != nil || string(data) != "existing legacy sessions" {
		t.Fatal("title runtime changed legacy native store contents or permissions")
	}
}

func TestOpenCodeTitleRejectsHarnessErrorsWithoutPrivateDiagnostics(t *testing.T) {
	for _, name := range []string{"missing binary", "old version", "model failure", "invalid stream"} {
		t.Run(name, func(t *testing.T) {
			g := OpenCodeTitleGenerator{Model: "openai/title-model", Reasoning: "none", Prompt: "Составь название.", DataHome: filepath.Join(t.TempDir(), "secretary-native-data"),
				run: func(_ context.Context, c titleCommand) ([]byte, error) {
					if name == "missing binary" {
						return nil, errors.New("fixture-private")
					}
					if c.Arguments[0] == "--version" {
						if name == "old version" {
							return []byte("1.9.0"), nil
						}
						return []byte("2.0.22"), nil
					}
					if name == "model failure" {
						return nil, errors.New("fixture-private")
					}
					return []byte("fixture-private\n"), nil
				},
			}
			got, err := g.Generate(context.Background(), "Проверь тесты.")
			if err == nil || got != "" || strings.Contains(err.Error(), "fixture-private") {
				t.Fatalf("title=%q err=%v", got, err)
			}
		})
	}
}

func TestOpenCodeTitleStreamKeepsFinalTextAndDeduplicatesParts(t *testing.T) {
	output := append(titleTextFrame("old", "message-1", "Промежуточный текст", 1), []byte("{\"type\":\"reasoning\",\"part\":{\"type\":\"reasoning\",\"text\":\"fixture-private\"}}\n")...)
	output = append(output, titleTextFrame("new-1", "message-2", "Безалкогольный ", 1)...)
	output = append(output, titleTextFrame("new-1", "message-2", "Безалкогольный ", 1)...)
	output = append(output, titleTextFrame("new-2", "message-2", "мохито", 1)...)
	got, err := parseOpenCodeTitle(output)
	if err != nil || got != "Безалкогольный мохито" {
		t.Fatalf("title=%q err=%v", got, err)
	}
	for _, output := range [][]byte{
		nil, []byte("not-json\n"), titleTextFrame("text", "message", "Незавершённый текст", 0),
		append(titleTextFrame("text", "message", "Название", 1), []byte("{\"type\":\"error\",\"error\":\"fixture-private\"}\n")...),
		[]byte("{\"type\":\"tool_use\",\"part\":{\"type\":\"tool\",\"text\":\"fixture-private\"}}\n"),
		titleTextFrame("text", "message", strings.Repeat("x", 4097), 1),
	} {
		if got, err := parseOpenCodeTitle(output); err == nil || got != "" {
			t.Fatal("invalid title stream accepted")
		}
	}
}

func TestTitleModelParts(t *testing.T) {
	for _, tc := range []struct {
		value, provider, model string
		ok                     bool
	}{
		{"gpt-6-luna", "openai", "gpt-6-luna", true},
		{"openai/model", "openai", "model", true},
		{"openrouter/provider/model", "openrouter", "provider/model", true},
		{"/model", "", "model", false}, {"provider/", "provider", "", false}, {"model#high", "", "", false},
	} {
		provider, model, ok := titleModelParts(tc.value)
		if provider != tc.provider || model != tc.model || ok != tc.ok {
			t.Fatalf("model parts mismatch for %q", tc.value)
		}
	}
}
