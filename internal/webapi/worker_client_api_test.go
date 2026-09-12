package webapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestWorkerFirstClientSurfaceHasNoTaskOrNativeSessionFields(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "worker-client.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "inspect", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)

	for _, endpoint := range []string{"/v1/workers", "/v1/workers/" + worker.WorkerRef, "/v1/workers/" + worker.WorkerRef + "/turns"} {
		response, err := client.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("endpoint=%s status=%d body=%s", endpoint, response.StatusCode, body)
		}
		if strings.Contains(string(body), "task_id") || strings.Contains(string(body), "runtime_session_id") || strings.Contains(string(body), attempt.ID+"-native") {
			t.Fatalf("endpoint=%s leaked legacy/native fields: %s", endpoint, body)
		}
	}
	_ = turn
	var details core.WorkerDetails
	response, err := client.Get(server.URL + "/v1/workers/" + worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(response.Body).Decode(&details); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if details.Worker.WorkerRef != worker.WorkerRef || len(details.Turns) != 1 || len(details.Attempts) != 1 {
		t.Fatalf("details=%#v", details)
	}
}
