package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestProjectsCRUDAPIIsDurableAndRequiresRevision(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "projects.db"))
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
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login(t, client, server.URL)
	payload := `{"id":"p1","name":"Project","description":"desc","mappings":[{"node":"macbook","path":"/tmp/project"}],"policy":{"default_node":"macbook","allowed_harness_kinds":["fx"]}}`
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/projects", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "create-p1")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create status=%d", response.StatusCode)
	}
	var project core.Project
	if err := json.NewDecoder(response.Body).Decode(&project); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	get, err := client.Get(server.URL + "/v1/projects/p1")
	if err != nil {
		t.Fatal(err)
	}
	if get.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", get.StatusCode)
	}
	get.Body.Close()
	update := `{"name":"Project 2","description":"desc","mappings":[{"node":"macbook","path":"/tmp/project"}],"policy":{"default_node":"macbook"},"expected_revision":1,"idempotency_key":"update-p1"}`
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/projects/p1", bytes.NewBufferString(update))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("update status=%d", response.StatusCode)
	}
	response.Body.Close()
	stale := `{"name":"stale","mappings":[{"node":"macbook","path":"/tmp/project"}],"expected_revision":1,"idempotency_key":"stale-p1"}`
	request, _ = http.NewRequest(http.MethodPut, server.URL+"/v1/projects/p1", bytes.NewBufferString(stale))
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale status=%d", response.StatusCode)
	}
	response.Body.Close()
	request, _ = http.NewRequest(http.MethodDelete, server.URL+"/v1/projects/p1?expected_revision=2", nil)
	request.Header.Set("Idempotency-Key", "delete-p1")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status=%d", response.StatusCode)
	}
	response.Body.Close()
}
