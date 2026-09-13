package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteSecretaryCallUsesCapabilityAndDecodesValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer capability" {
			t.Fatalf("authorization=%q", got)
		}
		var request struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Name != "list_workers" || string(request.Arguments) != `{}` {
			t.Fatalf("request=%+v arguments=%s", request, request.Arguments)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]string{{"worker_ref": "worker-1"}}})
	}))
	defer server.Close()

	value, err := (RemoteSecretary{BaseURL: server.URL, Capability: "capability", Client: server.Client()}).Call(context.Background(), "list_workers", nil)
	if err != nil {
		t.Fatal(err)
	}
	workers, ok := value.([]any)
	if !ok || len(workers) != 1 || workers[0].(map[string]any)["worker_ref"] != "worker-1" {
		t.Fatalf("value=%#v", value)
	}
}

func TestRemoteSecretaryCallPropagatesToolErrorWithoutLeakingCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "worker unavailable"})
	}))
	defer server.Close()

	_, err := (RemoteSecretary{BaseURL: server.URL, Capability: "private-capability", Client: server.Client()}).Call(context.Background(), "spawn_worker", json.RawMessage(`{"intent":"inspect"}`))
	if err == nil || !strings.Contains(err.Error(), "worker unavailable") || strings.Contains(err.Error(), "private-capability") {
		t.Fatalf("err=%v", err)
	}
}
