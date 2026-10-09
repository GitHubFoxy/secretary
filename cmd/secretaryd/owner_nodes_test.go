package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/webapi"
)

func TestRootHandlerOwnerNodeAccess(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := webapi.New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{PairingTokens: []string{"pair"}, AdminToken: "node-admin"})
	if err != nil {
		t.Fatal(err)
	}
	api.AttachNodeService(manager)
	server := httptest.NewServer(rootHandler(api.Handler(), http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), manager, false))
	defer server.Close()
	owner := &http.Client{Jar: mustProductionCookieJar(t)}
	loginProduction(t, owner, server.URL)
	for _, test := range []struct {
		name   string
		client *http.Client
		method string
		path   string
		bearer string
		status int
	}{
		{"owner read", owner, http.MethodGet, "/v1/nodes", "", http.StatusOK},
		{"owner admin denied", owner, http.MethodPost, "/v1/nodes/pairing-tokens", "", http.StatusUnauthorized},
		{"anonymous read denied", server.Client(), http.MethodGet, "/v1/nodes", "", http.StatusUnauthorized},
		{"invalid cookie denied", &http.Client{}, http.MethodGet, "/v1/nodes", "", http.StatusUnauthorized},
		{"admin read", server.Client(), http.MethodGet, "/v1/nodes", "node-admin", http.StatusOK},
		{"invalid admin denied", server.Client(), http.MethodGet, "/v1/nodes", "invalid-admin", http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, server.URL+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.bearer != "" {
				request.Header.Set("Authorization", "Bearer "+test.bearer)
			}
			if test.name == "invalid cookie denied" {
				request.AddCookie(&http.Cookie{Name: "secretary_session", Value: "invalid"})
			}
			response, err := test.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, test.status)
			}
		})
	}
}
