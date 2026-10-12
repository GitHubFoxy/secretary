package telegram

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneralMirrorTruncatedRussianTaskPromptAndStandardTitle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		title string
		text  string
	}{
		{
			name:  "reported Russian task prompt screenshot",
			title: "как будет пачкать по английски ? чтобы было разнве...",
			text:  "«Пачкать» по-английски — по-разному в зависимости от контекста:\n1. stain\n2. soil\n3. smear",
		},
		{
			name:  "standard title",
			title: "Weather Report",
			text:  "Sunny, 22°C with light breeze.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{}
			server := &fakeServer{}
			adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
			event := Event{
				Kind:             "worker.completed",
				WorkerRef:        "worker-test",
				Title:            tc.title,
				Text:             tc.text,
				TerminalIdentity: "turn:1",
			}

			if err := adapter.HandleEvent(context.Background(), event); err != nil {
				t.Fatal(err)
			}

			if len(transport.topics) != 1 {
				t.Fatalf("expected 1 topic created, got %d", len(transport.topics))
			}
			if transport.topics[0].Name != tc.title {
				t.Fatalf("expected topic name %q, got %q", tc.title, transport.topics[0].Name)
			}

			var topicMsg, generalMsg *SentMessage
			for i := range transport.sent {
				s := &transport.sent[i]
				if s.ThreadID != 0 {
					topicMsg = s
				} else {
					generalMsg = s
				}
			}

			if topicMsg == nil || generalMsg == nil {
				t.Fatalf("expected both topic and general messages, got sent=%#v", transport.sent)
			}
			if topicMsg.Text != tc.text {
				t.Fatalf("topic message text = %q, want %q", topicMsg.Text, tc.text)
			}
			if generalMsg.Text != tc.text {
				t.Fatalf("general mirror text = %q, want %q", generalMsg.Text, tc.text)
			}
			if strings.HasPrefix(generalMsg.Text, tc.title) || strings.HasPrefix(generalMsg.Text, "Worker:") {
				t.Fatalf("general mirror contains forbidden prefix: %q", generalMsg.Text)
			}
		})
	}
}

func TestGeneralMirrorTerminalKindsAndEmptyFallback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      string
		text      string
		wantBody  string
		workerRef string
		termID    string
	}{
		{
			name:      "worker completed with text",
			kind:      "worker.completed",
			text:      "Success result text",
			wantBody:  "Success result text",
			workerRef: "w-1",
			termID:    "turn:1",
		},
		{
			name:      "worker failed with error text",
			kind:      "worker.failed",
			text:      "Process exited with error code 1",
			wantBody:  "Process exited with error code 1",
			workerRef: "w-2",
			termID:    "turn:2",
		},
		{
			name:      "worker canceled with note",
			kind:      "worker.canceled",
			text:      "Execution canceled by user",
			wantBody:  "Execution canceled by user",
			workerRef: "w-3",
			termID:    "turn:3",
		},
		{
			name:      "worker completed empty text fallback",
			kind:      "worker.completed",
			text:      "",
			wantBody:  workerEventLabel("worker.completed"),
			workerRef: "w-4",
			termID:    "turn:4",
		},
		{
			name:      "worker failed empty text fallback",
			kind:      "worker.failed",
			text:      "",
			wantBody:  workerEventLabel("worker.failed"),
			workerRef: "w-5",
			termID:    "turn:5",
		},
		{
			name:      "worker canceled empty text fallback",
			kind:      "worker.canceled",
			text:      "",
			wantBody:  workerEventLabel("worker.canceled"),
			workerRef: "w-6",
			termID:    "turn:6",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &fakeTransport{}
			server := &fakeServer{}
			adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
			event := Event{
				Kind:             tc.kind,
				WorkerRef:        tc.workerRef,
				Title:            "TopicTitle",
				Text:             tc.text,
				TerminalIdentity: tc.termID,
			}

			if err := adapter.HandleEvent(context.Background(), event); err != nil {
				t.Fatal(err)
			}

			var topicMsg, generalMsg *SentMessage
			for i := range transport.sent {
				s := &transport.sent[i]
				if s.ThreadID != 0 {
					topicMsg = s
				} else {
					generalMsg = s
				}
			}

			if topicMsg == nil || generalMsg == nil {
				t.Fatalf("expected both topic and general messages, got sent=%#v", transport.sent)
			}
			if topicMsg.Text != tc.wantBody {
				t.Fatalf("topic text=%q, want=%q", topicMsg.Text, tc.wantBody)
			}
			if generalMsg.Text != tc.wantBody {
				t.Fatalf("general text=%q, want=%q", generalMsg.Text, tc.wantBody)
			}
		})
	}
}

