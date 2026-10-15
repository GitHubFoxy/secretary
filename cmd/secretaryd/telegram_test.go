package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
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
func (bridgeServer) ResumeWorker(context.Context, string) error                      { return nil }

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

type constrainedBridgeTransport struct{ bridgeTransport }

func (t *constrainedBridgeTransport) CreateForumTopic(ctx context.Context, chatID int64, name string) (telegram.ForumTopic, error) {
	if !utf8.ValidString(name) {
		return telegram.ForumTopic{}, errors.New("strings must be encoded in UTF-8")
	}
	return t.bridgeTransport.CreateForumTopic(ctx, chatID, name)
}

func (t *constrainedBridgeTransport) SendMessage(ctx context.Context, message telegram.OutgoingMessage) error {
	if !utf8.ValidString(message.Text) || len(utf16.Encode([]rune(message.Text))) > 4096 {
		return errors.New("invalid UTF-8 or message is too long")
	}
	return t.bridgeTransport.SendMessage(ctx, message)
}

func TestTelegramBridgeDelegationAndLongResultDoNotBlockReplay(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ack := "Worker запущен. Результат придёт отдельно."
	result := strings.Repeat("Рецепт 🍹 ", 600)
	events := []core.EventInput{
		{Kind: core.SecretaryTurnStartedEvent},
		{Kind: core.SecretaryToolCallEvent, Payload: map[string]string{"tool": "mcp_secretary_spawn_worker"}},
		{Kind: "worker.spawned", AggregateType: "worker", AggregateID: "worker-1", Payload: map[string]string{"title": strings.Repeat("я", 29) + "🍹" + strings.Repeat("р", 40)}},
		{Kind: core.SecretaryTextDeltaEvent, Payload: map[string]string{"text": ack}},
		{Kind: "conversation.entry", Payload: map[string]string{"kind": "secretary", "body": ack}},
		{Kind: core.SecretaryTurnFinishedEvent, Payload: map[string]string{"status": "succeeded"}},
		{Kind: "result.accepted", WorkerRef: "worker-1", CorrelationID: "turn-1", Payload: map[string]string{"summary": result, "status": "succeeded"}},
	}
	for _, event := range events {
		if _, err := store.RecordEventWithMetadata(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	transport := &constrainedBridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if seq, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatalf("durable bridge blocked at %d: %v", seq, err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if adapter.LastEventSeq() != int64(len(events)) || len(transport.sent) != 5 {
		t.Fatalf("cursor=%d messages=%d, want 6 events and 5 messages", adapter.LastEventSeq(), len(transport.sent))
	}
	foundAck := false
	for _, sent := range transport.sent {
		if sent.Text == ack {
			foundAck = true
		}
	}
	if !foundAck {
		t.Fatalf("delegation acknowledgement missing: %#v", transport.sent)
	}
	if _, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 5 {
		t.Fatalf("replay duplicated delivery: %d messages", len(transport.sent))
	}
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
		{Seq: 5, Kind: "conversation.entry", Payload: []byte(`{"kind":"secretary","body":"I'll create a new Worker"}`)},
		{Seq: 6, Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(`{"turn_id":"turn-1","status":"succeeded"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "I&#39;ll create a new Worker" {
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
		{Seq: 4, Kind: "conversation.entry", Payload: []byte(`{"kind":"secretary","body":"Answer starts and finishes"}`)},
		{Seq: 5, Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(`{"turn_id":"turn-1","status":"succeeded"}`)},
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

func TestTelegramEventBridgeMapsClientMessageForGeneralMirror(t *testing.T) {
	mapped := telegramEvent(core.Event{
		ID: "message-event-1", Seq: 9, Kind: "message.saved", Source: "client:external",
		Payload: []byte(`{"id":"entry-1","kind":"user","body":"Hi"}`),
	})
	if mapped.Kind != "message.saved" || mapped.Source != "client:external" || mapped.Text != "Hi" {
		t.Fatalf("mapped=%#v", mapped)
	}
}

func TestTelegramEventBridgePublishesOnlyDispatchedWorkerIntent(t *testing.T) {
	ctx := context.Background()
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	event := telegramEvent(core.Event{
		ID: "spawn-1", Seq: 1, Kind: "worker.spawned", AggregateType: "worker", AggregateID: "worker-private",
		Payload: []byte(`{"intent":"Fix the server.\nRun tests.","title":"Generic title","system_prompt":"private instructions","reasoning":"private reasoning","policy_snapshot":{"token":"private credential"},"session_id":"private session"}`),
	})
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].ThreadID != 1 || transport.sent[0].Text != "Fix the server.\nRun tests." {
		t.Fatalf("dispatched prompt delivery=%#v", transport.sent)
	}

	spawnWithNode := telegramEvent(core.Event{
		ID: "spawn-node", Seq: 2, Kind: "worker.spawned", AggregateType: "worker", AggregateID: "worker-node",
		Payload: []byte(`{"intent":"Check node.","node_id":"node-primary"}`),
	})
	if spawnWithNode.NodeID != "node-primary" {
		t.Fatalf("node_id mapping failed: %#v", spawnWithNode)
	}
	if err := adapter.HandleDurableEvent(ctx, spawnWithNode); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 2 || transport.sent[1].Text != "Узел: node-primary\n\nCheck node." {
		t.Fatalf("dispatched node prompt delivery=%#v", transport.sent)
	}
	emptyResult := telegramEvent(core.Event{
		Kind: "attempt.activity", WorkerRef: "worker-1",
		Payload: []byte(`{"kind":"tool_result","tool_result":{"name":"shell","call_id":"c-empty","status":"completed","output":""}}`),
	})
	if emptyResult.HasOutput {
		t.Fatalf("expected HasOutput=false for empty output: %#v", emptyResult)
	}
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
		kind, status, want string
	}{
		{"tool_call", "", "worker.tool_started"},
		{"tool_result", "completed", "worker.tool_finished"},
		{"tool_result", "failed", "worker.tool_failed"},
	} {
		mapped := telegramEvent(core.Event{Kind: "attempt.activity", WorkerRef: "worker-1", Payload: []byte(`{"kind":"` + tc.kind + `","tool_call":{"name":"shell","call_id":"call-test-1","preview":"python3 - <<'PY' import…","arguments":{"token":"do-not-leak"}},"tool_result":{"name":"shell","call_id":"call-test-1","preview":"make test","status":"` + tc.status + `","output":"do-not-leak"}}`)})
		if mapped.Kind != tc.want || mapped.Tool != "shell" || mapped.CallID != "call-test-1" {
			t.Fatalf("mapped event lost tool lifecycle or call_id: %#v", mapped)
		}
		if tc.kind == "tool_call" && mapped.ToolPreview != "python3 - <<'PY' import…" {
			t.Fatalf("tool preview=%q", mapped.ToolPreview)
		}
		if tc.kind == "tool_result" && mapped.ToolPreview != "make test" {
			t.Fatalf("result preview=%q", mapped.ToolPreview)
		}
		if tc.kind == "tool_result" && !mapped.HasOutput {
			t.Fatalf("expected HasOutput=true for nonempty output: %#v", mapped)
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
		if tc.kind == "tool_result" && tc.status == "completed" {
			if len(transport.sent) != 0 {
				t.Fatalf("successful tool result was sent: %#v", transport.sent)
			}
			continue
		}
		if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "shell") || strings.Contains(transport.sent[0].Text, "do-not-leak") || strings.Contains(transport.sent[0].Text, "Запуск:") || strings.Contains(transport.sent[0].Text, "▶") {
			t.Fatalf("unsafe or verbose tool progress message: %#v", transport.sent)
		}
		if tc.kind == "tool_call" && transport.sent[0].Text != "shell python3 - &lt;&lt;&#39;PY&#39; import…" {
			t.Fatalf("tool start=%q", transport.sent[0].Text)
		}
		if tc.status == "failed" && transport.sent[0].Text != "✗ Ошибка: shell make test" {
			t.Fatalf("tool failure=%q", transport.sent[0].Text)
		}
	}
}

func TestTelegramEventBridgeParallelToolsWithCallIDAndReplay(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}

	spawnEvent := core.Event{
		ID: "spawn-parallel", Seq: 1, Kind: "worker.spawned", AggregateType: "worker", AggregateID: "worker-parallel",
		Payload: []byte(`{"intent":"Run parallel tasks","node_id":"node-worker-1"}`),
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(spawnEvent)); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Узел: node-worker-1\n\nRun parallel tasks" {
		t.Fatalf("spawn delivery=%#v", transport.sent)
	}

	call1 := core.Event{
		ID: "call-1", Seq: 2, Kind: "attempt.activity", AggregateType: "attempt", WorkerRef: "worker-parallel",
		Payload: []byte(`{"kind":"tool_call","tool_call":{"name":"exec_command","call_id":"c-open","preview":"open https://example.com","arguments":{"url":"https://example.com","token":"secret-token"}}}`),
	}
	call2 := core.Event{
		ID: "call-2", Seq: 3, Kind: "attempt.activity", AggregateType: "attempt", WorkerRef: "worker-parallel",
		Payload: []byte(`{"kind":"tool_call","tool_call":{"name":"exec_command","call_id":"c-test","preview":"python3 test.py","arguments":{"script":"test.py"}}}`),
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(call1)); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(call2)); err != nil {
		t.Fatal(err)
	}

	if len(transport.sent) != 3 {
		t.Fatalf("calls sent=%d want 3", len(transport.sent))
	}
	if transport.sent[1].Text != "exec_command open https://example.com" {
		t.Fatalf("call1 sent=%q", transport.sent[1].Text)
	}
	if transport.sent[2].Text != "exec_command python3 test.py" {
		t.Fatalf("call2 sent=%q", transport.sent[2].Text)
	}

	res2 := core.Event{
		ID: "res-2", Seq: 4, Kind: "attempt.activity", AggregateType: "attempt", WorkerRef: "worker-parallel",
		Payload: []byte(`{"kind":"tool_result","tool_result":{"name":"exec_command","call_id":"c-test","status":"completed","output":""}}`),
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(res2)); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 3 {
		t.Fatalf("successful empty result was sent: %#v", transport.sent)
	}

	res1 := core.Event{
		ID: "res-1", Seq: 5, Kind: "attempt.activity", AggregateType: "attempt", WorkerRef: "worker-parallel",
		Payload: []byte(`{"kind":"tool_result","tool_result":{"name":"exec_command","call_id":"c-open","status":"completed","output":"secret stdout that must not leak"}}`),
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(res1)); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 3 {
		t.Fatalf("successful output result was sent: %#v", transport.sent)
	}

	completed := core.Event{
		ID: "completed", Seq: 6, Kind: "result.accepted", AggregateType: "result", WorkerRef: "worker-parallel",
		Payload: []byte(`{"status":"succeeded"}`),
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(completed)); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 4 || transport.sent[3].Text != "Worker завершён" {
		t.Fatalf("event after suppressed results was not delivered: %#v", transport.sent)
	}

	restartedAdapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []core.Event{spawnEvent, call1, call2, res2, res1, completed} {
		if err := restartedAdapter.HandleDurableEvent(ctx, telegramEvent(ev)); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 4 {
		t.Fatalf("replay changed visible messages: sent=%d want 4", len(transport.sent))
	}
}

func TestTelegramEventBridgeDropsToolEventsWithoutRealIdentity(t *testing.T) {
	for _, kind := range []string{"tool_call", "tool_result"} {
		mapped := telegramEvent(core.Event{Kind: "attempt.activity", Payload: []byte(`{"kind":"` + kind + `","title":"Reading","status":"completed"}`)})
		if mapped.Kind != "" || mapped.Tool != "" {
			t.Fatalf("title-only event became a tool: %#v", mapped)
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
		if sent.ThreadID == 0 && sent.Text == "done" {
			generalMirrors++
		}
	}
	if topicCompletions != 1 || generalMirrors != 1 {
		t.Fatalf("terminal notifications topic=%d general=%d messages=%#v", topicCompletions, generalMirrors, transport.sent)
	}
}

func TestTelegramEventBridgeSkipsTextlessOutcomeSoResultDeliversSummary(t *testing.T) {
	empty := telegramEvent(core.Event{
		ID: "outcome-empty", Seq: 30, Kind: "attempt.outcome_recorded", AggregateType: "attempt", AggregateID: "attempt-9", WorkerRef: "worker-1", CorrelationID: "turn-9",
		Payload: []byte(`{"status":"succeeded","classification":"final","attempt_id":"attempt-9"}`),
	})
	if empty.Kind != "" || !empty.WorkerAttemptTerminal || empty.AttemptID != "attempt-9" {
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
	if adapter.LastEventSeq() != 2 {
		t.Fatalf("bridge cursor=%d, want terminal event seq 2", adapter.LastEventSeq())
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
	if retryable.Kind != "" || !retryable.WorkerAttemptTerminal {
		t.Fatalf("retryable outcome was not kept terminal and quiet: %#v", retryable)
	}
}

func TestTelegramBridgeDeliversCanonicalAddressedRepliesForEveryTurn(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	var seq int64
	handle := func(kind, payload string) {
		t.Helper()
		seq++
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(core.Event{Seq: seq, Kind: kind, Source: "server", Payload: []byte(payload)})); err != nil {
			t.Fatal(err)
		}
	}
	for _, reply := range []string{"first reply", "second reply"} {
		handle("conversation.entry", `{"kind":"user","body":"private user text"}`)
		handle(core.SecretaryTurnStartedEvent, `{"turn_id":"t"}`)
		handle(core.SecretaryTextDeltaEvent, `{"turn_id":"t","text":"`+reply+`"}`)
		handle("conversation.entry", `{"kind":"secretary","body":"`+reply+`"}`)
		handle("conversation.entry", `{"kind":"worker_result","body":"worker summary"}`)
		handle(core.SecretaryTurnFinishedEvent, `{"status":"succeeded","completion":{"branch":"addressed_reply_only","reply_count":1,"assistant_chunks":0}}`)
		if err := adapter.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 2 || transport.sent[0].Text != "first reply" || transport.sent[1].Text != "second reply" {
		t.Fatalf("sent=%#v, want exactly the two Secretary replies", transport.sent)
	}
}

func TestTelegramBridgeShowsFailedAndEmptySecretaryTurnsWithoutRetryOrRawOutput(t *testing.T) {
	for _, terminal := range []string{`{"status":"succeeded"}`, `{"status":"failed","error":"native runtime failed"}`} {
		t.Run(terminal, func(t *testing.T) {
			transport := &bridgeTransport{}
			adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
			if err != nil {
				t.Fatal(err)
			}
			for index, event := range []core.Event{
				{Kind: core.SecretaryTurnStartedEvent, Payload: []byte(`{"turn_id":"t"}`)},
				{Kind: core.SecretaryTextDeltaEvent, Payload: []byte(`{"text":"uncanonical native output"}`)},
				{Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(terminal)},
			} {
				event.Seq = int64(index + 1)
				if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
					t.Fatal(err)
				}
				if index == 2 {
					if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := adapter.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(transport.sent) != 1 || strings.Contains(transport.sent[0].Text, "uncanonical") || !strings.Contains(transport.sent[0].Text, "Secretary") {
				t.Fatalf("visible failure missing or duplicated: %#v", transport.sent)
			}
		})
	}
}

func TestTelegramBridgeKeepsDistinctSameTextRepliesAcrossTurnsAndRestart(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, turn := range []string{"turn-1", "turn-2"} {
		for _, event := range []core.EventInput{
			{Kind: core.SecretaryTurnStartedEvent, Payload: map[string]string{"turn_id": turn}},
			{Kind: core.SecretaryTextDeltaEvent, Payload: map[string]string{"text": "Готово"}},
			{Kind: "conversation.entry", Payload: map[string]string{"kind": "secretary", "turn_id": turn, "body": "Готово"}},
			{Kind: core.SecretaryTurnFinishedEvent, Payload: map[string]string{"status": "succeeded", "turn_id": turn}},
		} {
			if _, err := store.RecordEventWithMetadata(ctx, event); err != nil {
				t.Fatal(err)
			}
		}
	}
	transport := &bridgeTransport{}
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	for index := 0; index < 2; index++ {
		adapter, err := telegram.New(telegram.Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
			t.Fatal(err)
		}
		if err := adapter.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 2 || transport.sent[0].Text != "Готово" || transport.sent[1].Text != "Готово" || transport.sent[0].Identity == transport.sent[1].Identity {
		t.Fatalf("canonical replies conflated or replayed: %#v", transport.sent)
	}
}

func TestTelegramBridgeShowsQueuedWorkerMessageOnlyInItsTopic(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{
		{Seq: 1, ID: "created", Kind: "worker.spawned", WorkerRef: "worker-1", Payload: []byte(`{"title":"Проверка"}`)},
		{Seq: 2, ID: "queued", Kind: "worker.message.queued", WorkerRef: "worker-1", Payload: []byte(`{"id":"message-1","text":"Проверь тесты","state":"pending"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 1 || transport.sent[0].ThreadID != 1 || !strings.Contains(transport.sent[0].Text, "В очереди") || !strings.Contains(transport.sent[0].Text, "Проверь тесты") {
		t.Fatalf("queue acknowledgement missing or mirrored to General: %#v", transport.sent)
	}
}

func TestTelegramBridgeKeepsCanonicalReplyAndTerminalFailureReadable(t *testing.T) {
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []core.Event{
		{Seq: 1, Kind: core.SecretaryTurnStartedEvent},
		{Seq: 2, Kind: "conversation.entry", Payload: []byte(`{"kind":"secretary","body":"Сохранённый ответ"}`)},
		{Seq: 3, Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(`{"status":"failed","error":"runtime failed"}`)},
	} {
		if err := adapter.HandleDurableEvent(context.Background(), telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Сохранённый ответ\n\nSecretary: ошибка — runtime failed" {
		t.Fatalf("reply/error hidden, repeated or concatenated: %#v", transport.sent)
	}
}

func TestTelegramBridgeDeliversPrePromptFailureExactlyOnce(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test", Harness: "fx", Model: "m", Reasoning: "high", ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy"}); err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Telegram-origin input")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishSecretaryTurn(ctx, turn.ID, core.SecretaryTurnFailed, "canonical Secretary context unavailable"); err != nil {
		t.Fatal(err)
	}
	transport := &bridgeTransport{}
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	config := telegram.Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Hour}
	adapter, err := telegram.New(config, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Secretary: ошибка — canonical Secretary context unavailable" {
		t.Fatalf("pre-Prompt failure hidden: %#v", transport.sent)
	}
	restarted, err := telegram.New(config, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridgeTelegramEventsOnce(ctx, store, restarted); err != nil {
		t.Fatal(err)
	}
	if err := restarted.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatal("pre-Prompt failure repeated after adapter restart")
	}
}

func TestTelegramAcknowledgementIsImmediateAndReplaySafeBeforeTerminal(t *testing.T) {
	ctx := context.Background()
	transport := &bridgeTransport{}
	path := filepath.Join(t.TempDir(), "telegram.json")
	cfg := telegram.Config{StatePath: path, OwnerChatID: 100, FlushInterval: time.Hour}
	adapter, err := telegram.New(cfg, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	started := core.Event{Seq: 1, Kind: core.SecretaryTurnStartedEvent}
	ack := core.Event{ID: "ack-event", Seq: 2, Kind: "conversation.entry", Payload: []byte(`{"kind":"secretary","body":"Сейчас проверю","acknowledgement":true}`)}
	for _, event := range []core.Event{started, ack} {
		if err := adapter.HandleDurableEvent(ctx, telegramEvent(event)); err != nil {
			t.Fatal(err)
		}
	}
	if len(transport.sent) != 1 || transport.sent[0].Text != "Сейчас проверю" {
		t.Fatalf("ack buffered until terminal: %#v", transport.sent)
	}
	adapter, err = telegram.New(cfg, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(ack)); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatal("ack replay duplicated delivery")
	}
	finish := core.Event{Seq: 3, Kind: core.SecretaryTurnFinishedEvent, Payload: []byte(`{"status":"succeeded","acknowledged":true}`)}
	if err := adapter.HandleDurableEvent(ctx, telegramEvent(finish)); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("terminal repeated ack: %#v", transport.sent)
	}
}

func TestTelegramBridgeFullACPPayloadOutputPresenceAndFailureSafety(t *testing.T) {
	ctx := context.Background()
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, telegram.Event{Kind: "worker.created", WorkerRef: "w-1"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		payloadJSON string
		wantKind    string
		wantOutput  bool
		wantVisible bool
	}{
		{
			name:        "empty raw output string",
			payloadJSON: `{"kind":"tool_result","tool_result":{"name":"shell","status":"completed","output":""}}`,
			wantKind:    "worker.tool_finished",
			wantOutput:  false,
		},
		{
			name:        "json-encoded empty string quotes",
			payloadJSON: `{"kind":"tool_result","tool_result":{"name":"shell","status":"completed","output":"\"\""}}`,
			wantKind:    "worker.tool_finished",
			wantOutput:  false,
		},
		{
			name:        "acp object with empty formatted_output",
			payloadJSON: `{"kind":"tool_result","tool_result":{"name":"shell","status":"completed","output":"{\"formatted_output\":\"\"}"}}`,
			wantKind:    "worker.tool_finished",
			wantOutput:  false,
		},
		{
			name:        "nonempty acp output",
			payloadJSON: `{"kind":"tool_result","tool_result":{"name":"shell","status":"completed","output":"{\"formatted_output\":\"secret token do-not-leak\"}"}}`,
			wantKind:    "worker.tool_finished",
			wantOutput:  true,
		},
		{
			name:        "failure with empty output",
			payloadJSON: `{"kind":"tool_result","tool_result":{"name":"shell","status":"failed","output":""}}`,
			wantKind:    "worker.tool_failed",
			wantOutput:  false,
			wantVisible: true,
		},
		{
			name:        "failure with error text",
			payloadJSON: `{"kind":"tool_result","tool_result":{"name":"shell","status":"failed","error":"critical failure do-not-leak"}}`,
			wantKind:    "worker.tool_failed",
			wantOutput:  true,
			wantVisible: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport.sent = nil
			event := core.Event{
				ID: "evt-" + tc.name, Seq: 100, Kind: "attempt.activity", WorkerRef: "w-1", AttemptID: "att-42",
				Payload: []byte(tc.payloadJSON),
			}
			mapped := telegramEvent(event)
			if mapped.Kind != tc.wantKind {
				t.Fatalf("mapped kind=%q want %q", mapped.Kind, tc.wantKind)
			}
			if mapped.AttemptID != "att-42" {
				t.Fatalf("attempt_id=%q want att-42", mapped.AttemptID)
			}
			if mapped.HasOutput != tc.wantOutput {
				t.Fatalf("HasOutput=%v want %v", mapped.HasOutput, tc.wantOutput)
			}
			if err := adapter.HandleEvent(ctx, mapped); err != nil {
				t.Fatal(err)
			}
			if tc.wantVisible && len(transport.sent) != 1 || !tc.wantVisible && len(transport.sent) != 0 {
				t.Fatalf("sent count=%d want visible=%v: %#v", len(transport.sent), tc.wantVisible, transport.sent)
			}
			if !tc.wantVisible {
				return
			}
			msg := transport.sent[0].Text
			if !strings.HasPrefix(msg, "✗ Ошибка: shell") {
				t.Fatalf("message %q missing safe failure", msg)
			}
			if strings.Contains(msg, "do-not-leak") || strings.Contains(msg, "secret") || strings.Contains(msg, "critical") {
				t.Fatalf("raw output leaked into topic: %q", msg)
			}
		})
	}
}

func TestTelegramBridgeEndToEndCoreToolResultPayload(t *testing.T) {
	ctx := context.Background()
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, telegram.Event{Kind: "worker.created", WorkerRef: "worker-e2e"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name          string
		outputPresent bool
	}{
		{"failed with output present", true},
		{"failed with empty output", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport.sent = nil
			activity := core.Activity{
				Metadata: core.ActivityMetadata{
					EventID: "evt-fail", Node: "local-node", HarnessInstanceID: "local/opencode",
					WorkerRef: "worker-e2e", AttemptID: "att-e2e", Sequence: 5, ObservedAt: time.Now().UTC(),
				},
				Kind: core.ActivityToolResult,
				ToolResult: &core.ToolResult{
					CallID:        "call-fail-" + tc.name,
					Name:          "exec_command",
					Output:        "",
					OutputPresent: tc.outputPresent,
					Status:        "failed",
					Preview:       "make test",
				},
			}
			encoded, err := json.Marshal(activity)
			if err != nil {
				t.Fatal(err)
			}
			event := core.Event{
				ID: "evt-5", Seq: 5, Kind: "attempt.activity",
				WorkerRef: "worker-e2e", AttemptID: "att-e2e",
				Payload: encoded,
			}
			mapped := telegramEvent(event)
			if mapped.Kind != "worker.tool_failed" {
				t.Fatalf("mapped kind=%q want worker.tool_failed", mapped.Kind)
			}
			if mapped.CallID != "call-fail-"+tc.name {
				t.Fatalf("mapped CallID=%q want call-fail-1", mapped.CallID)
			}
			if mapped.AttemptID != "att-e2e" {
				t.Fatalf("mapped AttemptID=%q want att-e2e", mapped.AttemptID)
			}
			if mapped.HasOutput != tc.outputPresent {
				t.Fatalf("mapped HasOutput=%v want %v", mapped.HasOutput, tc.outputPresent)
			}
			if err := adapter.HandleEvent(ctx, mapped); err != nil {
				t.Fatal(err)
			}
			if len(transport.sent) != 1 {
				t.Fatalf("sent count=%d want 1: %#v", len(transport.sent), transport.sent)
			}
			msg := transport.sent[0].Text
			if msg != "✗ Ошибка: exec_command make test" {
				t.Fatalf("rendered message %q missing failure signature", msg)
			}
		})
	}
}

