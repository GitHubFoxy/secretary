package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

func TestTelegramDeploymentIsExplicitlyGated(t *testing.T) {
	for _, key := range []string{
		"SECRETARY_TELEGRAM_ENABLED", "SECRETARY_TELEGRAM_BOT_TOKEN", "SECRETARY_TELEGRAM_SERVER_CREDENTIAL",
		"SECRETARY_TELEGRAM_OWNER_CHAT_ID", "SECRETARY_TELEGRAM_POLL_INTERVAL", "SECRETARY_TELEGRAM_FLUSH_INTERVAL",
	} {
		t.Setenv(key, "")
	}
	config, err := configuredTelegramDeployment()
	if err != nil || config.Enabled {
		t.Fatalf("disabled config=%#v err=%v", config, err)
	}

	t.Setenv("SECRETARY_TELEGRAM_ENABLED", "true")
	t.Setenv("SECRETARY_TELEGRAM_BOT_TOKEN", "fixture")
	config, err = configuredTelegramDeployment()
	if err == nil || strings.Contains(err.Error(), "fixture") || config.Enabled {
		t.Fatalf("incomplete config=%#v err=%v", config, err)
	}
}

type bridgeServer struct{}

func (bridgeServer) SendMessage(context.Context, telegram.InboundMessage) error      { return nil }
func (bridgeServer) SendWorkerMessage(context.Context, telegram.WorkerMessage) error { return nil }

type bridgeTransport struct {
	mu   sync.Mutex
	sent []telegram.OutgoingMessage
}

func (t *bridgeTransport) GetUpdates(context.Context, int64, time.Duration) ([]telegram.Update, error) {
	return nil, nil
}
func (t *bridgeTransport) SendMessage(_ context.Context, message telegram.OutgoingMessage) error {
	t.mu.Lock()
	t.sent = append(t.sent, message)
	t.mu.Unlock()
	return nil
}
func (t *bridgeTransport) CreateForumTopic(context.Context, int64, string) (telegram.ForumTopic, error) {
	return telegram.ForumTopic{}, nil
}

type bridgeState struct {
	LastEventSeq int64 `json:"last_event_seq"`
}

func TestTelegramEventBridgePaginatesAndPersistsCursor(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := 1; index <= 501; index++ {
		if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "unknown.event", Payload: map[string]int{"index": index}}); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	adapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Millisecond}, &bridgeTransport{}, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	bridgeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go bridgeTelegramEvents(bridgeCtx, store, adapter)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(statePath)
		if readErr == nil {
			var state bridgeState
			if json.Unmarshal(data, &state) == nil && state.LastEventSeq == 501 {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	data, _ := os.ReadFile(statePath)
	t.Fatalf("bridge cursor did not paginate: %s", data)
}

func TestTelegramEventBridgeMapsOfflineTerminalEvent(t *testing.T) {
	event := core.Event{Kind: "result.accepted", AggregateType: "result", WorkerRef: "worker-1", Payload: []byte(`{"status":"interrupted"}`)}
	if mapped := telegramEvent(event); mapped.Kind != "worker.offline" {
		t.Fatalf("mapped=%#v", mapped)
	}
}

func TestTelegramEventBridgeMapsDurableWorkerEventsToSafeAdapterEvents(t *testing.T) {
	event := core.Event{
		ID: "event-1", Seq: 10, Kind: "attempt.activity", AggregateType: "attempt", WorkerRef: "worker-1",
		Payload: []byte(`{"kind":"user_input_request","request":{"request_id":"request-1","summary":"answer"}}`),
	}
	mapped := telegramEvent(event)
	if mapped.Kind != "worker.needs_input" || mapped.WorkerRef != "worker-1" || mapped.Sequence != 10 {
		t.Fatalf("mapped=%#v", mapped)
	}
}