func TestGeneralMirrorFollowUpTurnPreservesSameRule(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	ctx := context.Background()

	e1 := Event{
		Kind:             "worker.completed",
		WorkerRef:        "worker-followup",
		Title:            "Follow-up Worker",
		Text:             "First result",
		TerminalIdentity: "turn:1",
	}
	if err := adapter.HandleEvent(ctx, e1); err != nil {
		t.Fatal(err)
	}

	e2 := Event{
		Kind:             "worker.completed",
		WorkerRef:        "worker-followup",
		Title:            "Follow-up Worker",
		Text:             "Follow-up result",
		TerminalIdentity: "turn:2",
	}
	if err := adapter.HandleEvent(ctx, e2); err != nil {
		t.Fatal(err)
	}

	if len(transport.sent) != 4 {
		t.Fatalf("expected 4 sent messages (2 topic + 2 general), got %d: %#v", len(transport.sent), transport.sent)
	}

	var generals []string
	for _, s := range transport.sent {
		if s.ThreadID == 0 {
			generals = append(generals, s.Text)
		}
	}
	if len(generals) != 2 || generals[0] != "First result" || generals[1] != "Follow-up result" {
		t.Fatalf("unexpected general messages: %#v", generals)
	}
}

func TestGeneralMirrorPreservesAuthorTopicPrefix(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	title := "Summary"
	authorText := "Summary:\nHere is the detailed summary."
	event := Event{
		Kind:             "worker.completed",
		WorkerRef:        "w-author",
		Title:            title,
		Text:             authorText,
		TerminalIdentity: "turn:auth",
	}

	if err := adapter.HandleEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	var generalMsg *SentMessage
	for i := range transport.sent {
		if transport.sent[i].ThreadID == 0 {
			generalMsg = &transport.sent[i]
		}
	}
	if generalMsg == nil {
		t.Fatal("missing general message")
	}
	if generalMsg.Text != authorText {
		t.Fatalf("author text altered: got %q, want %q", generalMsg.Text, authorText)
	}
}

func TestGeneralMirrorDistinctResultsSameBodyNotMerged(t *testing.T) {
	transport := &fakeTransport{}
	server := &fakeServer{}
	adapter := newTestAdapter(t, transport, server, filepath.Join(t.TempDir(), "telegram.json"))
	ctx := context.Background()

	body := "Identical response body"
	e1 := Event{
		Kind:             "worker.completed",
		WorkerRef:        "worker-a",
		Title:            "Worker A",
		Text:             body,
		TerminalIdentity: "identity-a",
	}
	e2 := Event{
		Kind:             "worker.completed",
		WorkerRef:        "worker-b",
		Title:            "Worker B",
		Text:             body,
		TerminalIdentity: "identity-b",
	}

	if err := adapter.HandleEvent(ctx, e1); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleEvent(ctx, e2); err != nil {
		t.Fatal(err)
	}

	if len(transport.sent) != 4 {
		t.Fatalf("expected 4 sent messages, got %d: %#v", len(transport.sent), transport.sent)
	}
	var topicA, topicB, genA, genB int
	for _, s := range transport.sent {
		if s.Identity == "terminal:identity-a" {
			topicA++
		}
		if s.Identity == "terminal:identity-a:general" {
			genA++
		}
		if s.Identity == "terminal:identity-b" {
			topicB++
		}
		if s.Identity == "terminal:identity-b:general" {
			genB++
		}
	}
	if topicA != 1 || genA != 1 || topicB != 1 || genB != 1 {
		t.Fatalf("unexpected message distribution: topicA=%d genA=%d topicB=%d genB=%d", topicA, genA, topicB, genB)
	}
}

