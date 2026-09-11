package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestNodeEndpointsAreAbsentWhenNodeServiceIsDisabled(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "nodes-disabled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled Node service status=%d", response.StatusCode)
	}
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

func TestBootstrapAndSecretaryModelSelection(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachSecretaryModelCatalog(func() map[string]string {
		return map[string]string{"default": "provider/default", "fast": "provider/fast"}
	}, func() string { return "default" }, nil)
	httpServer := httptest.NewServer(api.Handler())
	defer httpServer.Close()
	client := &http.Client{}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	login(t, client, httpServer.URL)
	response, err := client.Get(httpServer.URL + "/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var bootstrap bootstrapResponse
	if err := json.NewDecoder(response.Body).Decode(&bootstrap); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if bootstrap.ConversationID == "" || bootstrap.Secretary.Selected != "default" || bootstrap.Secretary.Models["fast"] != "provider/fast" {
		t.Fatalf("bootstrap=%#v", bootstrap)
	}
	payload := bytes.NewBufferString(`{"model":"fast"}`)
	response, err = client.Post(httpServer.URL+"/v1/secretary/model", "application/json", payload)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("model status=%d", response.StatusCode)
	}
	response.Body.Close()
	selected, found, err := store.GetSetting(context.Background(), secretaryModelSetting)
	if err != nil || !found || selected != "fast" {
		t.Fatalf("selected=%q found=%v err=%v", selected, found, err)
	}
}

func TestControlHandlerIsDebugOnly(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	controlMux := http.NewServeMux()
	controlMux.Handle("/v1/", api.Handler())
	controlMux.Handle("/v1/control/", api.ControlHandler())
	controlServer := httptest.NewServer(controlMux)
	defer controlServer.Close()
	response, err := controlServer.Client().Get(controlServer.URL + "/v1/control/overview")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("non-debug control status=%d", response.StatusCode)
	}
	response.Body.Close()
	api.SetDebug(true)
	debugServer := httptest.NewServer(controlMux)
	defer debugServer.Close()
	debugJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	debugClient := &http.Client{Jar: debugJar}
	login(t, debugClient, debugServer.URL)
	response, err = debugClient.Get(debugServer.URL + "/v1/control/overview")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("debug control status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestControlProfilesCanBeViewedAndEdited(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.SetDebug(true)
	saved := "original worker profile"
	reloaded := false
	api.AttachControl(ControlOptions{
		ProfileFiles: func() ([]ProfileFile, error) {
			return []ProfileFile{
				{Name: "secretary", Path: "/profiles/secretary.md", Content: "secretary", Hash: "hash-1", Runtime: "codex", Model: "default", Reasoning: "default"},
				{Name: "worker", Path: "/profiles/worker.md", Content: saved, Hash: "hash-2", Runtime: "codex", Model: "smart", Reasoning: "default"},
				{Name: "child_worker", Path: "/profiles/child-worker.md", Content: "child", Hash: "hash-3", Runtime: "codex", Model: "smart", Reasoning: "default"},
			}, nil
		},
		ReloadConfig: func() (any, error) {
			reloaded = true
			return map[string]string{"status": "reloaded"}, nil
		},
		ApplyProfile: func(name string, content []byte) error {
			if name != "worker" {
				return errors.New("unexpected profile")
			}
			saved = string(content)
			return nil
		},
	})
	mux := http.NewServeMux()
	mux.Handle("/v1/", api.Handler())
	mux.Handle("/v1/control/", api.ControlHandler())
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &http.Client{}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/control/profiles")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("profiles status=%d", response.StatusCode)
	}
	var profiles []ProfileFile
	if err := json.NewDecoder(response.Body).Decode(&profiles); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(profiles) != 3 || profiles[1].Name != "worker" || profiles[1].Content != saved {
		t.Fatalf("profiles=%#v", profiles)
	}
	response, err = client.Post(server.URL+"/v1/control/profiles/reload", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !reloaded {
		t.Fatalf("profile reload status=%d reloaded=%v", response.StatusCode, reloaded)
	}
	response.Body.Close()
	request, err := http.NewRequest(http.MethodPut, server.URL+"/v1/control/profiles/worker", bytes.NewBufferString(`{"content":"updated worker profile"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("profile update status=%d", response.StatusCode)
	}
	response.Body.Close()
	if saved != "updated worker profile" {
		t.Fatalf("saved profile=%q", saved)
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
