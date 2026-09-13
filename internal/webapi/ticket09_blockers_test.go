package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestTicket09SanitizerRemovesGenericSessionIdentifiersRecursively(t *testing.T) {
	value := map[string]any{
		"product": "keep",
		"nested": map[string]any{
			"session":            "generic-session",
			"session_id":         "snake-session",
			"sessionId":          "camel-session",
			"SESSION_ID":         "upper-session",
			"session_identifier": "snake-identifier",
			"sessionIdentifier":  "camel-identifier",
			"SESSION-IDENTIFIER": "upper-identifier",
			"product_reference":  "allowed-product-ref",
			"product_field":      "keep-nested",
			"items": []any{map[string]any{
				"SESSION":            "nested-session",
				"Session_Id":         "nested-snake-session",
				"SessionId":          "nested-camel-session",
				"Session_Identifier": "nested-identifier",
				"title":              "keep-item",
			}},
		},
	}

	encoded, err := json.Marshal(sanitizePublicJSON(value))
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, leaked := range []string{
		"generic-session", "snake-session", "camel-session", "upper-session", "snake-identifier",
		"camel-identifier", "upper-identifier", "nested-session", "nested-snake-session",
		"nested-camel-session", "nested-identifier",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("generic session identifier leaked: %q in %s", leaked, body)
		}
	}
	for _, kept := range []string{"keep", "allowed-product-ref", "keep-nested", "keep-item"} {
		if !strings.Contains(body, kept) {
			t.Fatalf("allowed product value removed: %q in %s", kept, body)
		}
	}
}

func TestTicket09ClientMutationsRequireNonEmptyIdempotencyKeyAndExactReplay(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "blockers.db"))
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
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar}
	login(t, owner, server.URL)

	missing := requestJSONWithoutIdempotency(t, owner, http.MethodPost, server.URL+"/v1/messages", `{"external_message_id":"missing-key","body":"hello"}`)
	if missing.status != http.StatusBadRequest {
		t.Fatalf("missing idempotency key accepted: status=%d body=%#v", missing.status, missing.body)
	}
	for _, mutation := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"missing-pair","display_name":"Missing","platform":"test"}`},
		{http.MethodPut, "/v1/user", `{"content":"missing-key"}`},
		{http.MethodPost, "/v1/projects", `{"name":"Missing project"}`},
	} {
		response := requestJSONWithoutIdempotency(t, owner, mutation.method, server.URL+mutation.path, mutation.body)
		if response.status != http.StatusBadRequest {
			t.Fatalf("%s %s accepted without key: status=%d body=%#v", mutation.method, mutation.path, response.status, response.body)
		}
	}

	first := requestWithHeaderJSON(t, owner, http.MethodPost, server.URL+"/v1/messages", `{"external_message_id":"exact-replay","body":"hello"}`, "message-key")
	if first.status != http.StatusAccepted {
		t.Fatalf("first mutation status=%d body=%#v", first.status, first.body)
	}
	second := requestWithHeaderJSON(t, owner, http.MethodPost, server.URL+"/v1/messages", `{"external_message_id":"exact-replay","body":"hello"}`, "message-key")
	if second.status != http.StatusAccepted || second.body["message_id"] != first.body["message_id"] || second.body["entry_seq"] != first.body["entry_seq"] {
		t.Fatalf("exact replay changed outcome: first=%#v second=%#v", first.body, second.body)
	}
	conflict := requestWithHeaderJSON(t, owner, http.MethodPost, server.URL+"/v1/messages", `{"external_message_id":"different","body":"hello"}`, "message-key")
	if conflict.status != http.StatusConflict {
		t.Fatalf("different payload reused key: status=%d body=%#v", conflict.status, conflict.body)
	}

	const callers = 8
	results := make(chan jsonResponse, callers)
	var group sync.WaitGroup
	for i := 0; i < callers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- requestWithHeaderJSON(t, owner, http.MethodPost, server.URL+"/v1/messages", `{"external_message_id":"concurrent","body":"one effect"}`, "concurrent-key")
		}()
	}
	group.Wait()
	close(results)
	var expected jsonResponse
	for result := range results {
		if result.status != http.StatusAccepted {
			t.Fatalf("concurrent mutation status=%d body=%#v", result.status, result.body)
		}
		if expected.body == nil {
			expected = result
			continue
		}
		if result.body["message_id"] != expected.body["message_id"] || result.body["entry_seq"] != expected.body["entry_seq"] {
			t.Fatalf("concurrent outcomes differ: first=%#v current=%#v", expected.body, result.body)
		}
	}
}

func requestJSONWithoutIdempotency(t *testing.T, client *http.Client, method, endpoint, payload string) jsonResponse {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, strings.NewReader(payload))
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

func requestWithHeaderJSON(t *testing.T, client *http.Client, method, endpoint, payload, key string) jsonResponse {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&body)
	return jsonResponse{status: response.StatusCode, body: body}
}