func TestNormalizeRuntimeActivityThroughTelegramEventSerialization(t *testing.T) {
	metadata := core.ActivityMetadata{
		EventID: "evt-norm", Node: "local", HarnessInstanceID: "local/fx",
		WorkerRef: "w-norm", AttemptID: "att-norm", Sequence: 1, ObservedAt: time.Now().UTC(),
	}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityToolResult}}

	cases := []struct {
		name        string
		result      string
		outputPres  bool
		status      string
		wantPresent bool
		wantVisible bool
		secret      string
	}{
		{name: "raw empty", result: "", outputPres: false, status: "completed", wantPresent: false},
		{name: "json-encoded empty", result: `""`, outputPres: false, status: "completed", wantPresent: false},
		{name: "acp empty formatted_output", result: `{"formatted_output":""}`, outputPres: false, status: "completed", wantPresent: false},
		{name: "genuinely empty json object", result: `{}`, outputPres: false, status: "completed", wantPresent: false},
		{name: "nonempty decoded text", result: "make test pass do-not-leak", outputPres: false, status: "completed", wantPresent: true, secret: "do-not-leak"},
		{name: "stderr-only nonempty semantic output with empty stdout", result: `""`, outputPres: true, status: "completed", wantPresent: true, secret: "sensitive-warning-do-not-leak"},
		{name: "nonempty json payload stripped to empty sanitized content", result: `{}`, outputPres: true, status: "completed", wantPresent: true, secret: "secret-token-do-not-leak"},
		{name: "failed with output present", result: "", outputPres: true, status: "failed", wantPresent: true, wantVisible: true, secret: "critical-failure-do-not-leak"},
		{name: "failed with empty output", result: "", outputPres: false, status: "failed", wantPresent: false, wantVisible: true},
	}

	ctx := context.Background()
	transport := &bridgeTransport{}
	adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, telegram.Event{Kind: "worker.created", WorkerRef: "w-norm"}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport.sent = nil
			act, ok := node.NormalizeRuntimeActivity(node.Activity{
				Kind:          node.ActivityToolResult,
				Tool:          "shell",
				CallID:        "c-" + tc.name,
				Result:        tc.result,
				OutputPresent: tc.outputPres,
				Status:        tc.status,
			}, metadata, capabilities)
			if !ok {
				t.Fatalf("NormalizeRuntimeActivity failed for %s", tc.name)
			}
			encoded, err := json.Marshal(act)
			if err != nil {
				t.Fatal(err)
			}
			event := core.Event{
				ID: "evt-" + tc.name, Seq: 10, Kind: "attempt.activity",
				WorkerRef: "w-norm", AttemptID: "att-norm",
				Payload: encoded,
			}
			mapped := telegramEvent(event)
			if mapped.HasOutput != tc.wantPresent {
				t.Fatalf("mapped.HasOutput=%v want %v for %s", mapped.HasOutput, tc.wantPresent, tc.name)
			}
			if err := adapter.HandleEvent(ctx, mapped); err != nil {
				t.Fatal(err)
			}
			if tc.wantVisible && len(transport.sent) != 1 || !tc.wantVisible && len(transport.sent) != 0 {
				t.Fatalf("sent count=%d want visible=%v: %#v", len(transport.sent), tc.wantVisible, transport.sent)
			}
			if !tc.wantVisible {
				return
			}
			msg := transport.sent[0].Text
			if !strings.HasPrefix(msg, "✗ Ошибка: shell") {
				t.Fatalf("rendered message %q missing safe failure for %s", msg, tc.name)
			}
			if tc.secret != "" && strings.Contains(msg, tc.secret) {
				t.Fatalf("secret %q leaked into rendered message: %q", tc.secret, msg)
			}
			for _, leak := range []string{"do-not-leak", "secret", "sensitive", "critical"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("leak %q found in rendered message: %q", leak, msg)
				}
			}
		})
	}
}

