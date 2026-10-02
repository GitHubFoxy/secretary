package telegram

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTaskTopicTitles(t *testing.T) {
	for _, tc := range []struct {
		name, prompt, answer, want string
		err                        error
	}{
		{"recipe", "Найди рецепт мохито без алкоголя и составь список ингредиентов.", "Безалкогольный мохито", "Безалкогольный мохито", nil},
		{"code", "Fix Telegram topic titles without changing Worker dispatch.", "Fix Telegram topic titles", "Fix Telegram topic titles", nil},
		{"quoted", "Проверь тесты", "\"Проверка тестов\"", "Проверка тестов", nil},
		{"empty", "Почини доставку сообщений", "  ", "Почини доставку сообщений", nil},
		{"failure", "Почини доставку сообщений", "", "Почини доставку сообщений", errors.New("private provider diagnostic")},
		{"json", "Почини доставку сообщений", `{"title":"Название"}`, "Почини доставку сообщений", nil},
		{"multiline", "Почини доставку сообщений", "Название\nПояснение", "Почини доставку сообщений", nil},
		{"markdown", "Почини доставку сообщений", "**Название**", "Почини доставку сообщений", nil},
		{"reasoning", "Почини доставку сообщений", "<think>private reasoning</think>", "Почини доставку сообщений", nil},
		{"secret", "Почини доставку сообщений", "Bearer private-fixture", "Почини доставку сообщений", nil},
		{"native", "Почини доставку сообщений", "session_id=private-fixture", "Почини доставку сообщений", nil},
		{"control", "Почини доставку сообщений", "Название\u202e", "Почини доставку сообщений", nil},
		{"invalid utf8", "Почини доставку сообщений", "Название\xff", "Почини доставку сообщений", nil},
		{"link", "Почини доставку сообщений", "https://example.test", "Почини доставку сообщений", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{}
			adapter, err := New(Config{StatePath: filepath.Join(t.TempDir(), "state.json"), OwnerChatID: 100,
				TitleGenerator: func(_ context.Context, prompt string) (string, error) {
					if prompt != tc.prompt {
						t.Errorf("unexpected task prompt: %q", prompt)
					}
					return tc.answer, tc.err
				},
			}, transport, &fakeServer{})
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Generic title", TaskPrompt: tc.prompt}); err != nil {
				t.Fatal(err)
			}
			if len(transport.topics) != 1 || transport.topics[0].Name != tc.want {
				t.Fatalf("topics=%#v, want %q", transport.topics, tc.want)
			}
		})
	}
}

func TestTopicTitleRuneLimitAndFallback(t *testing.T) {
	for _, text := range []string{strings.Repeat("я", 100), strings.Repeat("🍹 ", 40), strings.Repeat("Проверка названий Telegram ", 6)} {
		for _, got := range []string{validTopicTitle(strings.TrimSpace(text)), fallbackTopicTitle(text)} {
			if got == "" || !utf8.ValidString(got) || utf8.RuneCountInString(got) > 60 {
				t.Fatalf("invalid title %q", got)
			}
		}
	}
	for _, tc := range []struct{ prompt, want string }{
		{"\n## Почини сервер\nПотом проверь тесты", "Почини сервер"},
		{"token=fixture-private\nПроверь тесты", "Проверь тесты"},
		{"token=fixture-private", "Задача Worker"},
		{"\u202e", "Задача Worker"},
		{"", "Задача Worker"},
	} {
		if got := fallbackTopicTitle(tc.prompt); got != tc.want {
			t.Fatalf("fallback=%q, want %q", got, tc.want)
		}
	}
}

func TestTitleTimeoutDoesNotPreventResultOrAcceptLateAnswer(t *testing.T) {
	transport := &fakeTransport{}
	release := make(chan struct{})
	finished := make(chan struct{})
	defer func() { close(release); <-finished }()
	adapter, err := New(Config{StatePath: filepath.Join(t.TempDir(), "state.json"), OwnerChatID: 100, TitleTimeout: 10 * time.Millisecond,
		TitleGenerator: func(context.Context, string) (string, error) {
			defer close(finished)
			<-release // Намеренно не соблюдает context.
			return "Поздний ответ", nil
		},
	}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 1, Kind: "worker.created", WorkerRef: "worker-1", TaskPrompt: "Проверь тесты"}); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("title generation blocked delivery")
	}
	if err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 2, Kind: "worker.completed", WorkerRef: "worker-1", Text: "Готово", TerminalIdentity: "turn-1"}); err != nil {
		t.Fatal(err)
	}
	if adapter.LastEventSeq() != 2 || len(transport.sent) != 2 || transport.topics[0].Name != "Проверь тесты" {
		t.Fatalf("cursor=%d topics=%#v sent=%#v", adapter.LastEventSeq(), transport.topics, transport.sent)
	}
}

func TestTopicTitleIsNotRegeneratedOnReplayOrRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	transport := &fakeTransport{}
	var calls atomic.Int32
	cfg := Config{StatePath: path, OwnerChatID: 100, TitleGenerator: func(context.Context, string) (string, error) {
		calls.Add(1)
		return "Проверка тестов", nil
	}}
	adapter, err := New(cfg, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	event := Event{Sequence: 1, Kind: "worker.created", WorkerRef: "worker-1", TaskPrompt: "Проверь тесты", Payload: []byte(`{"private_metadata":"do-not-copy"}`)}
	if err := adapter.HandleDurableEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	adapter, err = New(cfg, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(transport.topics) != 1 {
		t.Fatalf("calls=%d topics=%#v", calls.Load(), transport.topics)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Проверь тесты") || strings.Contains(string(data), "do-not-copy") || !strings.Contains(string(data), "Проверка тестов") {
		t.Fatalf("unexpected persisted title state: %s", data)
	}
}

func TestLegacyTopicTitleWithoutTaskDoesNotInvokeModel(t *testing.T) {
	transport := &fakeTransport{}
	adapter, err := New(Config{StatePath: filepath.Join(t.TempDir(), "state.json"), OwnerChatID: 100,
		TitleGenerator: func(context.Context, string) (string, error) {
			t.Error("model invoked without task prompt")
			return "", nil
		},
	}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "legacy", Title: "Старое название"}); err != nil {
		t.Fatal(err)
	}
	if transport.topics[0].Name != "Старое название" {
		t.Fatalf("topic=%#v", transport.topics)
	}
}
