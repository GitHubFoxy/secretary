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

	// Set up a worker topic mapping with a pending request ID
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

	// 1. Message outside topic (thread 0 / general): /resume should be ignored (no work triggered)
	generalUpdate := Update{
		ID: 1,
		Message: &Message{
			MessageID: 10,
			ChatID:   100,
			ThreadID: 0,
			Text:     "/resume",
		},
	}
	if err := adapter.processUpdate(ctx, generalUpdate); err != nil {
		t.Fatal(err)
	}
	if len(server.resumed) != 0 || len(server.messages) != 0 {
		t.Fatalf("expected no server work outside topic, got resumed=%v messages=%v", server.resumed, server.messages)
	}

	// 2. Message inside worker topic (thread 42): /resume calls ResumeWorker
	topicUpdate := Update{
		ID: 2,
		Message: &Message{
			MessageID: 11,
			ChatID:   100,
			ThreadID: 42,
			Text:     "/resume",
		},
	}
	if err := adapter.processUpdate(ctx, topicUpdate); err != nil {
		t.Fatal(err)
	}
	if len(server.resumed) != 1 || server.resumed[0] != "w-1" {
		t.Fatalf("expected ResumeWorker called for w-1, got %v", server.resumed)
	}

	// Verify PendingRequestID was preserved
	adapter.mu.Lock()
	mapping := adapter.state.Topics["w-1"]
	adapter.mu.Unlock()
	if mapping.PendingRequestID != "req-123" {
		t.Fatalf("expected PendingRequestID req-123 preserved, got %q", mapping.PendingRequestID)
	}
}

func TestTelegramWorkerLimitedEventRendering(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"mode":         "pending",
		"cause":        "tokens_depleted",
		"scheduled_at": "2026-10-13T04:00:00Z",
	})

	event := Event{
		Kind:      "worker.limited",
		WorkerRef: "w-1",
		Payload:   payload,
	}

	rendered := renderWorkerEvent(event)
	if !strings.Contains(rendered, "tokens_depleted") {
		t.Fatalf("expected cause in rendered message, got: %s", rendered)
	}
	if !strings.Contains(rendered, "автоматически") {
		t.Fatalf("expected mode in rendered message, got: %s", rendered)
	}
	if !strings.Contains(rendered, "2026-10-13T04:00:00Z") {
		t.Fatalf("expected scheduled_at in rendered message, got: %s", rendered)
	}
	if !strings.Contains(rendered, "/resume") {
		t.Fatalf("expected /resume instruction in rendered message, got: %s", rendered)
	}
}