func TestTelegramLegacyFailureEventReplayOnReopen(t *testing.T) {
	for _, malformedStatus := range []bool{false, true} {
		t.Run(fmt.Sprint(malformedStatus), func(t *testing.T) {
			var legacyStatus any = "failed"
			if malformedStatus {
				legacyStatus = map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private"}
			}
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "legacy-events.db")
			store, err := core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			event, err := store.RecordEvent(ctx, "result.accepted", "worker", "attempt", "", map[string]any{"status": "failed", "summary": "safe"})
			if err != nil {
				t.Fatal(err)
			}
			successText := "Successful Bearer example https://unknown.invalid/output"
			success, err := store.RecordEvent(ctx, "result.accepted", "worker", "success-attempt", "", map[string]any{"status": "succeeded", "summary": successText})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			raw, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]any{"status": legacyStatus, "extra": map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private"}, "text": "Digest username=owner, response=privatevalue https://unknown.invalid/private", "summary": "Digest username=owner, response=privatevalue; Bearer private-token https://unknown.invalid/private " + strings.Repeat("я", 1200), "failure": map[string]any{"category": 123, "message": "Digest username=owner, response=privatevalue https://unknown.invalid/private " + strings.Repeat("я", 1200)}, "failure_category": []any{"Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "diagnostics": map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "error_code": []any{"Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "error_message": map[string]any{"message": "Digest username=owner, response=privatevalue https://unknown.invalid/private "}, "failure_code": []any{"Digest username=owner, response=privatevalue https://unknown.invalid/private "}})
			if _, err := raw.ExecContext(ctx, `UPDATE events SET payload_json = ? WHERE id = ?`, string(payload), event.ID); err != nil {
				t.Fatal(err)
			}
			if err := raw.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			events, err := store.EventsAfterSeq(ctx, event.Seq-1, 500)
			if err != nil || len(events) != 2 {
				t.Fatalf("events=%#v err=%v", events, err)
			}
			delivered := telegramEvent(events[0])
			if delivered.EventID != event.ID || delivered.Sequence != event.Seq || delivered.AttemptID != "attempt" || len([]rune(delivered.Text)) > 1000 {
				t.Fatalf("identity/bound changed: %#v", delivered)
			}
			for _, secret := range []string{"username=owner", "privatevalue", "private-token", "unknown.invalid"} {
				if strings.Contains(delivered.Text, secret) || strings.Contains(string(delivered.Payload), secret) {
					t.Fatalf("Telegram consumer leaked %s", secret)
				}
			}
			transport := &bridgeTransport{}
			adapter, err := telegram.New(telegram.Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100, FlushInterval: time.Hour}, transport, &bridgeServer{})
			if err != nil {
				t.Fatal(err)
			}
			if err := adapter.HandleEvent(ctx, telegram.Event{Kind: "worker.created", WorkerRef: "worker"}); err != nil {
				t.Fatal(err)
			}
			transport.sent = nil
			if err := adapter.HandleDurableEvent(ctx, delivered); err != nil {
				t.Fatal(err)
			}
			if err := adapter.HandleDurableEvent(ctx, delivered); err != nil {
				t.Fatal(err)
			}
			if err := adapter.Flush(ctx); err != nil {
				t.Fatal(err)
			}
			if len(transport.sent) == 0 {
				t.Fatal("failure notification missing")
			}
			for _, message := range transport.sent {
				for _, secret := range []string{"privatevalue", "unknown.invalid", "username=owner"} {
					if strings.Contains(message.Text, secret) {
						t.Fatalf("notification leaked: %s", message.Text)
					}
				}
			}
			if got := telegramEvent(events[1]); got.EventID != success.ID || got.Text != successText {
				t.Fatalf("successful output changed: %#v", got)
			}
		})
	}
}
