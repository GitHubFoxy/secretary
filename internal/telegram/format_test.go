package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTelegramSafeFormattingPreservesLayout(t *testing.T) {
	const raw = "# Погода\n\n- **Сейчас:** <5 & 7>\n- [36:21](https://example.org/a_(b)?x=1&y=2)\n\n```go\nfmt.Println(\"🍹\")\n```\n"
	clean := safeText(raw)
	if clean != strings.TrimSpace(raw) {
		t.Fatal("sanitization changed meaningful newlines/spacing")
	}
	formatted, plain := formatTelegramChunk(clean)
	for _, want := range []string{"<b>Погода</b>\n\n- <b>Сейчас:</b> &lt;5 &amp; 7&gt;", `<a href="https://example.org/a_(b)?x=1&amp;y=2">36:21</a>`, "<pre>fmt.Println(&#34;🍹&#34;)\n</pre>"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("supported formatting missing: %q", want)
		}
	}
	if !strings.Contains(plain, "\n\n- ") || !strings.Contains(plain, "36:21 (https://example.org/a_(b)?x=1&y=2)") {
		t.Fatal("fallback is not readable")
	}
}

func TestTelegramFormatterEscapesMarkupAndRejectsUnsafeLinks(t *testing.T) {
	for _, target := range []string{"javascript:alert(1)", "data:text/html,x", "file:///tmp/a", "tg://user?id=1", "https://user:pass@example.org", "https://example.org/\tpath", "//example.org/path", "https://example.org/%zz"} {
		formatted, plain := formatTelegramChunk("[label](" + target + ")")
		if strings.Contains(formatted, "<a ") || formatted != "label" || plain != "label" {
			t.Fatalf("unsafe link retained: scheme_only_rejected=%t", !strings.Contains(formatted, "<a "))
		}
	}
	formatted, plain := formatTelegramChunk(`<b onclick="x">literal & text</b> [broken](https://example.org`)
	if strings.Contains(formatted, "<b") || !strings.Contains(formatted, "&lt;b") || !strings.Contains(plain, "[broken]") {
		t.Fatal("untrusted/incomplete markup was not readable and escaped")
	}
	if got := safeText("# Heading\n\n- worker_ref: private\n- safe"); !strings.Contains(got, "\n\n- [redacted]\n- safe") {
		t.Fatal("redaction removed layout or retained internal identity")
	}
	message, _ := secretaryBatchMessage(1, "api_"+"token=private-value", nil)
	if strings.Contains(message.Text, "private-value") {
		t.Fatal("redaction missed concatenated delta marker")
	}
}

// Exercise the production HTTP encoding/classifier plus real adapter outbox.
// The HTTP fixture is not real Telegram acceptance.
func TestTelegramParseRejectionFallbackRetryAndDedup(t *testing.T) {
	ctx := context.Background()
	var modes []string
	var delivered []string
	failPlain := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.HasSuffix(r.URL.Path, "createForumTopic") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_thread_id": 2, "name": "Погода"}})
			return
		}
		if !strings.HasSuffix(r.URL.Path, "sendMessage") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		mode := r.Form.Get("parse_mode")
		modes = append(modes, mode)
		if mode != "" {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Bad Request: can't parse entities", "error_code": 400})
			return
		}
		if failPlain {
			failPlain = false
			w.WriteHeader(503)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "temporary transport failure", "error_code": 503})
			return
		}
		delivered = append(delivered, r.Form.Get("text"))
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()
	transport := &BotAPITransport{BaseURL: server.URL, BotToken: "fixture"}
	state := filepath.Join(t.TempDir(), "state.json")
	adapter := newTestAdapter(t, transport, &fakeServer{}, state)
	event := Event{Sequence: 1, Kind: "worker.completed", WorkerRef: "wrk_private", Title: "Погода", Text: "# Ответ\n\n- [source](https://example.org)\n- <safe>", TerminalIdentity: "result-fixture"}
	if err := adapter.HandleDurableEvent(ctx, event); err == nil {
		t.Fatal("non-format error from fallback was hidden")
	}
	if len(modes) != 2 || modes[0] != "HTML" || modes[1] != "" || len(adapter.state.Outbox) != 1 || !adapter.state.Outbox[0].PlainFallback {
		t.Fatal("fallback was not checkpointed before retry")
	}
	adapter = newTestAdapter(t, transport, &fakeServer{}, state)
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := adapter.HandleDurableEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 2 || modes[2] != "" || strings.Contains(delivered[1], "wrk_") || !strings.Contains(delivered[0], "\n\n- source (https://example.org)") || len(adapter.state.Outbox) != 0 {
		t.Fatal("retry/dedup/layout/source identity checks failed")
	}
}

func TestTelegramNonFormattingFailuresNeverImmediatelyFallback(t *testing.T) {
	for _, tc := range []struct {
		code        int
		description string
	}{
		{429, "Too Many Requests"}, {503, "can't parse entities"}, {400, "message is too long"}, {403, "Forbidden"},
	} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(tc.code)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": tc.code, "description": tc.description})
		}))
		adapter := newTestAdapter(t, &BotAPITransport{BaseURL: server.URL, BotToken: "fixture"}, &fakeServer{}, filepath.Join(t.TempDir(), "state.json"))
		err := adapter.sendMessage(context.Background(), OutgoingMessage{ChatID: 100, Text: "[source](https://example.org)", Identity: "test"})
		server.Close()
		if err == nil || errors.Is(err, ErrFormatting) || calls != 1 || adapter.state.Outbox[0].PlainFallback {
			t.Fatal("transport failure incorrectly triggered duplicate immediate send")
		}
	}
	if !errors.Is(botAPIError("sendMessage", 400, "Bad Request: can't parse entities"), ErrFormatting) {
		t.Fatal("known parse rejection not classified")
	}
}

func TestTelegramHTMLExpansionKeepsParsedSizeWithinLimit(t *testing.T) {
	raw := strings.Repeat("&", 4096)
	formatted, plain := formatTelegramChunk(telegramMessageChunk(raw))
	parsed := html.UnescapeString(formatted)
	if len(formatted) != 20480 || parsed != raw || plain != raw || len(utf16.Encode([]rune(parsed))) != 4096 {
		t.Fatal("HTML entity expansion changed parsed content or UTF-16 size accounting")
	}
}

func TestTelegramCurrentChunkFormattingHasBalancedTags(t *testing.T) {
	text := strings.Repeat("я", 4090) + "[source](https://example.org/path)\n\n```go\ntext\n```"
	var reconstructed strings.Builder
	for len(text) > 0 {
		raw := telegramMessageChunk(text)
		html, plain := formatTelegramChunk(raw)
		if strings.Count(html, "<a ") != strings.Count(html, "</a>") || strings.Count(html, "<pre>") != strings.Count(html, "</pre>") || plain == "" {
			t.Fatal("a source size boundary produced unbalanced HTML or lost content")
		}
		reconstructed.WriteString(raw)
		text = text[len(raw):]
	}
	if reconstructed.Len() == 0 {
		t.Fatal("Result disappeared")
	}
}
