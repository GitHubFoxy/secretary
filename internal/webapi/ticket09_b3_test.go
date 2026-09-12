package webapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

type ticket09WorkerActions struct {
	details core.WorkerDetails
	request ctl.MessageWorkerRequest
}

func (a *ticket09WorkerActions) MessageWorker(_ context.Context, request ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	a.request = request
	return a.details, nil
}
func (a *ticket09WorkerActions) RespondWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	return a.details, nil
}
func (a *ticket09WorkerActions) CancelWorker(context.Context, string) (core.WorkerDetails, error) {
	return a.details, nil
}
func (a *ticket09WorkerActions) CloseWorker(context.Context, string) (core.WorkerDetails, error) {
	return a.details, nil
}

func TestTicket09GenericWorkerMessageUsesAuthenticatedBearerClient(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "worker-spoof.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, _, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "needs input", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "question"})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, person.ID, "spoof-device", "Spoof", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	client, credential, err := store.ApproveClient(ctx, pairing.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := &ticket09WorkerActions{}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	actions.details.Worker = worker
	api.AttachWorkerResponder(actions)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	post := func(key string, spoof string) *http.Response {
		t.Helper()
		payload := bytes.NewBufferString(`{"text":"answer","request_id":"input-request","idempotency_key":"` + key + `"}`)
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/workers/"+worker.WorkerRef+"/message", payload)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+credential)
		if spoof != "" {
			request.Header.Set("X-Client-ID", spoof)
		}
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := post("authenticated-input", "")
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("worker message status=%d", response.StatusCode)
	}
	if actions.request.ClientID != client.ID {
		t.Fatalf("worker request client_id=%q want authenticated %q", actions.request.ClientID, client.ID)
	}
	if response = post("spoofed-input", "attacker-client"); response.StatusCode != http.StatusBadRequest {
		response.Body.Close()
		t.Fatalf("spoofed worker message status=%d want=%d", response.StatusCode, http.StatusBadRequest)
	} else {
		response.Body.Close()
	}
}
