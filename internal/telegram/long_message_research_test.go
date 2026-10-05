package telegram

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// This exercises the exported Adapter event seam and the Telegram Transport
// boundary. It documents current destination ordering and durable checkpoints;
// it does not claim Telegram-side exactly-once delivery after an unknown ACK.
type orderedResearchTransport struct {
	*constrainedTransport
	calls  []OutgoingMessage
	failAt int
}

func (t *orderedResearchTransport) SendMessage(ctx context.Context, message OutgoingMessage) error {
	t.calls = append(t.calls, message)
	if t.failAt > 0 && len(t.calls) == t.failAt {
		return errors.New("synthetic send failure")
	}
	return t.constrainedTransport.SendMessage(ctx, message)
}

func TestLongResultTopicThenGeneralCheckpointOrder(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	transport := &orderedResearchTransport{
		constrainedTransport: &constrainedTransport{fakeTransport: &fakeTransport{}},
		failAt:               2,
	}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	text := strings.Repeat("я", 5000)
	event := Event{Sequence: 1, Kind: "worker.completed", WorkerRef: "worker-1", Title: "Long Result", Text: text, TerminalIdentity: "turn-1"}
	if err := adapter.HandleDurableEvent(ctx, event); err == nil {
		t.Fatal("expected failure on the second Topic chunk")
	}
	if len(transport.calls) != 2 || len(transport.constrainedTransport.sent) != 1 || transport.calls[0].ThreadID == 0 || transport.calls[1].ThreadID == 0 {
		t.Fatal("delivery did not start with Topic chunks and checkpoint the first acknowledged chunk")
	}

	transport.failAt = 0
	adapter = newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	adapter = newTestAdapter(t, transport, &fakeServer{}, statePath)
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}

	wantThreads := []int64{1, 1, 1, 0, 0}
	if len(transport.calls) != len(wantThreads) {
		var gotThreads []int64
		for _, call := range transport.calls {
			gotThreads = append(gotThreads, call.ThreadID)
		}
		t.Fatalf("send attempts=%d, want %d including one failed chunk and no duplicate replay; threads=%v", len(transport.calls), len(wantThreads), gotThreads)
	}
	for i, want := range wantThreads {
		if transport.calls[i].ThreadID != want {
			t.Fatalf("send attempt %d thread=%d, want %d", i+1, transport.calls[i].ThreadID, want)
		}
	}
	for _, retried := range transport.calls[2:] {
		if retried.ThreadID == transport.calls[0].ThreadID && retried.Text == transport.calls[0].Text {
			t.Fatal("replay resent the Topic chunk whose success was checkpointed")
		}
	}
	var topic, general strings.Builder
	for _, sent := range transport.constrainedTransport.fakeTransport.sent {
		if sent.ThreadID == 0 {
			general.WriteString(sent.Text)
		} else {
			topic.WriteString(sent.Text)
		}
	}
	if topic.String() != text || general.String() != "Long Result:\n"+text {
		t.Fatal("retry/replay lost, reordered, or duplicated delivered Result text")
	}
	t.Log("attempts: Topic chunk 1, Topic chunk 2 (failed), Topic chunk 2 (retry), General chunks 1-2; post-checkpoint event replays sent nothing")
}
