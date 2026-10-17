package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newT04Adapter(t *testing.T, path string, transport Transport, server ServerClient, group int64) *Adapter {
	t.Helper()
	a, err := New(Config{
		StatePath: path, OwnerChatID: 100, GroupChatID: &group,
		FlushInterval: time.Minute,
	}, transport, server)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestT04DMOnlyDoesNotRequireForumTopics(t *testing.T) {
	ctx := context.Background()
	transport, server := &fakeTransport{}, &fakeServer{}
	adapter := newT04Adapter(t, filepath.Join(t.TempDir(), "state.json"), transport, server, 0)
	if err := adapter.HandleUpdate(ctx, Update{ID: 10, Message: &Message{ChatID: 100, FromID: 100, ChatType: "private", Text: "hello"}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.created", WorkerRef: "w1", TaskPrompt: "research"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.completed", WorkerRef: "w1", Text: "result", TerminalIdentity: "turn-1"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "message.saved", EventID: "web-entry", Source: "web", Text: "from web"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.topics) != 0 {
		t.Fatalf("DM-only must not create topics: %#v", transport.topics)
	}
	if len(server.messages) != 1 || server.messages[0].ExternalMessageID != "10" {
		t.Fatalf("canonical DM input=%#v", server.messages)
	}
	if len(transport.sent) != 2 {
		t.Fatalf("expected Result and Web entry in DM: %#v", transport.sent)
	}
	for _, sent := range transport.sent {
		if sent.ChatID != 100 || sent.ThreadID != 0 {
			t.Fatalf("non-DM delivery in DM-only mode: %#v", sent)
		}
	}
}

func TestT04LinkedForumGeneralAndWorkerTopics(t *testing.T) {
	ctx := context.Background()
	transport, server := &fakeTransport{}, &fakeServer{}
	// Forum General is thread 1; test-created Worker Topics start at 2.
	transport.topics = []ForumTopic{{ChatID: -200, ThreadID: 1, Name: "General"}}
	path := filepath.Join(t.TempDir(), "state.json")
	adapter := newT04Adapter(t, path, transport, server, 0)
	group := &Message{ChatID: -200, ChatType: "supergroup", IsForum: true, ThreadID: 1, FromID: 300, Text: "/linkgroup"}
	if err := adapter.HandleUpdate(ctx, Update{ID: 1, Message: group}); err != nil {
		t.Fatal(err)
	}
	if adapter.state.GroupChat != 0 {
		t.Fatal("another member linked the group")
	}
	ownerGroup := *group
	ownerGroup.FromID = 100
	if err := adapter.HandleUpdate(ctx, Update{ID: 2, Message: &ownerGroup}); err != nil {
		t.Fatal(err)
	}
	if adapter.state.GroupChat != -200 {
		t.Fatalf("linked group=%d", adapter.state.GroupChat)
	}
	if err := adapter.HandleUpdate(ctx, Update{ID: 3, Message: &Message{ChatID: 100, FromID: 100, Text: "DM input"}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(ctx, Update{ID: 4, Message: &Message{ChatID: -200, FromID: 100, ThreadID: 1, Text: "General input"}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleUpdate(ctx, Update{ID: 5, Message: &Message{ChatID: -200, FromID: 300, ThreadID: 1, Text: "forbidden"}}); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 2 || server.messages[0].Body != "DM input" || server.messages[1].Body != "General input" {
		t.Fatalf("one canonical input per owner update: %#v", server.messages)
	}
	if len(transport.sent) != 2 || transport.sent[0].ChatID != -200 || transport.sent[1].ChatID != 100 {
		t.Fatalf("bidirectional General mirrors: %#v", transport.sent)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "worker.created", WorkerRef: "w1", TaskPrompt: "research"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SyncWorkerTopics(ctx); err != nil {
		t.Fatal(err)
	}
	mapping := adapter.state.Topics["w1"]
	if mapping.ChatID != -200 || mapping.ThreadID != 2 {
		t.Fatalf("Worker Topic mapping=%#v", mapping)
	}
	for _, update := range []Update{
		{ID: 6, Message: &Message{ChatID: -200, FromID: 300, ThreadID: 2, Text: "not authorized"}},
		{ID: 7, Message: &Message{ChatID: -200, FromID: 100, ThreadID: 2, Text: "worker steer"}},
	} {
		if err := adapter.HandleUpdate(ctx, update); err != nil {
			t.Fatal(err)
		}
	}
	if len(server.workers) != 1 || server.workers[0].WorkerRef != "w1" || server.workers[0].Text != "worker steer" {
		t.Fatalf("Worker Topic routing=%#v", server.workers)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "message.saved", EventID: "web-42", Source: "web", Text: "Web input"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "secretary.turn.started"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "secretary.text_delta", Text: "Secretary reply"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, Event{Kind: "secretary.turn.finished"}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	result := Event{Kind: "worker.completed", WorkerRef: "w1", Text: "Worker result", TerminalIdentity: "turn-43"}
	if err := adapter.HandleEvent(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SyncWorkerTopics(ctx); err != nil {
		t.Fatal(err)
	}
	count := func(body string, chatID, threadID int64) int {
		n := 0
		for _, item := range transport.sent {
			if item.ChatID == chatID && item.ThreadID == threadID && item.Text == body {
				n++
			}
		}
		return n
	}
	for _, body := range []string{"Web input", "Secretary reply", "Worker result"} {
		if count(body, 100, 0) != 1 || count(body, -200, 0) != 1 {
			t.Fatalf("%q not projected once to both chats: %#v", body, transport.sent)
		}
	}
	if count("Worker result", -200, 2) != 1 {
		t.Fatalf("Worker Topic lost Result: %#v", transport.sent)
	}
	before := len(transport.sent)
	adapter = newT04Adapter(t, path, transport, server, 0)
	if err := adapter.HandleUpdate(ctx, Update{ID: 3, Message: &Message{ChatID: 100, FromID: 100, Text: "DM input"}}); err != nil {
		t.Fatal(err)
	}
	for _, replay := range []Event{{Kind: "message.saved", EventID: "web-42", Source: "web", Text: "Web input"}, result} {
		if err := adapter.HandleEvent(ctx, replay); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.SyncWorkerTopics(ctx); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(server.messages) != 2 || len(transport.sent) != before {
		t.Fatalf("restart/replay duplicated input or sends: inputs=%#v sends=%#v", server.messages, transport.sent)
	}
}

type t04FlakyTransport struct {
	fakeTransport
	calls  int
	failAt int
}

func (t *t04FlakyTransport) SendMessage(ctx context.Context, msg OutgoingMessage) error {
	t.calls++
	if t.calls == t.failAt {
		return errors.New("simulated Telegram outage")
	}
	return t.fakeTransport.SendMessage(ctx, msg)
}

func TestT04PartialOutboundRetriesOnlyUndeliveredChat(t *testing.T) {
	ctx := context.Background()
	transport := &t04FlakyTransport{failAt: 2}
	path := filepath.Join(t.TempDir(), "state.json")
	server := &fakeServer{}
	adapter := newT04Adapter(t, path, transport, server, -200)
	event := Event{Kind: "message.saved", EventID: "web-1", Source: "client:web", Text: "hello"}
	if err := adapter.HandleEvent(ctx, event); err == nil {
		t.Fatal("expected failure delivering to group")
	}
	if len(transport.sent) != 1 || transport.sent[0].ChatID != 100 {
		t.Fatalf("DM delivery should persist before failed group send: %#v", transport.sent)
	}
	transport.failAt = 0
	adapter = newT04Adapter(t, path, transport, server, -200)
	if err := adapter.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 2 || transport.sent[1].ChatID != -200 {
		t.Fatalf("retry duplicated delivered DM or missed group: %#v", transport.sent)
	}
}