func TestGeneralMirrorFailAfterTopicSuccessAndRecovery(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "telegram.json")

	transport := &constrainedTransport{fakeTransport: &fakeTransport{}, failCall: 2}
	adapter := newTestAdapter(t, transport, &fakeServer{}, statePath)
	event := Event{
		Sequence:         1,
		Kind:             "worker.completed",
		WorkerRef:        "w-retry",
		Title:            "Retry Topic",
		Text:             "Delivered Text",
		TerminalIdentity: "turn-retry-1",
	}

	if err := adapter.HandleDurableEvent(ctx, event); err == nil {
		t.Fatal("expected temporary failure on General")
	}

	if len(transport.sent) != 1 || transport.sent[0].ThreadID == 0 {
		t.Fatalf("expected 1 delivered topic message before failure, got %#v", transport.sent)
	}

	transport.failCall = 0
	restarted := newTestAdapter(t, transport, &fakeServer{}, statePath)

	if err := restarted.HandleDurableEvent(ctx, event); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	if err := restarted.HandleDurableEvent(ctx, event); err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	var topicCount, generalCount int
	for _, s := range transport.sent {
		if s.ThreadID != 0 {
			topicCount++
			if s.Text != "Delivered Text" {
				t.Fatalf("unexpected topic text %q", s.Text)
			}
		} else {
			generalCount++
			if s.Text != "Delivered Text" {
				t.Fatalf("unexpected general text %q", s.Text)
			}
		}
	}
	if topicCount != 1 || generalCount != 1 {
		t.Fatalf("expected exactly 1 topic and 1 general message, got topic=%d general=%d", topicCount, generalCount)
	}
}

func TestGeneralMirrorLongBodySplittingAndPlainFallback(t *testing.T) {
	ctx := context.Background()
	transport := &constrainedTransport{fakeTransport: &fakeTransport{}}
	adapter := newTestAdapter(t, transport, &fakeServer{}, filepath.Join(t.TempDir(), "telegram.json"))

	repeatBlock := "Рецепт 🍹 Напиток с цитрусом и мятой. Отличный освежающий вкус.\n\n"
	text := strings.Repeat(repeatBlock, 100)
	event := Event{
		Kind:             "worker.completed",
		WorkerRef:        "worker-long",
		Title:            "Кулинарный гид",
		Text:             text,
		TerminalIdentity: "long-turn-1",
	}

	if err := adapter.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}

	var topicReconstructed, generalReconstructed strings.Builder
	var topicChunks, generalChunks int
	for _, s := range transport.sent {
		if s.ThreadID != 0 {
			topicChunks++
			topicReconstructed.WriteString(s.Text)
		} else {
			generalChunks++
			generalReconstructed.WriteString(s.Text)
			if strings.HasPrefix(s.Text, "Кулинарный гид") || strings.HasPrefix(s.Text, "Worker:") {
				t.Fatalf("chunk has forbidden prefix: %q", s.Text)
			}
		}
	}

	if topicChunks < 2 || generalChunks < 2 {
		t.Fatalf("expected multiple chunks, got topic=%d general=%d", topicChunks, generalChunks)
	}
	want := safeText(text)
	if topicReconstructed.String() != want {
		t.Fatalf("topic reconstructed text mismatch")
	}
	if generalReconstructed.String() != want {
		t.Fatalf("general reconstructed text mismatch: got len=%d want len=%d", generalReconstructed.Len(), len(want))
	}
}
