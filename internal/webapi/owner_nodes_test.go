package webapi

import (
	"context"
	"encoding/json"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOwnerCookieReadsNodesWithRemoteManagerWithoutAdminAccess(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{PairingTokens: []string{"pair"}, AdminToken: "node-admin"})
	if err != nil {
		t.Fatal(err)
	}
	api.AttachNodeService(manager)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var nodes []publicNode
	if response.StatusCode != http.StatusOK {
		t.Fatalf("owner Node read status=%d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&nodes); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/nodes/pairing-tokens", nil)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("owner cookie acquired Node admin access: %d", response.StatusCode)
	}
	response, err = server.Client().Get(server.URL + "/v1/nodes")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous Node read status=%d", response.StatusCode)
	}
	request, _ = http.NewRequest(http.MethodGet, server.URL+"/v1/nodes", nil)
	request.Header.Set("Authorization", "Bearer node-admin")
	response, err = server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Node admin read regressed: %d", response.StatusCode)
	}
}
