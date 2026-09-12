package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/webapi"
)

func TestProductionAssemblyRespondsWithoutManualResponderAttachment(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "production.db"))
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
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "production-worker", Title: "Production worker", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", Intent: "inspect", PolicySnapshot: "{}"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	local := node.NewLocal(&productionRuntime{})
	if _, err := local.Dispatch(ctx, node.StartRequest{WorkerRef: worker.WorkerRef}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, core.Activity{Metadata: core.ActivityMetadata{EventID: "production-approval", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "production-request", Summary: "write file"}}); err != nil {
		t.Fatal(err)
	}
	api, err := webapi.New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	// This is the production assembly seam. The test must not attach a responder itself.
	attachProductionWorkerServices(api, store, person.ID, capability, local, nil, nil)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustProductionCookieJar(t)}
	loginProduction(t, client, server.URL)
	response, err := client.Post(server.URL+"/v1/approvals/production-request/approve", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("approval status=%d", response.StatusCode)
	}
	approval, err := store.Approval(ctx, "production-request")
	if err != nil || approval.State != core.ApprovalApproved {
		t.Fatalf("approval=%#v err=%v", approval, err)
	}
}

type productionRuntime struct{}

func (*productionRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	return &productionSession{}, nil
}

type productionSession struct{}

func (*productionSession) ID() string                                    { return "production-native" }
func (*productionSession) Prompt(context.Context, string) error          { return nil }
func (*productionSession) Steer(context.Context, string) (bool, error)   { return true, nil }
func (*productionSession) Cancel(context.Context) error                  { return nil }
func (*productionSession) Activity() <-chan node.Activity                { return nil }
func (*productionSession) Result() <-chan node.Result                    { return nil }
func (*productionSession) Close() error                                  { return nil }
func (*productionSession) Respond(context.Context, string, string) error { return nil }

func mustProductionCookieJar(t *testing.T) *cookiejar.Jar {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return jar
}

func loginProduction(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	response, err := client.Post(baseURL+"/v1/web/session", "application/json", strings.NewReader(`{"bootstrap_token":"bootstrap"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login status=%d", response.StatusCode)
	}
}
