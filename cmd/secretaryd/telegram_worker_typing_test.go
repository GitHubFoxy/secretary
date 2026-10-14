package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

func TestTelegramEventBridgeMapsWorkerStartedAttemptID(t *testing.T) {
	mapped := telegramEvent(core.Event{
		Kind: "worker.started", AggregateType: "worker", AggregateID: "worker-1",
		Payload: []byte(`{"id":"attempt-1","state":"running"}`),
	})
	if mapped.Kind != "worker.started" || mapped.WorkerRef != "worker-1" || mapped.AttemptID != "attempt-1" {
		t.Fatalf("worker.started mapped=%#v", mapped)
	}
}

func TestTelegramBridgeRecoversActiveTypingFromLegacyCursor(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.RecordEvent(ctx, "worker.started", "worker-1", "attempt-1", "", map[string]string{"id": "attempt-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEvent(ctx, "attempt.outcome_recorded", "worker-1", "attempt-1", "", map[string]string{"status": "succeeded", "classification": "final"}); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	legacyState := `{"version":1,"owner_chat_id":100,"last_event_seq":1,"topics":{"worker-1":{"worker_ref":"worker-1","chat_id":100,"thread_id":77}}}`
	if err := os.WriteFile(statePath, []byte(legacyState), 0600); err != nil {
		t.Fatal(err)
	}
	adapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100}, &bridgeTransport{}, bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if !adapter.WorkerTypingRecoveryNeeded() {
		t.Fatal("legacy state did not request worker typing recovery")
	}
	if err := recoverTelegramWorkerTyping(ctx, store, adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.WorkerTypingRecoveryNeeded() {
		t.Fatal("worker typing recovery marker was not cleared")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var recovered struct {
		WorkerTyping map[string]struct {
			ThreadID int64 `json:"thread_id"`
		} `json:"worker_typing"`
	}
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatal(err)
	}
	if len(recovered.WorkerTyping) != 1 {
		t.Fatalf("recovered typing attempts=%#v", recovered.WorkerTyping)
	}
	for _, attempt := range recovered.WorkerTyping {
		if attempt.ThreadID != 77 {
			t.Fatalf("recovered typing thread=%d, want 77", attempt.ThreadID)
		}
	}
	if _, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	recovered.WorkerTyping = nil
	if err := json.Unmarshal(data, &recovered); err != nil {
		t.Fatal(err)
	}
	if len(recovered.WorkerTyping) != 0 {
		t.Fatalf("terminal event did not clear recovered typing: %#v", recovered.WorkerTyping)
	}
}

func TestTelegramBridgeRejectsPrunedWorkerTypingRecoveryGap(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	started, err := store.RecordEvent(ctx, "worker.started", "worker-1", "attempt-1", "", map[string]string{"id": "attempt-1"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	terminal, err := store.RecordEvent(ctx, "attempt.outcome_recorded", "worker-1", "attempt-1", "", map[string]string{"status": "succeeded", "classification": "final"})
	if err != nil {
		t.Fatal(err)
	}
	if !terminal.CreatedAt.After(started.CreatedAt) {
		t.Fatalf("event timestamps did not advance: started=%s terminal=%s", started.CreatedAt, terminal.CreatedAt)
	}
	if _, err := store.PruneEvents(ctx, terminal.CreatedAt); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	legacyState := fmt.Sprintf(`{"version":1,"owner_chat_id":100,"last_event_seq":%d,"topics":{"worker-1":{"worker_ref":"worker-1","chat_id":100,"thread_id":77}}}`, terminal.Seq)
	if err := os.WriteFile(statePath, []byte(legacyState), 0600); err != nil {
		t.Fatal(err)
	}
	adapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100}, &bridgeTransport{}, bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	err = recoverTelegramWorkerTyping(ctx, store, adapter)
	if err == nil || !strings.Contains(err.Error(), "event sequence gap") {
		t.Fatalf("recovery error=%v, want explicit sequence gap", err)
	}
	if !adapter.WorkerTypingRecoveryNeeded() {
		t.Fatal("incomplete recovery was marked initialized")
	}
}
