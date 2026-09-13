package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBotAPITransportDecodesProductionNestedMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botprivate-token/getUpdates" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{map[string]any{
			"update_id": 42,
			"message": map[string]any{
				"message_id":        9,
				"from":              map[string]any{"id": 777, "is_bot": false, "first_name": "Owner"},
				"chat":              map[string]any{"id": 555, "type": "supergroup"},
				"message_thread_id": 33,
				"text":              "/start one-time",
			},
		}}})
	}))
	defer server.Close()

	updates, err := (&BotAPITransport{BaseURL: server.URL, BotToken: "private-token"}).GetUpdates(context.Background(), 1, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Message == nil {
		t.Fatalf("updates=%#v", updates)
	}
	message := updates[0].Message
	if message.ChatID != 555 || message.FromID != 777 || message.ThreadID != 33 || message.Text != "/start one-time" {
		t.Fatalf("message=%#v", message)
	}
}
