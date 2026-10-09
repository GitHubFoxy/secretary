package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestAuthenticatedWorkerQueueAcceptanceAndReplay(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "wait", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "wait"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	var firstID string
	for i := 0; i < 2; i++ {
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/workers/"+worker.WorkerRef+"/message", bytes.NewBufferString(`{"text":"/q after terminal","idempotency_key":"same-input"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var details core.WorkerDetails
		err = json.NewDecoder(response.Body).Decode(&details)
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted || err != nil {
			t.Fatalf("status=%d err=%v", response.StatusCode, err)
		}
		if details.ActionMode != "queued" || len(details.QueuedMessages) != 1 || len(details.Attempts) != 1 {
			t.Fatalf("details=%#v", details)
		}
		if i == 0 {
			firstID = details.QueuedMessages[0].ID
		} else if firstID != details.QueuedMessages[0].ID {
			t.Fatal("HTTP replay duplicated queue")
		}
	}
	response, err := client.Get(server.URL + "/v1/workers/" + worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot core.WorkerDetails
	err = json.NewDecoder(response.Body).Decode(&snapshot)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || err != nil || len(snapshot.QueuedMessages) != 1 {
		t.Fatalf("snapshot=%#v status=%d err=%v", snapshot, response.StatusCode, err)
	}
}
