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
func (t *bridgeTransport) CreateForumTopic(_ context.Context, chatID int64, _ string) (telegram.ForumTopic, error) {
	return telegram.ForumTopic{ChatID: chatID, ThreadID: 1}, nil
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

func TestTelegramEventBridgeMapsInputApprovalAsNeedsInput(t *testing.T) {
	mapped := telegramEvent(core.Event{
		ID: "approval-input", Seq: 11, Kind: "approval.requested", AggregateType: "approval", WorkerRef: "worker-1",
		Payload: []byte(`{"kind":"input","request_id":"request-1","action_summary":"answer"}`),
	})
	if mapped.Kind != "worker.needs_input" {
		t.Fatalf("input approval mapped=%#v, want needs_input", mapped)
	}
	permission := telegramEvent(core.Event{
		ID: "approval-permission", Seq: 12, Kind: "approval.requested", AggregateType: "approval", WorkerRef: "worker-1",
		Payload: []byte(`{"kind":"permission","request_id":"request-2","action_summary":"run shell"}`),
	})
	if permission.Kind != "worker.approval_requested" {
		t.Fatalf("permission approval mapped=%#v, want approval_requested", permission)
	}
}

func TestTelegramEventBridgeSendsOneTerminalNotificationForOneTurn(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{
		{ID: "outcome-1", Seq: 20, Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: "attempt-1", WorkerRef: "worker-1", CorrelationID: "turn-1", AttemptID: "attempt-1", Payload: []byte(`{"status":"succeeded","classification":"final","summary":"done","attempt_id":"attempt-1"}`)},
		{ID: "result-1", Seq: 21, Kind: "result.accepted", AggregateType: "result", AggregateID: "result-1", WorkerRef: "worker-1", CorrelationID: "turn-1", AttemptID: "attempt-1", Payload: []byte(`{"id":"result-1","turn_id":"turn-1","status":"succeeded","summary":"done"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	var completions int
	for _, sent := range transport.sent {
		if strings.Contains(sent.Text, "Worker завершён") {
			completions++
		}
	}
	if completions != 1 {
		t.Fatalf("terminal notifications=%d, messages=%#v", completions, transport.sent)
	}
}

func TestTelegramEventBridgeMapsTerminalOutcomeOnceAndSkipsRetryable(t *testing.T) {
	outcome := telegramEvent(core.Event{
		ID: "outcome-1", Seq: 20, Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: "attempt-1", WorkerRef: "worker-1", CorrelationID: "turn-1", AttemptID: "attempt-1",
		Payload: []byte(`{"status":"succeeded","classification":"final","summary":"done","attempt_id":"attempt-1"}`),
	})
	accepted := telegramEvent(core.Event{
		ID: "result-1", Seq: 21, Kind: "result.accepted", AggregateType: "result", AggregateID: "result-1", WorkerRef: "worker-1", CorrelationID: "turn-1", AttemptID: "attempt-1",
		Payload: []byte(`{"id":"result-1","turn_id":"turn-1","status":"succeeded","summary":"done"}`),
	})
	if outcome.Kind != "worker.completed" || accepted.Kind != "worker.completed" {
		t.Fatalf("terminal mappings outcome=%#v accepted=%#v", outcome, accepted)
	}
	if outcome.TerminalIdentity == "" || accepted.TerminalIdentity == "" || outcome.TerminalIdentity != accepted.TerminalIdentity {
		t.Fatalf("terminal identities outcome=%q accepted=%q", outcome.TerminalIdentity, accepted.TerminalIdentity)
	}
	retryable := telegramEvent(core.Event{
		ID: "retry-1", Seq: 22, Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: "attempt-2", WorkerRef: "worker-1", CorrelationID: "turn-1", AttemptID: "attempt-2",
		Payload: []byte(`{"status":"failed","classification":"retryable","summary":"temporary"}`),
	})
	if retryable.Kind != "" {
		t.Fatalf("retryable outcome became terminal notification: %#v", retryable)
	}
}
