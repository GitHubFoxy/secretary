package telegram

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerTopicPromptPrecedesActivityAndSurvivesReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, path)
	prompt := "Проверь сервер.\n\n- Исправь ошибку\n- Запусти тесты"
	event := Event{Sequence: 1, Kind: "worker.created", WorkerRef: "worker-1", TaskPrompt: prompt,
		Payload: []byte(`{"system_prompt":"private instructions","reasoning":"private reasoning","session_id":"private session"}`)}
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleDurableEvent(ctx, Event{Sequence: 2, Kind: "worker.tool_started", WorkerRef: "worker-1", Tool: "shell"}); err != nil {
		t.Fatal(err)
	}
	adapter = newTestAdapter(t, transport, &fakeServer{}, path)
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	// Also exercise replay without the durable cursor shortcut.
	if err := adapter.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if len(transport.topics) != 1 || len(transport.sent) != 2 {
		t.Fatalf("topics=%#v messages=%#v", transport.topics, transport.sent)
	}
	first := transport.sent[0]
	if first.Text != prompt || first.ThreadID != transport.topics[0].ThreadID {
		t.Fatalf("prompt lost text, formatting or destination: %#v", first)
	}
	for _, message := range transport.sent {
		if strings.Contains(message.Text, "private") || strings.Contains(message.Text, "worker-1") {
			t.Fatalf("private metadata leaked: %#v", message)
		}
	}
}

func TestWorkerTopicPromptRetryResumesAfterRestart(t *testing.T) {
	for name, failCall := range map[string]int{"first chunk": 1, "later chunk": 2} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "telegram.json")
			transport := &constrainedTransport{fakeTransport: &fakeTransport{}, failCall: failCall}
			adapter := newTestAdapter(t, transport, &fakeServer{}, path)
			prompt := strings.Repeat("Проверь 🍹\n", 600)
			event := Event{Sequence: 1, Kind: "worker.created", WorkerRef: "worker-1", TaskPrompt: prompt}
			if err := adapter.HandleDurableEvent(ctx, event); err == nil {
				t.Fatal("expected temporary delivery failure")
			}
			if adapter.LastEventSeq() != 0 || len(transport.topics) != 1 {
				t.Fatalf("failed delivery advanced cursor or recreated Topic: cursor=%d topics=%d", adapter.LastEventSeq(), len(transport.topics))
			}
			adapter = newTestAdapter(t, transport, &fakeServer{}, path)
			// Flush may deliver the pending prompt before the event is replayed.
			if err := adapter.Flush(ctx); err != nil {
				t.Fatal(err)
			}
			if err := adapter.HandleDurableEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			if err := adapter.HandleEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			for _, message := range transport.sent {
				if message.ThreadID != transport.topics[0].ThreadID {
					t.Fatalf("prompt sent outside Topic: %#v", message)
				}
				text.WriteString(message.Text)
			}
			if text.String() != prompt {
				t.Fatal("lost or duplicated prompt chunks")
			}
			if len(transport.topics) != 1 || adapter.LastEventSeq() != 1 || len(adapter.state.Outbox) != 0 || !adapter.state.Topics["worker-1"].PromptDelivered {
				t.Fatalf("delivery state: %#v", adapter.state)
			}
		})
	}
}

func TestDeterministicTopicKeepsSavedWorkerTitleWithoutExtraModelTurn(t *testing.T) {
	transport := &fakeTransport{}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "state.json"))
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Проверка сервера", TaskPrompt: "Прочитай большой task prompt и проверь сервер"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.topics) != 1 || transport.topics[0].Name != "Проверка сервера" {
		t.Fatalf("saved title replaced: %#v", transport.topics)
	}
}
