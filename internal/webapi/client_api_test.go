package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestClientPairingScopesCredentialIsolationAndRevoke(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "clients.db"))
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

	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"device_id":"pi-1","display_name":"Pi","platform":"pi","scopes":["conversation:read"]}`)
	if pair.status != http.StatusCreated || pair.body["status"] != "pending" || pair.body["client_id"] == nil {
		t.Fatalf("pair=%d %#v", pair.status, pair.body)
	}

	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)
	clientID := pair.body["client_id"].(string)
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/approve", `{}`)
	if approve.status != http.StatusOK || approve.body["credential"] == "" || approve.body["status"] != "active" {
		t.Fatalf("approve=%d %#v", approve.status, approve.body)
	}
	credential := approve.body["credential"].(string)

	client := &http.Client{}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("client conversation status=%d", response.StatusCode)
	}
	response.Body.Close()

	request, _ = http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	request.Header.Set("Authorization", "Bearer bootstrap")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bootstrap accepted as Client status=%d", response.StatusCode)
	}
	response.Body.Close()

	revoke := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/revoke", `{}`)
	if revoke.status != http.StatusOK || revoke.body["status"] != "revoked" {
		t.Fatalf("revoke=%d %#v", revoke.status, revoke.body)
	}
	request, _ = http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked Client status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestClientAcknowledgementUserRevisionAndLegacyResponseRedaction(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "contract.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	userPath := filepath.Join(t.TempDir(), "user.md")
	if err := os.WriteFile(userPath, []byte("prefer short answers"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadUserDocument(context.Background(), userPath); err != nil {
		t.Fatal(err)
	}
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)

	user := getJSON(t, owner, server.URL+"/v1/user")
	if user.status != http.StatusOK || user.body["content"] != "prefer short answers" || user.body["revision"].(float64) != 1 {
		t.Fatalf("user=%d %#v", user.status, user.body)
	}
	updated := requestJSON(t, owner, http.MethodPut, server.URL+"/v1/user", `{"content":"prefer durable context"}`)
	if updated.status != http.StatusOK || updated.body["content"] != "prefer durable context" || updated.body["revision"].(float64) != 2 {
		t.Fatalf("updated=%d %#v", updated.status, updated.body)
	}
	invalid := requestJSON(t, owner, http.MethodPut, server.URL+"/v1/user", `{"content":"bad\u0000document"}`)
	if invalid.status != http.StatusBadRequest {
		t.Fatalf("invalid user status=%d body=%#v", invalid.status, invalid.body)
	}
	still := getJSON(t, owner, server.URL+"/v1/user")
	if still.body["content"] != "prefer durable context" || still.body["revision"].(float64) != 2 {
		t.Fatalf("invalid update replaced snapshot: %#v", still.body)
	}

	message := postJSON(t, owner, server.URL+"/v1/messages", `{"external_message_id":"client-message-1","body":"hello"}`)
	for _, key := range []string{"message_id", "entry_seq", "state", "duplicate"} {
		if _, ok := message.body[key]; !ok {
			t.Fatalf("ack missing %q: %#v", key, message.body)
		}
	}
	duplicate := postJSON(t, owner, server.URL+"/v1/messages", `{"external_message_id":"client-message-1","body":"hello"}`)
	if duplicate.body["duplicate"] != true || duplicate.body["message_id"] != message.body["message_id"] || duplicate.body["entry_seq"] != message.body["entry_seq"] {
		t.Fatalf("duplicate acknowledgement=%#v first=%#v", duplicate.body, message.body)
	}
}

func TestClientSurfaceUsesWorkerEntitiesAndOrderedReplayRoute(t *testing.T) {
	server, owner := testServer(t)
	login(t, owner, server.URL)
	before := postMessage(t, owner, server.URL, "replay-1", "before")
	if _, ok := before["message_id"]; !ok {
		t.Fatalf("legacy Web acknowledgement missing message_id: %#v", before)
	}
	response, err := owner.Get(server.URL + "/v1/conversation?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	var entries []core.ConversationEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(entries) != 1 || entries[0].Seq != 1 {
		t.Fatalf("replay=%#v", entries)
	}
	response, err = owner.Get(server.URL + "/v1/conversation/ws?after_seq=bad")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("ws replay alias status=%d", response.StatusCode)
	}
	response.Body.Close()
}

type jsonResponse struct {
	status int
	body   map[string]any
}

func postJSON(t *testing.T, client *http.Client, endpoint, payload string) jsonResponse {
	return requestJSON(t, client, http.MethodPost, endpoint, payload)
}

func requestJSON(t *testing.T, client *http.Client, method, endpoint, payload string) jsonResponse {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&body)
	return jsonResponse{status: response.StatusCode, body: body}
}

func getJSON(t *testing.T, client *http.Client, endpoint string) jsonResponse {
	t.Helper()
	response, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&body)
	return jsonResponse{status: response.StatusCode, body: body}
}
