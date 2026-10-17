package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
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
	record(core.EventInput{Kind: "message.saved", Source: "web", Payload: map[string]any{"body": "Web message"}})
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

func TestT04DeploymentAllowsDMWithoutGroupAndValidatesForumID(t *testing.T) {
	t.Setenv("SECRETARY_TELEGRAM_ENABLED", "true")
	t.Setenv("SECRETARY_TELEGRAM_BOT_TOKEN", "fixture")
	t.Setenv("SECRETARY_TELEGRAM_SERVER_CREDENTIAL", "fixture")
	t.Setenv("SECRETARY_TELEGRAM_OWNER_CHAT_ID", "100")
	t.Setenv("SECRETARY_TELEGRAM_GROUP_CHAT_ID", "")
	config, err := configuredTelegramDeployment()
	if err != nil || config.OwnerChatID != 100 || config.GroupChatID != 0 {
		t.Fatalf("DM-only deployment=%#v err=%v", config, err)
	}
	t.Setenv("SECRETARY_TELEGRAM_GROUP_CHAT_ID", "-200")
	config, err = configuredTelegramDeployment()
	if err != nil || config.GroupChatID != -200 {
		t.Fatalf("forum deployment=%#v err=%v", config, err)
	}
	t.Setenv("SECRETARY_TELEGRAM_GROUP_CHAT_ID", "200")
	if _, err := configuredTelegramDeployment(); err == nil {
		t.Fatal("positive group id accepted")
	}
}

type t04WorkerMessages struct {
	mu sync.Mutex
	received []telegram.WorkerMessage
}
func (*t04WorkerMessages) SendMessage(context.Context, telegram.InboundMessage) error { return nil }
func (s *t04WorkerMessages) SendWorkerMessage(_ context.Context, m telegram.WorkerMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, m)
	return nil
}
func (*t04WorkerMessages) ResumeWorker(context.Context, string) error { return nil }

type t04ForumTransport struct {
	bridgeTransport
	failTopics bool
	topicCalls int
}
func (t *t04ForumTransport) CreateForumTopic(_ context.Context, chatID int64, name string) (telegram.ForumTopic, error) {
	t.topicCalls++
	if t.failTopics {
		return telegram.ForumTopic{}, fmt.Errorf("forum permission temporarily unavailable")
	}
	return telegram.ForumTopic{ChatID: chatID, ThreadID: int64(9 + t.topicCalls), Name: name}, nil
}

func TestT04LinkGroupRestoresPreexistingWorkersFromServerAndRoutesTopics(t *testing.T) {
	ctx, store, _, details := newTelegramBridgeWorker(t)
	_, conversation, err := store.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workerRef := details.Worker.WorkerRef
	listWorkers := func(ctx context.Context) ([]telegram.WorkerTopic, error) {
		workers, err := store.WorkersForConversation(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		topics := make([]telegram.WorkerTopic, 0, len(workers))
		for _, worker := range workers {
			if !worker.Archived {
				topics = append(topics, telegram.WorkerTopic{WorkerRef: worker.WorkerRef, Title: worker.Title, Intent: worker.Intent})
			}
		}
		return topics, nil
	}
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	group := int64(0)
	transport := &t04ForumTransport{}
	server := &t04WorkerMessages{}
	makeAdapter := func() *telegram.Adapter {
		t.Helper()
		a, err := telegram.New(telegram.Config{
			StatePath: statePath, OwnerChatID: 100, GroupChatID: &group,
			ExistingWorkers: listWorkers,
		}, transport, server)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	adapter := makeAdapter()
	// Processing the Worker event while DM-only must not create a Topic.
	if err := adapter.HandleEvent(ctx, telegram.Event{Kind: "worker.created", WorkerRef: workerRef, Title: details.Worker.Title, TaskPrompt: details.Worker.Intent}); err != nil {
		t.Fatal(err)
	}
	if transport.topicCalls != 0 {
		t.Fatal("created topic before a group was linked")
	}
	link := telegram.Update{ID: 1001, Message: &telegram.Message{
		ChatID: -200, ChatType: "supergroup", IsForum: true,
		FromID: 100, ThreadID: 1, Text: "/linkgroup",
	}}
	if err := adapter.HandleUpdate(ctx, link); err != nil {
		t.Fatal(err)
	}
	if transport.topicCalls != 1 {
		t.Fatalf("existing Worker Topic not restored at link: calls=%d", transport.topicCalls)
	}
	if err := adapter.HandleUpdate(ctx, telegram.Update{ID: 1002, Message: &telegram.Message{
		ChatID: -200, FromID: 100, ThreadID: 10, Text: "direct Worker instruction",
	}}); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.received) != 1 || server.received[0].WorkerRef != workerRef {
		t.Fatalf("restored Worker Topic did not route correctly: %#v", server.received)
	}
	adapter = makeAdapter()
	if err := adapter.SyncWorkerTopics(ctx); err != nil {
		t.Fatal(err)
	}
	if transport.topicCalls != 1 {
		t.Fatalf("restart duplicated a durable topic mapping: calls=%d", transport.topicCalls)
	}
}

func TestT04TopicRecoveryRetriesAfterFailedLinkAndRestart(t *testing.T) {
	ctx, store, _, details := newTelegramBridgeWorker(t)
	_, conversation, err := store.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(ctx context.Context) ([]telegram.WorkerTopic, error) {
		workers, err := store.WorkersForConversation(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		out := make([]telegram.WorkerTopic, 0, len(workers))
		for _, worker := range workers {
			out = append(out, telegram.WorkerTopic{WorkerRef: worker.WorkerRef, Title: worker.Title, Intent: worker.Intent})
		}
		return out, nil
	}
	path := filepath.Join(t.TempDir(), "state.json")
	group := int64(0)
	transport := &t04ForumTransport{failTopics: true}
	newAdapter := func() *telegram.Adapter {
		t.Helper()
		a, err := telegram.New(telegram.Config{StatePath: path, OwnerChatID: 100, GroupChatID: &group, ExistingWorkers: lookup}, transport, &bridgeServer{})
		if err != nil { t.Fatal(err) }
		return a
	}
	a := newAdapter()
	err = a.HandleUpdate(ctx, telegram.Update{ID: 99, Message: &telegram.Message{ChatID: -200, ChatType: "supergroup", IsForum: true, FromID: 100, Text: "/linkgroup"}})
	if err == nil {
		t.Fatal("expected transient createForumTopic failure")
	}
	transport.failTopics = false
	a = newAdapter()
	if err := a.SyncWorkerTopics(ctx); err != nil {
		t.Fatal(err)
	}
	if transport.topicCalls != 2 || details.Worker.WorkerRef == "" {
		t.Fatalf("reconnect did not restore existing Worker: calls=%d ref=%q", transport.topicCalls, details.Worker.WorkerRef)
	}
	if err := a.SyncWorkerTopics(ctx); err != nil {
		t.Fatal(err)
	}
	if transport.topicCalls != 2 {
		t.Fatal("sync is not idempotent")
	}
}
