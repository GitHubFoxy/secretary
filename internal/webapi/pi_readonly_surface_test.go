package webapi

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
