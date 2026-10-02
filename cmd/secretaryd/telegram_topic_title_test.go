package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

func TestTelegramTopicTitleBridgeUsesOnlyDispatchedIntent(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prompt := "Найди рецепт мохито без алкоголя.\nСоставь список ингредиентов."
	worker := core.Worker{WorkerRef: "worker-1", Title: "Generic task", Intent: prompt,
		PolicySnapshot: "private-policy", ProjectSnapshot: "private-project", Workspace: "private-workspace",
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "worker.spawned", AggregateType: "worker", AggregateID: worker.WorkerRef, Payload: worker}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "result.accepted", WorkerRef: worker.WorkerRef, CorrelationID: "turn-1", Payload: map[string]string{"summary": "Готово", "status": "succeeded"}}); err != nil {
		t.Fatal(err)
	}
	transport := &bridgeTransport{}
	calls := 0
	path := filepath.Join(t.TempDir(), "telegram.json")
	adapter, err := telegram.New(telegram.Config{StatePath: path, OwnerChatID: 100,
		TitleGenerator: func(_ context.Context, input string) (string, error) {
			calls++
			if input != prompt {
				t.Errorf("model received something other than dispatched task: %q", input)
			}
			return "", errors.New("private provider failure")
		},
	}, transport, &bridgeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || adapter.LastEventSeq() != 2 || len(transport.sent) != 2 {
		t.Fatalf("calls=%d cursor=%d sent=%#v", calls, adapter.LastEventSeq(), transport.sent)
	}
	for _, sent := range transport.sent {
		if strings.Contains(sent.Text, "private") {
			t.Fatalf("private data in delivery: %#v", sent)
		}
	}
	if _, err := bridgeTelegramEventsOnce(ctx, store, adapter); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(transport.sent) != 2 {
		t.Fatal("replay repeated generation or Result")
	}
}

func TestTelegramEventDoesNotInferTaskFromGenericTitleOrResult(t *testing.T) {
	for _, kind := range []string{"worker.spawned", "worker.started", "result.accepted"} {
		payload, _ := json.Marshal(map[string]string{"title": "Generic title", "summary": "Result", "intent": "Dispatched task", "policy_snapshot": "private instructions"})
		event := telegramEvent(core.Event{Kind: kind, Payload: payload})
		want := ""
		if kind == "worker.spawned" {
			want = "Dispatched task"
		}
		if event.TaskPrompt != want {
			t.Fatalf("kind=%s prompt=%q, want %q", kind, event.TaskPrompt, want)
		}
	}
}
