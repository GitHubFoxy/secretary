package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateFormatting(t *testing.T) {
	tests := []struct {
		name, raw, formatted string
		valid                bool
	}{
		{"ru punctuation", "Завтра напомни Михаилу проверить Telegram не удаляй папку", "Завтра напомни Михаилу проверить Telegram. Не удаляй папку.", true},
		{"en cleanup", "Please um check the file and and report back", "Please check the file and report back.", true},
		{"ru boundary", "Не удаляй папку", "Удаляй папку", false},
		{"ru negation count", "Не удаляй файл и не обновляй сервер", "Не удаляй файл и обновляй сервер", false},
		{"ru substring", "Я не удалял", "Я неудалял", false},
		{"en negation", "Please don't delete it", "Please delete it", false},
		{"added negation", "Please delete it", "Please don't delete it", false},
		{"number change", "Запусти 12 задач", "Запусти 123 задачи", false},
		{"number dropped", "Repeat 12 and 12", "Repeat 12", false},
		{"spoken number", "Отправь три документа", "Отправь два документа", false},
		{"name", "Напомни Михаилу и Анне", "Напомни Михаилу и Алине", false},
		{"path", "Удалить /tmp/project", "Удалить /tmp/projects", false},
		{"relative path", "Read config.yaml", "Read config.yml", false},
		{"identifier", "Use my_model_v3", "Use my_model_v4", false},
		{"command", "Run rm -rf /tmp/demo", "Run cp -rf /tmp/demo", false},
		{"quoted command", "Run `echo hi`", "Run `echo hello`", false},
		{"url", "Visit https://example.com/a.", "Visit https://example.com/b.", false},
		{"technical retained", "Check config.yaml and /tmp/demo and https://example.com/a", "Check config.yaml, /tmp/demo, and https://example.com/a.", true},
		{"preamble", "Позвони Анне", "Вот отформатированный текст: Позвони Анне.", false},
		{"english preamble", "call Anna", "Here is the formatted text: call Anna.", false},
		{"markdown", "call Anna", "```\ncall Anna\n```", false},
		{"empty", "call Anna", " ", false},
		{"truncated", "some long message with many words and details", "some", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFormatting(tt.raw, tt.formatted)
			if (err == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, err)
			}
		})
	}
}

func TestOpenAIFormatterRequest(t *testing.T) {
	for _, pair := range [][2]string{{"Не удаляй папку", "Не удаляй папку."}, {"Please do not delete it", "Please do not delete it."}} {
		t.Run(pair[0], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing authorization")
				}
				var request struct {
					Model     string                           `json:"model"`
					Reasoning string                           `json:"reasoning_effort"`
					Messages  []struct{ Role, Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				if request.Model != "exact-model" || request.Reasoning != "none" {
					t.Errorf("wrong model/reasoning: %+v", request)
				}
				if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[0].Content != "format only" || request.Messages[1].Role != "user" || request.Messages[1].Content != pair[0] {
					t.Errorf("wrong prompt/data separation: %+v", request)
				}
				json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": pair[1]}}}})
			}))
			defer server.Close()
			t.Setenv("TEST_FORMATTER_KEY", "test-key")
			formatter := &OpenAIFormatter{Enabled: true, Model: "exact-model", Prompt: "format only", APIKeyEnv: "TEST_FORMATTER_KEY", BaseURL: server.URL}
			actual, err := formatter.Format(context.Background(), pair[0])
			if err != nil || actual != pair[1] {
				t.Fatalf("actual=%q error=%v", actual, err)
			}
		})
	}
}

func TestOpenAIFormatterFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"length", 200, `{"choices":[{"finish_reason":"length","message":{"content":"Не удаляй папку."}}]}`},
		{"refusal", 200, `{"choices":[{"finish_reason":"stop","message":{"content":"Не удаляй папку.","refusal":"SECRET"}}]}`},
		{"missing finish", 200, `{"choices":[{"message":{"content":"Не удаляй папку."}}]}`},
		{"unsafe text", 200, `{"choices":[{"finish_reason":"stop","message":{"content":"Удаляй папку."}}]}`},
		{"empty", 200, `{"choices":[{"finish_reason":"stop","message":{"content":""}}]}`},
		{"no choices", 200, `{"choices":[]}`},
		{"API error", 401, `{"error":{"message":"SECRET credential"}}`},
		{"invalid response", 200, `SECRET`},
		{"oversized", 200, strings.Repeat("SECRET", 12000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body) }))
			defer server.Close()
			f := &OpenAIFormatter{Enabled: true, Model: "exact-model", Prompt: "format only", APIKey: "SECRET", BaseURL: server.URL}
			got, err := f.Format(context.Background(), "Не удаляй папку")
			if err == nil || got != "" {
				t.Fatalf("got=%q error=%v", got, err)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("secret leak: %v", err)
			}
		})
	}
}

func TestOpenAIFormatterConfigurationAndTimeout(t *testing.T) {
	f := &OpenAIFormatter{}
	if got, err := f.Format(context.Background(), " raw "); err != nil || got != " raw " {
		t.Fatalf("disabled: %q %v", got, err)
	}
	f.Enabled = true
	f.Model = "exact-model"
	f.Prompt = "format only"
	if _, err := f.Format(context.Background(), "hello"); err == nil {
		t.Fatal("missing key accepted")
	}
	f.APIKey = "test"
	f.Reasoning = "high"
	if _, err := f.Format(context.Background(), "hello"); err == nil {
		t.Fatal("thinking mode accepted")
	}
	f.Reasoning = "none"
	f.Model = ""
	if _, err := f.Format(context.Background(), "hello"); err == nil {
		t.Fatal("missing model substituted")
	}
	f.Model = "exact-model"
	f.Prompt = ""
	if _, err := f.Format(context.Background(), "hello"); err == nil {
		t.Fatal("empty prompt accepted")
	}
	f.Prompt = "format only"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(50 * time.Millisecond) }))
	defer server.Close()
	f.BaseURL = server.URL
	f.Timeout = 10 * time.Millisecond
	if _, err := f.Format(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
}

func TestValidateFormattingRejectsAddedProtectedTokens(t *testing.T) {
	tests := []struct{ raw, formatted string }{
		{"Позвони Ивану", "Позвони Ивану в 14:30."},
		{"не удаляй 3 файла", "не удаляй 3 файла, удали 5 файлов"},
		{"Открой страницу", "Открой страницу https://example.com."},
		{"Проверь каталог", "Проверь каталог /home/coder."},
		{"Проверь настройки", "Проверь настройки config.yaml."},
		{"Проверь 3 файла", "Проверь 3 файла и 3 каталога."},
		{"Открой https://example.com", "Открой https://example.com и https://example.com."},
		{"Проверь config.yaml", "Проверь config.yaml и config.yaml."},
	}
	for _, tt := range tests {
		if err := ValidateFormatting(tt.raw, tt.formatted); err == nil {
			t.Errorf("accepted addition: %q -> %q", tt.raw, tt.formatted)
		}
	}
	if err := ValidateFormatting("Открой https://example.com и /home/coder", "Открой https://example.com, и /home/coder."); err != nil {
		t.Fatalf("sentence punctuation rejected: %v", err)
	}
}
