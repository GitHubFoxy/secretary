package webapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestTicket09WorkerResponseRejectsSpoofAndUsesBearerIdentity(t *testing.T) {
	for _, test := range []struct {
		name      string
		kind      core.ActivityKind
		requestID string
		text      string
	}{
		{name: "approval", kind: core.ActivityPermissionRequest, requestID: "spoof-approval", text: "approve"},
		{name: "needs_input", kind: core.ActivityUserInputRequest, requestID: "spoof-input", text: "answer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ticket09WorkerResponseIdentityRegression(t, test.kind, test.requestID, test.text)
		})
	}
}

func ticket09WorkerResponseIdentityRegression(t *testing.T, kind core.ActivityKind, requestID, text string) {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "response-identity.db"))
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
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "wait", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "wait"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{EventID: requestID + "-event", Node: "node", HarnessInstanceID: "node/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: kind, Request: &core.ActivityRequest{RequestID: requestID, Summary: "answer required"}}); err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, person.ID, "response-identity", "Response", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	client, credential, err := store.ApproveClient(ctx, pairing.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: &apiApprovalRuntime{}}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(service)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	post := func(key, spoof string) *http.Response {
		t.Helper()
		body := bytes.NewBufferString(`{"text":"` + text + `","request_id":"` + requestID + `","idempotency_key":"` + key + `"}`)
		request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/workers/"+worker.WorkerRef+"/message", body)
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
	if response := post("spoofed-"+requestID, "attacker"); response.StatusCode != http.StatusBadRequest {
		response.Body.Close()
		t.Fatalf("spoofed response status=%d", response.StatusCode)
	} else {
		response.Body.Close()
	}
	if response := post("real-"+requestID, ""); response.StatusCode != http.StatusAccepted {
		response.Body.Close()
		t.Fatalf("authenticated response status=%d", response.StatusCode)
	} else {
		response.Body.Close()
	}
	approval, err := store.Approval(ctx, requestID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.ResolvedBy != client.ID {
		t.Fatalf("resolved_by=%q want authenticated client %q", approval.ResolvedBy, client.ID)
	}
	if approval.ResolvedBy == "attacker" || approval.ResolvedBy == "client" || approval.ResolvedBy == "" {
		t.Fatalf("resolved_by used spoofable/fallback identity: %q", approval.ResolvedBy)
	}
}
