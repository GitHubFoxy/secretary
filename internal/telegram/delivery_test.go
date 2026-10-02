package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// Enforce the Bot API constraints that the old permissive fake missed.
type constrainedTransport struct {
	*fakeTransport
	calls    int
	failCall int
}

func (t *constrainedTransport) CreateForumTopic(ctx context.Context, chatID int64, name string) (ForumTopic, error) {
	if !utf8.ValidString(name) {
		return ForumTopic{}, errors.New("strings must be encoded in UTF-8")
	}
	return t.fakeTransport.CreateForumTopic(ctx, chatID, name)
}

func (t *constrainedTransport) SendMessage(ctx context.Context, message OutgoingMessage) error {
	if !utf8.ValidString(message.Text) || len(utf16.Encode([]rune(message.Text))) > 4096 {
		return errors.New("invalid UTF-8 or message is too long")
	}
	t.calls++
	if t.calls == t.failCall {
		return errors.New("temporary send failure")
	}
	return t.fakeTransport.SendMessage(ctx, message)
}

func TestWorkerTopicKeepsUnicodeAndAdvancesDurableCursor(t *testing.T) {
	transport := &constrainedTransport{fakeTransport: &fakeTransport{}}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))
	title := strings.Repeat("я", 29) + "🍹" + strings.Repeat("р", 40)
	err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 1, Kind: "worker.created", WorkerRef: "worker-1", Title: title})
	if err != nil {
		t.Fatalf("Worker Topic blocks the durable event bridge: %v", err)
	}
	if adapter.LastEventSeq() != 1 || len(transport.topics) != 1 {
		t.Fatalf("cursor=%d topics=%d", adapter.LastEventSeq(), len(transport.topics))
	}
	if got := transport.topics[0].Name; got != string([]rune(title)[:60]) {
		t.Fatalf("title=%q, want first 60 Unicode characters", got)
	}
}

func TestLongWorkerResultResumesBothDestinationsAfterRestart(t *testing.T) {
	for name, failCall := range map[string]int{"topic": 2, "general": 3} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			statePath := filepath.Join(t.TempDir(), "telegram.json")
			transport := &constrainedTransport{fakeTransport: &fakeTransport{}, failCall: failCall}
			adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
			text := strings.Repeat("Рецепт 🍹 ", 600)
			event := Event{Sequence: 1, Kind: "worker.completed", WorkerRef: "worker-1", Text: text, TerminalIdentity: "turn-1"}
			if err := adapter.HandleDurableEvent(ctx, event); err == nil {
				t.Fatal("expected temporary transport failure")
			}
			if len(transport.sent) != failCall-1 {
				t.Fatalf("delivered chunks=%d, want %d before failure", len(transport.sent), failCall-1)
			}
			adapter = newTestAdapter(t, transport, &fakeServer{}, statePath)
			if err := adapter.HandleDurableEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			if err := adapter.HandleDurableEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			var topic, general strings.Builder
			for _, message := range transport.sent {
				if message.ThreadID == 0 {
					general.WriteString(message.Text)
				} else {
					topic.WriteString(message.Text)
				}
			}
			want := safeText(text)
			if topic.String() != want || general.String() != "worker-1:\n"+want {
				t.Fatalf("lost or duplicated result chunks: topic=%d general=%d", topic.Len(), general.Len())
			}
			if adapter.LastEventSeq() != 1 || len(adapter.state.Outbox) != 0 {
				t.Fatalf("cursor=%d outbox=%d", adapter.LastEventSeq(), len(adapter.state.Outbox))
			}
		})
	}
}
