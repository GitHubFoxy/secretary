package telegram

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingResumeServer struct {
	fakeServer
	resumed []string
}

func (s *recordingResumeServer) ResumeWorker(_ context.Context, workerRef string) error {
	s.resumed = append(s.resumed, workerRef)
	return nil
}

func TestTelegramResumeCommandRouting(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	server := &recordingResumeServer{}
	adapter := newTestAdapter(t, transport, server, path)

	adapter.mu.Lock()
	adapter.state.Topics["w-1"] = TopicMapping{
		WorkerRef:        "w-1",
		ChatID:           100,
		ThreadID:         42,
		PendingRequestID: "req-123",
		CreatedAt:        time.Now().UTC(),
	}
	_ = adapter.saveLocked()
	adapter.mu.Unlock()

	generalUpdate := Update{
		ID: 1,
		Message: &Message{
			MessageID: 10,
			ChatID:    100,
			ThreadID:  0,
			Text:      "/resume",
		},
	}
	if err := adapter.processUpdate(ctx, generalUpdate); err != nil {
		t.Fatal(err)
	}
	if len(server.resumed) != 0 || len(server.messages) != 0 {
		t.Fatalf("expected no server work outside topic, got resumed=%v messages=%v", server.resumed, server.messages)
	}

	topicUpdate := Update{
		ID: 2,
		Message: &Message{
			MessageID: 11,
			ChatID:    100,
			ThreadID:  42,
			Text:      "/resume",
		},
	}
	if err := adapter.processUpdate(ctx, topicUpdate); err != nil {
		t.Fatal(err)
	}
	if len(server.resumed) != 1 || server.resumed[0] != "w-1" {
		t.Fatalf("expected ResumeWorker called for w-1, got %v", server.resumed)
	}

	adapter.mu.Lock()
	mapping := adapter.state.Topics["w-1"]
	adapter.mu.Unlock()
	if mapping.PendingRequestID != "req-123" {
		t.Fatalf("expected PendingRequestID req-123 preserved, got %q", mapping.PendingRequestID)
	}
}

func TestTelegramWorkerLimitedEventRendering(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"mode":      "pending",
		"resets_at": "2026-10-13T04:00:00Z",
	})

	event := Event{
		Kind:      "worker.limited",
		WorkerRef: "w-1",
		Payload:   payload,
	}

	rendered := renderWorkerEvent(event)
	expected := "Достигнут лимит Codex. Продолжу автоматически после " + time.Date(2026, 10, 13, 4, 1, 0, 0, time.UTC).Local().Format("15:04") + "."
	if rendered != expected || strings.Contains(rendered, "\n") {
		t.Fatalf("rendered = %q, want one-line %q", rendered, expected)
	}

	payload, _ = json.Marshal(map[string]any{
		"mode":      "manual_hold",
		"resets_at": "2026-10-13T04:00:00Z",
	})
	event.Payload = payload
	rendered = renderWorkerEvent(event)
	expected = "Достигнут лимит Codex. Вы можете продолжить после " + time.Date(2026, 10, 13, 4, 1, 0, 0, time.UTC).Local().Format("15:04") + "."
	if rendered != expected || strings.Contains(rendered, "\n") {
		t.Fatalf("rendered = %q, want one-line %q", rendered, expected)
	}
}
