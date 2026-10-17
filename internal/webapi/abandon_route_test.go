package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func TestOwnerAbandonRequiresExplicitAttemptAndWarningConsent(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "abandon.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil { t.Fatal(err) }
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "abandon-web-worker", Intent: "possibly performed external side effect",
		ProjectID: "project", NodeID: "unreachable", HarnessInstanceID: "unreachable/fx",
	}, core.TurnSpec{Input: "perform external action"})
	if err != nil { t.Fatal(err) }
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
	if err := store.RecoverPhase4Attempts(ctx, core.Phase4AttemptRecoveryFunc(
		func(context.Context, core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
			return core.Phase4RecoveryAwaiting, nil
		})); err != nil { t.Fatal(err) }
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	api.AttachWorkerResponder(ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	post := func(payload any) int {
		t.Helper()
		encoded, err := json.Marshal(payload)
		if err != nil { t.Fatal(err) }
		response, err := client.Post(server.URL+"/v1/workers/"+worker.WorkerRef+"/abandon",
			"application/json", bytes.NewReader(encoded))
		if err != nil { t.Fatal(err) }
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := post(map[string]any{"attempt_id": attempt.ID, "idempotency_key": "without-confirmation"}); status != http.StatusBadRequest {
		t.Fatalf("silent abandon was accepted: %d", status)
	}
	request := map[string]any{"attempt_id": attempt.ID, "confirm_unknown_execution": true, "idempotency_key": "confirmed"}
	if status := post(request); status != http.StatusAccepted { t.Fatalf("owner abandon rejected: %d", status) }
	if status := post(request); status != http.StatusAccepted { t.Fatalf("idempotent replay rejected: %d", status) }
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil { t.Fatal(err) }
	if details.CurrentAttempt().State != core.AttemptInterrupted || len(details.Results) != 1 {
		t.Fatalf("owner abandon did not persist exactly one result: %#v", details)
	}
}
