package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

type fakeSecretaryWorkerTools struct{}

func (fakeSecretaryWorkerTools) ListNodes(context.Context) ([]core.NodeRecord, error) {
	return []core.NodeRecord{{Node: "node-a"}}, nil
}
func (fakeSecretaryWorkerTools) ListProjects(context.Context) ([]core.Project, error) {
	return []core.Project{{ID: "project-a", Name: "Project A"}}, nil
}
func (fakeSecretaryWorkerTools) ListWorkers(context.Context) ([]core.Worker, error) {
	return nil, nil
}
func (fakeSecretaryWorkerTools) GetWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) SpawnWorker(context.Context, ctl.SpawnWorkerRequest) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) CancelWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}
func (fakeSecretaryWorkerTools) CloseWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, nil
}

func TestSecretaryToolCallRequiresCapabilityAndUsesAttachedRuntimeService(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(context.Background(), api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	api.AttachSecretaryWorkerTools(fakeSecretaryWorkerTools{})
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	unauthorized, err := http.Post(server.URL+"/v1/internal/secretary/tools/call", "application/json", stringsReader(`{"name":"list_projects","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.StatusCode)
	}
	unauthorized.Body.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/internal/secretary/tools/call", stringsReader(`{"name":"list_projects","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authorized status=%d", response.StatusCode)
	}
	var result struct {
		Value []core.Project `json:"value"`
		Error string         `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Error != "" || len(result.Value) != 1 || result.Value[0].ID != "project-a" {
		t.Fatalf("result=%+v", result)
	}
}

func stringsReader(value string) *strings.Reader { return strings.NewReader(value) }
