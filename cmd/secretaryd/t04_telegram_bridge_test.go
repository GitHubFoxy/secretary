package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

// Exercises the durable Server event log -> Telegram adapter bridge, rather
// than only calling the adapter with synthetic events.
func TestT04ServerEventBridgeProjectsToDMAndForumWithoutReplay(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record := func(event core.EventInput) {
		t.Helper()
		if _, err := store.RecordEventWithMetadata(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	record(core.EventInput{Kind: "message.saved", Source: "client:web", Payload: map[string]any{"body": "Web message"}})
	record(core.EventInput{Kind: core.SecretaryTurnStartedEvent})
	record(core.EventInput{Kind: "conversation.entry", Payload: map[string]any{"kind": "secretary", "body": "Secretary answer"}})
	record(core.EventInput{Kind: core.SecretaryTurnFinishedEvent, Payload: map[string]any{"status": "succeeded"}})
	record(core.EventInput{Kind: "worker.spawned", AggregateType: "worker", AggregateID: "worker-1", WorkerRef: "worker-1", Payload: map[string]any{"intent": "Research"}})
	record(core.EventInput{Kind: "result.accepted", AggregateType: "result", AggregateID: "result-1", WorkerRef: "worker-1", CorrelationID: "turn-1", Payload: map[string]any{"turn_id": "turn-1", "summary": "Worker answer", "status": "succeeded"}})

	group := int64(-200)
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &bridgeTransport{}
	makeAdapter := func() *telegram.Adapter {
		t.Helper()
		a, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100, GroupChatID: &group, FlushInterval: time.Hour}, transport, &bridgeServer{})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	adapter := makeAdapter()
	if failedSeq, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatalf("bridge failed at %d: %v", failedSeq, err)
	}
	transport.mu.Lock()
	sent := append([]telegram.OutgoingMessage(nil), transport.sent...)
	transport.mu.Unlock()
	count := func(text string, chatID, threadID int64) int {
		n := 0
		for _, msg := range sent {
			if msg.ChatID == chatID && msg.ThreadID == threadID && msg.Text == text {
				n++
			}
		}
		return n
	}
	for _, text := range []string{"Web message", "Secretary answer", "Worker answer"} {
		if count(text, 100, 0) != 1 || count(text, -200, 0) != 1 {
			t.Fatalf("%q not mirrored once in DM and General: %#v", text, sent)
		}
	}
	if len(sent) != 8 { // 3 DM + 3 General + Worker Topic prompt and result.
		t.Fatalf("unexpected Telegram deliveries: %#v", sent)
	}
	adapter = makeAdapter()
	if failedSeq, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatalf("reconnected bridge failed at %d: %v", failedSeq, err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if len(transport.sent) != len(sent) {
		t.Fatalf("durable event replay duplicated sends: before=%d after=%d", len(sent), len(transport.sent))
	}
}
