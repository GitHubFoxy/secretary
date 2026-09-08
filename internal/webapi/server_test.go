package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/coder/websocket"
)

func testServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(api.Handler())
	t.Cleanup(httpServer.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return httpServer, &http.Client{Jar: jar}
}

func login(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	response, err := client.Post(baseURL+"/v1/web/session", "application/json", bytes.NewBufferString(`{"bootstrap_token":"bootstrap"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login status = %d", response.StatusCode)
	}
}

func postMessage(t *testing.T, client *http.Client, baseURL, id, body string) map[string]any {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"external_message_id": id, "body": body})
	response, err := client.Post(baseURL+"/v1/messages", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("message status = %d", response.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWebLoginConversationAndInboundDeduplication(t *testing.T) {
	server, client := testServer(t)
	login(t, client, server.URL)
	first := postMessage(t, client, server.URL, "m-1", "hello")
	if first["duplicate"] != false {
		t.Fatalf("first response = %#v", first)
	}
	duplicate := postMessage(t, client, server.URL, "m-1", "hello")
	if duplicate["duplicate"] != true {
		t.Fatalf("duplicate response = %#v", duplicate)
	}

	response, err := client.Get(server.URL + "/v1/conversation?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var entries []map[string]any
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0]["body"] != "hello" || entries[0]["seq"].(float64) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestWebSocketReplaysThenReceivesLiveEntries(t *testing.T) {
	server, client := testServer(t)
	login(t, client, server.URL)
	postMessage(t, client, server.URL, "m-1", "before connect")

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(parsed) {
		header.Add("Cookie", cookie.String())
	}
	parsed.Scheme = "ws"
	parsed.Path = "/v1/ws"
	parsed.RawQuery = "after_seq=0"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if response != nil {
			t.Fatalf("dial websocket: %v, status=%d", err, response.StatusCode)
		}
		t.Fatal(err)
	}
	defer connection.CloseNow()
	_, replay, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var replayEntry map[string]any
	if err := json.Unmarshal(replay, &replayEntry); err != nil || replayEntry["body"] != "before connect" {
		t.Fatalf("replay = %s, err=%v", replay, err)
	}

	postMessage(t, client, server.URL, "m-2", "after connect")
	_, live, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var liveEntry map[string]any
	if err := json.Unmarshal(live, &liveEntry); err != nil || liveEntry["body"] != "after connect" {
		t.Fatalf("live = %s, err=%v", live, err)
	}
}
