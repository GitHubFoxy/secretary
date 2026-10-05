package telegram

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

type capturedBotAPIMessage struct {
	threadID int64
	text     string
	mode     string
}

func deliverResultToBotAPIFixture(t *testing.T, workerRef, result string) []capturedBotAPIMessage {
	t.Helper()
	var sent []capturedBotAPIMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "createForumTopic"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_thread_id": 17, "name": "Long result"}})
		case strings.HasSuffix(r.URL.Path, "sendMessage"):
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse sendMessage form: %v", err)
				return
			}
			threadID, _ := strconv.ParseInt(r.Form.Get("message_thread_id"), 10, 64)
			sent = append(sent, capturedBotAPIMessage{threadID: threadID, text: r.Form.Get("text"), mode: r.Form.Get("parse_mode")})
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	adapter, err := New(Config{StatePath: filepath.Join(t.TempDir(), "telegram.json"), OwnerChatID: 100}, &BotAPITransport{BaseURL: server.URL, BotToken: "fixture"}, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleDurableEvent(context.Background(), Event{Sequence: 1, Kind: "worker.completed", WorkerRef: workerRef, Title: "Long result", Text: result, TerminalIdentity: "turn-1"}); err != nil {
		t.Fatal(err)
	}
	return sent
}

func telegramHTMLVisibleText(text string) string {
	var visible strings.Builder
	for index := 0; index < len(text); {
		if text[index] == '<' {
			if end := strings.IndexByte(text[index:], '>'); end > 0 {
				index += end + 1
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(text[index:])
		visible.WriteString(text[index : index+size])
		index += size
	}
	return html.UnescapeString(visible.String())
}

func topicTexts(t *testing.T, sent []capturedBotAPIMessage) []string {
	t.Helper()
	var topic []string
	for _, message := range sent {
		if message.threadID != 17 {
			continue
		}
		if message.mode != "HTML" {
			t.Fatalf("Topic parse_mode=%q, want HTML", message.mode)
		}
		if units := len(utf16.Encode([]rune(telegramHTMLVisibleText(message.text)))); units > 4096 {
			t.Fatalf("Topic message has %d visible UTF-16 units, limit is 4096", units)
		}
		topic = append(topic, message.text)
	}
	return topic
}

func TestLegacyPartialFencedOutboxResumesAtOriginalSourceByteOffset(t *testing.T) {
	ctx := context.Background()
	statePath := filepath.Join(t.TempDir(), "telegram.json")
	body := strings.Repeat("🍹", 5000) + "\n"
	source := "```go\n" + body + "```\n"
	legacyDeliveredBytes := len("```go\n") + 2045*len("🍹")
	legacyState, err := json.Marshal(map[string]any{
		"version":       1,
		"owner_chat_id": 100,
		"outbox": []map[string]any{{
			"ChatID": 100, "ThreadID": 17, "Text": source,
			"Identity": "terminal:legacy", "delivered_bytes": legacyDeliveredBytes,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, legacyState, 0o600); err != nil {
		t.Fatal(err)
	}

	var requests []capturedBotAPIMessage
	failAt := 2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse sendMessage form: %v", err)
			return
		}
		threadID, _ := strconv.ParseInt(r.Form.Get("message_thread_id"), 10, 64)
		requests = append(requests, capturedBotAPIMessage{threadID: threadID, text: r.Form.Get("text"), mode: r.Form.Get("parse_mode")})
		if len(requests) == failAt {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 503, "description": "fixture restart"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()
	transport := &BotAPITransport{BaseURL: server.URL, BotToken: "fixture"}
	adapter, err := New(Config{StatePath: statePath, OwnerChatID: 100}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err == nil {
		t.Fatal("expected transient failure after one acknowledged migrated chunk")
	}
	failAt = 0
	adapter, err = New(Config{StatePath: statePath, OwnerChatID: 100}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 || requests[0].threadID != 17 || requests[1].threadID != 17 || requests[2].threadID != 17 {
		t.Fatalf("legacy outbox requests=%d, expected three sequential Topic chunks", len(requests))
	}
	first := telegramHTMLVisibleText(requests[0].text)
	second := telegramHTMLVisibleText(requests[1].text)
	resumed := telegramHTMLVisibleText(requests[2].text)
	expectedFirst := strings.Repeat("🍹", 2048)
	expectedSecond := strings.Repeat("🍹", 907) + "\n"
	if requests[0].mode != "HTML" || requests[1].mode != "HTML" || requests[2].mode != "HTML" ||
		first != expectedFirst || second != expectedSecond || resumed != second ||
		!strings.HasPrefix(requests[0].text, "<pre>") || !strings.HasSuffix(requests[0].text, "</pre>") {
		t.Fatal("legacy checkpoint was interpreted as rendered HTML/fence bytes or acknowledged source was repeated")
	}
}

func TestLongResultReopensLongFencedCodeInEveryBotAPIChunk(t *testing.T) {
	body := strings.Repeat("x", 5000) + "\n"
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "fence-worker", "```go\n"+body+"```\n"))
	var reconstructed strings.Builder
	for _, message := range topic {
		if !strings.HasPrefix(message, "<pre>") || !strings.HasSuffix(message, "</pre>") || strings.Contains(message, "```") {
			t.Fatal("a long fenced-code chunk was not independently formatted with balanced tags")
		}
		reconstructed.WriteString(telegramHTMLVisibleText(message))
	}
	if topicUTF16Units := len(utf16.Encode([]rune(reconstructed.String()))); topicUTF16Units != len(utf16.Encode([]rune(body))) || reconstructed.String() != body {
		t.Fatalf("fenced code changed across chunks: units=%d", topicUTF16Units)
	}
}

func TestOversizedMarkdownAtomsRemainLosslessEscapedTextThroughBotAPI(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"link", "[" + strings.Repeat("x", 5000) + "](https://example.org)"},
		{"inline-code", "`" + strings.Repeat("x", 5000) + "`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			topic := topicTexts(t, deliverResultToBotAPIFixture(t, tc.name+"-oversized-worker", tc.text))
			var reconstructed strings.Builder
			for _, message := range topic {
				if strings.Contains(message, "<a href=") || strings.Contains(message, "<code>") {
					t.Fatal("an oversized Markdown atom was partially formatted")
				}
				reconstructed.WriteString(telegramHTMLVisibleText(message))
			}
			if reconstructed.String() != tc.text {
				t.Fatal("oversized Markdown fallback lost source content")
			}
		})
	}
}

func TestLongResultKeepsFittingInlineCodeWholeThroughBotAPI(t *testing.T) {
	code := "`" + strings.Repeat("x", 10) + "`"
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "code-worker", strings.Repeat("А", 4094)+code))
	found := false
	for _, message := range topic {
		if strings.Contains(message, "<code>"+strings.Repeat("x", 10)+"</code>") {
			found = true
		}
		if strings.Contains(message, "`x") || strings.Contains(message, "x`") {
			t.Fatal("a fitting inline code span was split and delivered literally")
		}
	}
	if !found {
		t.Fatal("the complete fitting inline code span was not formatted")
	}
}

