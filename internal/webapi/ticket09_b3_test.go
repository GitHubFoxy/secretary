package webapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestWorkerActionAPIExposesMessageFollowUpAndApprove(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "worker-actions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, _, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "work", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "work"})
	if err != nil {
		t.Fatal(err)
	}
	actions := &ticket09WorkerActions{details: core.WorkerDetails{Worker: worker}}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(actions)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: jar}
	login(t, owner, server.URL)
	for _, item := range []struct {
		action string
		body   string
	}{
		{action: "message", body: `{"text":"steer","idempotency_key":"action-message"}`},
		{action: "follow-up", body: `{"text":"follow up","idempotency_key":"action-follow-up"}`},
		{action: "approve", body: `{"request_id":"request-1","text":"approve","idempotency_key":"action-approve"}`},
	} {
		response, err := owner.Post(server.URL+"/v1/workers/"+worker.WorkerRef+"/"+item.action, "application/json", strings.NewReader(item.body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("action=%s status=%d", item.action, response.StatusCode)
		}
		if actions.request.Text == "" {
			t.Fatalf("action=%s did not reach common API", item.action)
		}
	}
}

func TestWorkerMessageScopeOnlyAllowsMessageRoute(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "worker-message-scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, _, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "work", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "work"})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, person.ID, "message-only", "Message only", "test", []core.ClientScope{core.ScopeWorkerMessage})
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := store.ApproveClient(ctx, pairing.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := &ticket09WorkerActions{details: core.WorkerDetails{Worker: worker}}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(actions)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	for _, item := range []struct {
		path, body string
		want       int
	}{
		{"message", `{"text":"hello","idempotency_key":"scope-message"}`, http.StatusAccepted},
		{"stop", `{}`, http.StatusForbidden}, {"cancel", `{}`, http.StatusForbidden},
		{"steer", `{"text":"x"}`, http.StatusForbidden}, {"respond", `{"request_id":"r","response":"x"}`, http.StatusForbidden},
		{"approve", `{"request_id":"r","text":"approve"}`, http.StatusForbidden},
		{"close", `{}`, http.StatusForbidden}, {"follow-up", `{"text":"x"}`, http.StatusForbidden},
	} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/workers/"+worker.WorkerRef+"/"+item.path, strings.NewReader(item.body))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != item.want {
			t.Errorf("%s status=%d want=%d", item.path, res.StatusCode, item.want)
		}
	}
}

func TestConversationWriteScopeOnlyAllowsMessageEndpoint(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "conversation-write-scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, _, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "work", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "work"})
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, person.ID, "conversation-write", "Conversation writer", "test", []core.ClientScope{core.ScopeConversationWrite})
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := store.ApproveClient(ctx, pairing.ID)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	for _, item := range []struct {
		path, body string
		want       int
	}{
		{"messages", `{"external_message_id":"m1","body":"hello","idempotency_key":"message-write"}`, http.StatusAccepted},
		{"workers/" + worker.WorkerRef + "/message", `{"text":"x","idempotency_key":"worker-write"}`, http.StatusForbidden},
		{"workers/" + worker.WorkerRef + "/cancel", `{}`, http.StatusForbidden},
		{"workers/" + worker.WorkerRef + "/close", `{}`, http.StatusForbidden},
		{"workers/" + worker.WorkerRef + "/approve", `{"request_id":"r","text":"approve"}`, http.StatusForbidden},
	} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/"+item.path, strings.NewReader(item.body))
		req.Header.Set("Authorization", "Bearer "+credential)
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != item.want {
			t.Errorf("%s status=%d want=%d", item.path, res.StatusCode, item.want)
		}
	}
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
