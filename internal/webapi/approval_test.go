package webapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestApprovalAPIRejectsWrongClientAuthentication(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "approval-auth.db"))
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
	response, err := server.Client().Get(server.URL + "/v1/approvals")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong client status=%d", response.StatusCode)
	}
	response.Body.Close()
}
