package main

import (
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
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
