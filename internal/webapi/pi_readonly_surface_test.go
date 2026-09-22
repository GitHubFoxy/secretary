package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// pairedReadonlyCredential creates the Pi viewer credential shape: a Client
// approved with exactly the three viewer scopes from docs/pi-viewer.md.
func pairedReadonlyCredential(t *testing.T, server *httptest.Server) string {
	t.Helper()
	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair",
		`{"bootstrap_token":"bootstrap","device_id":"pi-negative","display_name":"Pi","platform":"pi","scopes":["conversation:read","worker:read","approval:read"]}`)
	if pair.status != http.StatusCreated || pair.body["client_id"] == nil {
		t.Fatalf("pair=%d %#v", pair.status, pair.body)
	}
	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+pair.body["client_id"].(string)+"/approve", `{}`)
	if approve.status != http.StatusOK || approve.body["credential"] == nil {
		t.Fatalf("approve=%d %#v", approve.status, approve.body)
	}
	return approve.body["credential"].(string)
}

func TestReadOnlyCredentialIsRefusedOutsideTheReadSurface(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "pi-negative.db"))
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
	credential := pairedReadonlyCredential(t, server)

	client := &http.Client{}

	// Positive control: the same credential reads the snapshot surface.
	read, err := http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	if err != nil {
		t.Fatal(err)
	}
	read.Header.Set("Authorization", "Bearer "+credential)
	readResponse, err := client.Do(read)
	if err != nil {
		t.Fatal(err)
	}
	readResponse.Body.Close()
	if readResponse.StatusCode != http.StatusOK {
		t.Fatalf("read surface status=%d, want 200", readResponse.StatusCode)
	}

	denied := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/workers/worker-1/message"},
		{http.MethodPost, "/v1/approvals/request-1/approve"},
		{http.MethodPut, "/v1/user"},
		{http.MethodPost, "/v1/projects"},
		{http.MethodPost, "/v1/secretary/model"},
		{http.MethodPost, "/v1/telegram/pairing"},
		{http.MethodPost, "/v1/internal/secretary/tools/call"},
		{http.MethodPost, "/v1/clients/pair"},
		{http.MethodPost, "/v1/clients/other-client/approve"},
		{http.MethodGet, "/v1/clients"},
		{http.MethodGet, "/v1/control/overview"},
		{http.MethodPost, "/v1/control/config"},
		{http.MethodGet, "/v1/nodes/connect"},
	}
	for _, route := range denied {
		request, err := http.NewRequest(route.method, server.URL+route.path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", route.method, route.path, err)
		}
		response.Body.Close()
		if response.StatusCode < 400 {
			t.Fatalf("%s %s status=%d with read-only credential, want refusal", route.method, route.path, response.StatusCode)
		}
	}
}

func TestReadOnlyCredentialGrantsExactlyViewerScopes(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "pi-scopes.db"))
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
	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair",
		`{"bootstrap_token":"bootstrap","device_id":"pi-scopes","display_name":"Pi","platform":"pi","scopes":["conversation:read","worker:read","approval:read"]}`)
	if pair.status != http.StatusCreated || pair.body["client_id"] == nil {
		t.Fatalf("pair=%d %#v", pair.status, pair.body)
	}
	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+pair.body["client_id"].(string)+"/approve", `{}`)
	if approve.status != http.StatusOK || approve.body["credential"] == nil {
		t.Fatalf("approve=%d %#v", approve.status, approve.body)
	}
	granted, ok := approve.body["scopes"].([]any)
	if !ok {
		t.Fatalf("approve scopes=%#v, want explicit list", approve.body["scopes"])
	}
	want := map[string]bool{"conversation:read": true, "worker:read": true, "approval:read": true}
	if len(granted) != len(want) {
		t.Fatalf("granted scopes=%v, want exactly %v", granted, want)
	}
	for _, scope := range granted {
		name, _ := scope.(string)
		if !want[name] {
			t.Fatalf("granted scopes=%v, want exactly %v", granted, want)
		}
	}
}

func piSnapshotServer(t *testing.T, ctx context.Context) (*core.Store, core.Conversation, *httptest.Server, string) {
	t.Helper()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "pi-snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, conversation, err := store.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)
	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair",
		`{"bootstrap_token":"bootstrap","device_id":"pi-snapshot","display_name":"Pi","platform":"pi","scopes":["conversation:read","worker:read","approval:read"],"idempotency_key":"pi-snapshot"}`)
	if pair.status != http.StatusCreated {
		t.Fatalf("pair=%d %#v", pair.status, pair.body)
	}
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+pair.body["client_id"].(string)+"/approve", `{}`)
	if approve.status != http.StatusOK || approve.body["credential"] == nil {
		t.Fatalf("approve=%d %#v", approve.status, approve.body)
	}
	return store, conversation, server, approve.body["credential"].(string)
}

func getConversationPage(t *testing.T, server *httptest.Server, credential, query string) (int, conversationPage) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/conversation"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var page conversationPage
	if response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode, page
}

