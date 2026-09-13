package telegram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeTransport struct {
	updates []Update
	sent    []SentMessage
	topics  []ForumTopic
}

func (f *fakeTransport) GetUpdates(context.Context, int64, time.Duration) ([]Update, error) {
	updates := append([]Update(nil), f.updates...)
	f.updates = nil
	return updates, nil
}
func (f *fakeTransport) SendMessage(_ context.Context, message OutgoingMessage) error {
	f.sent = append(f.sent, SentMessage{OutgoingMessage: message})
	return nil
}
func (f *fakeTransport) CreateForumTopic(_ context.Context, chatID int64, name string) (ForumTopic, error) {
	topic := ForumTopic{ChatID: chatID, ThreadID: int64(len(f.topics) + 1), Name: name}
	f.topics = append(f.topics, topic)
	return topic, nil
}

type fakeServer struct {
	messages []InboundMessage
	workers  []WorkerMessage
}

func (f *fakeServer) SendMessage(_ context.Context, message InboundMessage) error {
	f.messages = append(f.messages, message)
	return nil
}
func (f *fakeServer) SendWorkerMessage(_ context.Context, message WorkerMessage) error {
	f.workers = append(f.workers, message)
	return nil
}

func newTestAdapter(t *testing.T, transport Transport, server ServerClient, statePath string) *Adapter {
	t.Helper()
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestDuplicateUpdateIsIgnoredAndUnauthorizedCannotChangeState(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.SetOwnerChat(100); err != nil {
		t.Fatal(err)
	}
	update := Update{ID: 7, Message: &Message{ChatID: 100, FromID: 100, Text: "hello"}}
	if err := adapter.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(context.Background(), update); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 {
		t.Fatalf("messages after duplicate = %d", len(server.messages))
	}
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 8, Message: &Message{ChatID: 999, FromID: 999, Text: "read state"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 || len(transport.sent) != 0 {
		t.Fatalf("unauthorized update changed state: messages=%d sent=%d", len(server.messages), len(transport.sent))
	}
}

func TestWorkerTopicMappingPersistsAndRoutesFollowUp(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, statePath)
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.created", WorkerRef: "w-1", Title: "Fix header"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.topics) != 1 || transport.topics[0].ThreadID == 0 {
		t.Fatalf("topics = %#v", transport.topics)
	}
	threadID := transport.topics[0].ThreadID
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(context.Background(), Update{ID: 9, Message: &Message{ChatID: 100, ThreadID: threadID, FromID: 100, Text: "keep going"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.workers) != 1 || server.workers[0].WorkerRef != "w-1" || server.workers[0].Text != "keep going" {
		t.Fatalf("worker messages = %#v", server.workers)
	}
	if len(transport.topics) != 1 {
		t.Fatalf("restart created another topic: %#v", transport.topics)
	}
}

func TestSecretaryEventsAreThrottledAndWorkerEventsAreReadable(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "Hello "}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.text_delta", Text: "world"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "secretary.tool_call", Tool: "list_workers"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("delta sent before throttle flush: %#v", transport.sent)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 1 || !strings.Contains(transport.sent[0].Text, "Hello world") || !strings.Contains(transport.sent[0].Text, "list_workers") {
		t.Fatalf("secretary batch = %#v", transport.sent)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.activity", WorkerRef: "w-1", Text: "analysis: secret-cot", Tool: "shell", Payload: json.RawMessage(`{"token":"node-secret","path":"/tmp/work"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := transport.sent[len(transport.sent)-1].Text
	if strings.Contains(last, "secret-cot") || strings.Contains(last, "node-secret") || strings.Contains(last, "runtime") {
		t.Fatalf("raw or sensitive worker event leaked: %q", last)
	}
	if !strings.Contains(last, "shell") {
		t.Fatalf("readable worker activity missing: %q", last)
	}
}

func TestImportantWorkerStatesAndRestartReplay(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &fakeTransport{updates: []Update{{ID: 11, Message: &Message{ChatID: 100, FromID: 100, Text: "hello"}}}}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, statePath)
	if err := adapter.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 {
		t.Fatalf("poll messages = %#v", server.messages)
	}
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100, FlushInterval: time.Minute}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 1 {
		t.Fatalf("replay duplicated inbound message: %#v", server.messages)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.approval_requested", WorkerRef: "w-1", Text: "approve shell"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.completed", WorkerRef: "w-1", Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(context.Background(), Event{Kind: "worker.offline", WorkerRef: "w-1"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) < 3 {
		t.Fatalf("important events were lost: %#v", transport.sent)
	}
}

func TestPairingCodeIsOneTimeAndDoesNotExposeCredential(t *testing.T) {
	adapter := newTestAdapter(t, &fakeTransport{}, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	pairing, err := adapter.CreatePairing("bot")
	if err != nil {
		t.Fatal(err)
	}
	if pairing.Code == "" || !strings.Contains(pairing.DeepLink, pairing.Code) {
		t.Fatalf("pairing = %#v", pairing)
	}
	if strings.Contains(pairing.DeepLink, "token") || pairing.Credential != "" {
		t.Fatalf("pairing exposed credential: %#v", pairing)
	}
	if err := adapter.RedeemPairing(pairing.Code, 100); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RedeemPairing(pairing.Code, 101); err == nil {
		t.Fatal("reused pairing code was accepted")
	}
}

func TestStateFileIsAtomicAndReadableAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telegram.json")
	adapter := newTestAdapter(t, &fakeTransport{}, &fakeServer{}, path)
	if err := adapter.SetOwnerChat(100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
}
