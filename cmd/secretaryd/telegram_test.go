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

func TestTelegramDeploymentDefaultsToTwoSecondPolling(t *testing.T) {
	t.Setenv("SECRETARY_TELEGRAM_ENABLED", "true")
	t.Setenv("SECRETARY_TELEGRAM_BOT_TOKEN", "fixture")
	t.Setenv("SECRETARY_TELEGRAM_SERVER_CREDENTIAL", "fixture")
	t.Setenv("SECRETARY_TELEGRAM_POLL_INTERVAL", "")
	config, err := configuredTelegramDeployment()
	if err != nil {
		t.Fatal(err)
	}
	if config.PollInterval != 2*time.Second {
		t.Fatalf("poll interval=%s, want 2s", config.PollInterval)
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
func (t *bridgeTransport) SendChatAction(context.Context, int64, int64, string) error {
	return nil
}
func (t *bridgeTransport) CreateForumTopic(_ context.Context, chatID int64, _ string) (telegram.ForumTopic, error) {
	return telegram.ForumTopic{ChatID: chatID, ThreadID: 1}, nil
}

type bridgeState struct {
	LastEventSeq int64 `json:"last_event_seq"`
}

func TestTelegramBridgePreservesSecretaryDeltaWhitespace(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Minute}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{
		{Seq: 1, Kind: core.SecretaryTurnStartedEvent, Payload: []byte(`{"turn_id":"turn-1"}`)},
		{Seq: 2, Kind: core.SecretaryTextDeltaEvent, Payload: []byte(`{"turn_id":"turn-1","text":"I'll create"}`)},
		{Seq: 3, Kind: core.SecretaryTextDeltaEvent, Payload: []byte(`{"turn_id":"turn-1","text":" "}`)},
		{Seq: 4, Kind: core.SecretaryTextDeltaEvent, Payload: []byte(`{"turn_id":"turn-1","text":"a new Worker"}`)},
		{Seq: 5, Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(`{"turn_id":"turn-1","status":"succeeded"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "I'll create a new Worker" {
		t.Fatalf("Secretary delta text=%#v, want spaces preserved", transport.sent)
	}
}

func TestTelegramBridgeKeepsSecretaryDeltasUntilTurnFinished(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Minute}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{
		{Seq: 1, Kind: core.SecretaryTurnStartedEvent, Payload: []byte(`{"turn_id":"turn-1"}`)},
		{Seq: 2, Kind: core.SecretaryTextDeltaEvent, Payload: []byte(`{"turn_id":"turn-1","text":"Answer starts"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("sent during open turn: %#v", transport.sent)
	}
	for _, event := range []core.Event{
		{Seq: 3, Kind: core.SecretaryTextDeltaEvent, Payload: []byte(`{"turn_id":"turn-1","text":" and finishes"}`)},
		{Seq: 4, Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(`{"turn_id":"turn-1","status":"succeeded"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Answer starts and finishes" {
		t.Fatalf("Secretary response=%#v, want full response", transport.sent)
	}
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
	deadline := time.Now().Add(5 * time.Second)
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

func TestTelegramEventBridgeMapsToolActivityWithoutArgumentsOrOutput(t *testing.T) {
	for _, tc := range []struct {
		kind, want string
	}{
		{"tool_call", "worker.tool_started"},
		{"tool_result", "worker.tool_finished"},
	} {
		mapped := telegramEvent(core.Event{Kind: "attempt.activity", WorkerRef: "worker-1", Payload: []byte(`{"kind":"` + tc.kind + `","tool_call":{"name":"shell","arguments":{"token":"do-not-leak"}},"tool_result":{"name":"shell","output":"do-not-leak"}}`)})
		if mapped.Kind != tc.want || mapped.Tool != "shell" {
			t.Fatalf("mapped event lost tool status: %#v", mapped)
		}
		transport := &bridgeTransport{}
		adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
		if err != nil {
			t.Fatal(err)
		}
		if err := adapter.HandleEvent(context.Background(), telegram.Event{Kind: "worker.created", WorkerRef: "worker-1"}); err != nil {
			t.Fatal(err)
		}
		if err := adapter.HandleEvent(context.Background(), mapped); err != nil {
			t.Fatal(err)
		}
		if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "shell") || strings.Contains(transport.sent[0].Text, "do-not-leak") {
			t.Fatalf("unsafe tool progress message: %#v", transport.sent)
		}
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

func TestTelegramEventBridgeDeduplicatesOneRequestAcrossActivityAndApproval(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), telegram.Event{Kind: "worker.created", WorkerRef: "worker-1", Title: "Task"}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{
		{ID: "activity-request", Seq: 20, Kind: "attempt.activity", AggregateType: "attempt", WorkerRef: "worker-1", Payload: []byte(`{"kind":"permission_request","request":{"request_id":"request-1","summary":"run shell"}}`)},
		{ID: "approval-request", Seq: 21, Kind: "approval.requested", AggregateType: "approval", WorkerRef: "worker-1", Payload: []byte(`{"kind":"permission","request_id":"request-1","action_summary":"run shell"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	var approvals, inputs int
	for _, sent := range transport.sent {
		if strings.Contains(sent.Text, "Нужно разрешение") {
			approvals++
		}
		if strings.Contains(sent.Text, "Нужен ответ") {
			inputs++
		}
	}
	if approvals+inputs != 1 || approvals != 1 {
		t.Fatalf("request notifications approvals=%d inputs=%d messages=%#v", approvals, inputs, transport.sent)
	}
}

func TestTelegramEventBridgeMapsCanceledTerminalEvent(t *testing.T) {
	mapped := telegramEvent(core.Event{
		ID: "canceled-1", Kind: "result.accepted", AggregateType: "result", WorkerRef: "worker-1",
		Payload: []byte(`{"status":"canceled","summary":"stopped"}`),
	})
	if mapped.Kind != "worker.canceled" {
		t.Fatalf("mapped=%#v, want worker.canceled", mapped)
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
	var topicCompletions, generalMirrors int
	for _, sent := range transport.sent {
		if sent.ThreadID != 0 && sent.Text == "done" {
			topicCompletions++
		}
		if sent.ThreadID == 0 && sent.Text == "worker-1:\ndone" {
			generalMirrors++
		}
	}
	if topicCompletions != 1 || generalMirrors != 1 {
		t.Fatalf("terminal notifications topic=%d general=%d messages=%#v", topicCompletions, generalMirrors, transport.sent)
	}
}

func TestTelegramEventBridgeSkipsTextlessOutcomeSoResultDeliversSummary(t *testing.T) {
	empty := telegramEvent(core.Event{
		ID: "outcome-empty", Seq: 30, Kind: "attempt.outcome_recorded", AggregateType: "attempt", WorkerRef: "worker-1", CorrelationID: "turn-9",
		Payload: []byte(`{"status":"succeeded","classification":"final","attempt_id":"attempt-9"}`),
	})
	if empty.Kind != "" {
		t.Fatalf("textless outcome mapped=%#v, want skipped", empty)
	}
	accepted := telegramEvent(core.Event{
		ID: "result-9", Seq: 31, Kind: "result.accepted", AggregateType: "result", WorkerRef: "worker-1", CorrelationID: "turn-9",
		Payload: []byte(`{"status":"succeeded","summary":"pong"}`),
	})
	if accepted.Kind != "worker.completed" || accepted.Text != "pong" {
		t.Fatalf("result accepted mapped=%#v, want completed with pong", accepted)
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