func TestConversationSnapshotPagination(t *testing.T) {
	ctx := context.Background()
	store, conversation, server, credential := piSnapshotServer(t, ctx)
	for i := 1; i <= 5; i++ {
		if _, err := store.AppendEntry(ctx, conversation.ID, core.EntryUser, fmt.Sprintf("message %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if status, page := getConversationPage(t, server, credential, "?limit=2"); status != http.StatusOK {
		t.Fatalf("tail status=%d", status)
	} else {
		if len(page.Entries) != 2 || page.Entries[0].Seq != 4 || page.Entries[1].Seq != 5 {
			t.Fatalf("tail entries=%+v, want seq 4,5 ascending", page.Entries)
		}
		if page.NextAfterSeq != nil {
			t.Fatalf("tail next_after_seq=%v, want null", *page.NextAfterSeq)
		}
		if page.NextBeforeSeq == nil || *page.NextBeforeSeq != 4 {
			t.Fatalf("tail next_before_seq=%v, want 4", page.NextBeforeSeq)
		}
		if status, prev := getConversationPage(t, server, credential, fmt.Sprintf("?before_seq=%d&limit=2", *page.NextBeforeSeq)); status != http.StatusOK {
			t.Fatalf("prev status=%d", status)
		} else if len(prev.Entries) != 2 || prev.Entries[0].Seq != 2 || prev.Entries[1].Seq != 3 {
			t.Fatalf("prev entries=%+v, want seq 2,3 ascending", prev.Entries)
		}
	}
	if status, page := getConversationPage(t, server, credential, "?after_seq=2&limit=2"); status != http.StatusOK {
		t.Fatalf("forward status=%d", status)
	} else {
		if len(page.Entries) != 2 || page.Entries[0].Seq != 3 || page.Entries[1].Seq != 4 {
			t.Fatalf("forward entries=%+v, want seq 3,4 ascending", page.Entries)
		}
		if page.NextBeforeSeq != nil {
			t.Fatalf("forward next_before_seq=%v, want null", *page.NextBeforeSeq)
		}
		if page.NextAfterSeq == nil || *page.NextAfterSeq != 4 {
			t.Fatalf("forward next_after_seq=%v, want 4", page.NextAfterSeq)
		}
	}
	if status, page := getConversationPage(t, server, credential, "?after_seq=5&limit=2"); status != http.StatusOK {
		t.Fatalf("empty status=%d", status)
	} else if len(page.Entries) != 0 || page.NextBeforeSeq != nil || page.NextAfterSeq != nil {
		t.Fatalf("empty page=%+v, want no entries and null cursors", page)
	}
	for _, query := range []string{"?limit=0", "?limit=-5", "?limit=abc", "?before_seq=abc", "?before_seq=0", "?before_seq=-3", "?after_seq=0&before_seq=5"} {
		if status, _ := getConversationPage(t, server, credential, query); status != http.StatusBadRequest {
			t.Fatalf("query %q status=%d, want 400", query, status)
		}
	}
}

func TestConversationSnapshotLimitClampsToMaximum(t *testing.T) {
	ctx := context.Background()
	store, conversation, server, credential := piSnapshotServer(t, ctx)
	for i := 0; i < 505; i++ {
		if _, err := store.AppendEntry(ctx, conversation.ID, core.EntryUser, fmt.Sprintf("bulk %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	status, page := getConversationPage(t, server, credential, "")
	if status != http.StatusOK {
		t.Fatalf("default status=%d", status)
	}
	if len(page.Entries) != 100 {
		t.Fatalf("default entries=%d, want default 100", len(page.Entries))
	}
	if status, page := getConversationPage(t, server, credential, "?limit=10000"); status != http.StatusOK {
		t.Fatalf("clamp status=%d", status)
	} else if len(page.Entries) != 500 {
		t.Fatalf("clamp entries=%d, want maximum 500", len(page.Entries))
	}
}

func TestApprovalListReturnsAllowlistedConversationApprovals(t *testing.T) {
	ctx := context.Background()
	store, conversation, server, credential := piSnapshotServer(t, ctx)
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "approval-worker", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	permission := core.Activity{Metadata: core.ActivityMetadata{EventID: "approval-1", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "approval-request-1", Summary: "run shell"}}
	_ = worker
	if _, err := store.RecordNodeActivityReplay(ctx, permission); err != nil {
		t.Fatal(err)
	}
	otherPerson, otherConversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = otherPerson
	otherWorker, otherTurn, otherAttempt, err := store.CreateWorker(ctx, otherConversation.ID, core.WorkerSpec{WorkerRef: "other-worker", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	otherPermission := core.Activity{Metadata: core.ActivityMetadata{EventID: "approval-2", Node: "local", HarnessInstanceID: "local/fx", WorkerRef: otherWorker.WorkerRef, TurnID: otherTurn.ID, AttemptID: otherAttempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: "approval-request-2", Summary: "run shell"}}
	if _, err := store.RecordNodeActivityReplay(ctx, otherPermission); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/approvals?limit=100", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("approvals status=%d", response.StatusCode)
	}
	var approvals []map[string]any
	if err := json.NewDecoder(response.Body).Decode(&approvals); err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 {
		t.Fatalf("approvals=%v, want exactly the owner conversation approval", approvals)
	}
	allowed := map[string]bool{"id": true, "kind": true, "action_summary": true, "risk_category": true, "state": true, "requested_at": true, "expires_at": true}
	for key := range approvals[0] {
		if !allowed[key] {
			t.Fatalf("approval leaks private field %q: %v", key, approvals[0])
		}
	}
	for _, key := range []string{"id", "kind", "action_summary", "risk_category", "state", "requested_at", "expires_at"} {
		if _, ok := approvals[0][key]; !ok {
			t.Fatalf("approval misses required field %q: %v", key, approvals[0])
		}
	}
}