func TestLongResultKeepsFittingMarkdownLinkWholeThroughBotAPI(t *testing.T) {
	link := "[source](https://example.org/p?x=1&y=2)"
	result := strings.Repeat("А", 4090) + link
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "link-worker", result))
	want := `<a href="https://example.org/p?x=1&amp;y=2">source</a>`
	found := false
	for _, message := range topic {
		if strings.Contains(message, want) {
			found = true
		}
		if strings.Contains(message, "[source") || strings.Contains(message, "](https://example.org") {
			t.Fatal("a fitting Markdown link was split and delivered literally")
		}
	}
	if !found {
		t.Fatal("the complete fitting Markdown link was not rendered as one Telegram anchor")
	}
}

func TestBotAPIChunkLimitCountsParsedUTF16NotEscapedHTMLBytes(t *testing.T) {
	result := strings.Repeat("&", 4096)
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "escaped-worker", result))
	wireChars := 0
	if len(topic) > 0 {
		wireChars = len(topic[0])
	}
	if len(topic) != 1 || wireChars != 20480 || telegramHTMLVisibleText(topic[0]) != result {
		t.Fatalf("HTML escaping changed the parsed 4096-unit message: chunks=%d wire_chars=%d", len(topic), wireChars)
	}
}

func TestResultKeepsExtendedGraphemeClustersWholeThroughBotAPI(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, cluster string
	}{
		{"combining", strings.Repeat("А", 4095), "e\u0301"},
		{"zwj-emoji", strings.Repeat("А", 4094), "👩\u200d💻"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.prefix + tc.cluster
			topic := topicTexts(t, deliverResultToBotAPIFixture(t, tc.name+"-worker", result))
			if len(topic) != 2 || topic[0] != tc.prefix || topic[1] != tc.cluster || strings.Join(topic, "") != result {
				t.Fatal("a normal extended grapheme cluster was split or dropped")
			}
		})
	}
}

func TestSingleOversizedGraphemeUsesLosslessCodePointFallbackThroughBotAPI(t *testing.T) {
	result := "e" + strings.Repeat("\u0301", 4096)
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "oversized-grapheme-worker", result))
	var reconstructed strings.Builder
	for _, message := range topic {
		visible := telegramHTMLVisibleText(message)
		if visible == "" {
			t.Fatal("oversized grapheme fallback made no source progress")
		}
		reconstructed.WriteString(visible)
	}
	if reconstructed.String() != result {
		t.Fatalf("oversized grapheme fallback lost code points: got=%d want=%d bytes", reconstructed.Len(), len(result))
	}
}

func TestLongResultSplitsAtParagraphBoundaryThroughBotAPI(t *testing.T) {
	firstParagraph := strings.Repeat("А", 4089)
	secondParagraph := strings.Repeat("Б", 20)
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "paragraph-worker", firstParagraph+"\n\n"+secondParagraph))
	if len(topic) != 2 || topic[0] != firstParagraph+"\n\n" || topic[1] != secondParagraph {
		firstUnits := 0
		if len(topic) > 0 {
			firstUnits = len(utf16.Encode([]rune(topic[0])))
		}
		t.Fatalf("Topic chunks did not keep the paragraph boundary and full text: chunks=%d first_units=%d", len(topic), firstUnits)
	}
}

func TestLongResultKeepsSentenceBoundaryThroughBotAPI(t *testing.T) {
	firstSentence := strings.Repeat("А", 4089) + ". "
	secondSentence := strings.Repeat("Б", 20)
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "sentence-worker", firstSentence+secondSentence))
	if len(topic) != 2 || topic[0] != firstSentence || topic[1] != secondSentence {
		t.Fatalf("Topic chunks did not split after the complete sentence: chunks=%d", len(topic))
	}
}

func TestLongResultKeepsListItemBoundaryThroughBotAPI(t *testing.T) {
	firstItem := "- " + strings.Repeat("А", 4092)
	topic := topicTexts(t, deliverResultToBotAPIFixture(t, "list-worker", firstItem+"\n- next"))
	if len(topic) != 2 || topic[0] != firstItem+"\n" || topic[1] != "- next" {
		t.Fatalf("Topic chunks split a list item or lost content: chunks=%d", len(topic))
	}
}
