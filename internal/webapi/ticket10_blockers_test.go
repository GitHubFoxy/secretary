package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

type ticket10HostileWorkerActions struct {
	details core.WorkerDetails
	calls   map[string]int
}

func (a *ticket10HostileWorkerActions) MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	a.calls["message"]++
	return a.details, nil
}

func (a *ticket10HostileWorkerActions) RespondWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	a.calls["message"]++
	return a.details, nil
}

func (a *ticket10HostileWorkerActions) CancelWorker(context.Context, string) (core.WorkerDetails, error) {
	a.calls["cancel"]++
	return a.details, nil
}

func (a *ticket10HostileWorkerActions) CloseWorker(context.Context, string) (core.WorkerDetails, error) {
	a.calls["close"]++
	return a.details, nil
}

func TestTicket10WorkerActionResponsesSanitizeFreshAndIdempotencyReplay(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "ticket10-actions.db"))
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
	hostile := core.WorkerDetails{
		Worker: core.Worker{
			ID: worker.ID, WorkerRef: worker.WorkerRef, Intent: worker.Intent, ProjectID: worker.ProjectID, NodeID: worker.NodeID,
			HarnessInstanceID: worker.HarnessInstanceID, PolicySnapshot: `{"policy":"private","credentials":"credential-secret","callback":"callback-secret","session_id":"native-session","thought":"raw thought"}`,
			Status: worker.Status,
		},
		Turns:    []core.Turn{{ID: turn.ID, WorkerID: worker.ID, Input: turn.Input, ContextSnapshot: `{"session_id":"context-session","access_token":"context-token","reasoning":"private reasoning","safe":"kept"}`, State: turn.State, CurrentAttemptID: attempt.ID}},
		Attempts: []core.Phase4Attempt{{ID: attempt.ID, WorkerID: worker.ID, TurnID: turn.ID, NodeID: "node", HarnessInstanceID: "node/fx", State: attempt.State}},
		Outcomes: []core.AttemptOutcome{{ID: "outcome-1", AttemptID: attempt.ID, Diagnostics: `internal reasoning: do not expose`, ErrorMessage: `callback=callback-secret`}},
		Results:  []core.Phase4Result{{ID: "result-1", WorkerID: worker.ID, TurnID: turn.ID, AttemptID: attempt.ID, Summary: "safe result", ArtifactRefs: `{"credential":"artifact-secret","safe":"artifact-kept"}`}},
	}
	actions := &ticket10HostileWorkerActions{details: hostile, calls: make(map[string]int)}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachWorkerResponder(actions)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)

	expected, err := json.Marshal(sanitizePublicJSON(hostile))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		action string
		body   string
	}{
		{action: "message", body: `{"text":"steer","idempotency_key":"%s"}`},
		{action: "follow-up", body: `{"text":"follow up","idempotency_key":"%s"}`},
		{action: "approve", body: `{"request_id":"approval-1","text":"approve","idempotency_key":"%s"}`},
		{action: "cancel", body: `{"idempotency_key":"%s"}`},
		{action: "close", body: `{"idempotency_key":"%s"}`},
	} {
		key := "ticket10-" + item.action
		requestBody := fmt.Sprintf(item.body, key)
		commonPath := item.action == "message" || item.action == "follow-up" || item.action == "approve"
		beforeCalls := actions.calls[item.action]
		if commonPath {
			beforeCalls = actions.calls["message"]
		}
		for replay := 0; replay < 2; replay++ {
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/workers/"+worker.WorkerRef+"/"+item.action, bytes.NewBufferString(requestBody))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("action=%s replay=%d status=%d body=%s", item.action, replay, response.StatusCode, body)
			}
			if string(body) != string(expected)+"\n" {
				t.Fatalf("action=%s replay=%d returned unsanitized or changed DTO: %s", item.action, replay, body)
			}
		}
		calls := actions.calls[item.action]
		if commonPath {
			calls = actions.calls["message"]
		}
		if calls != beforeCalls+1 {
			t.Fatalf("action=%s fresh/replay calls=%d want=%d", item.action, calls, beforeCalls+1)
		}
	}
	for _, forbidden := range []string{"credential-secret", "callback-secret", "native-session", "context-session", "context-token", "private reasoning", "artifact-secret"} {
		if strings.Contains(string(expected), forbidden) {
			t.Fatalf("test fixture was not hostile enough, sanitizer retained %q: %s", forbidden, expected)
		}
	}
}
