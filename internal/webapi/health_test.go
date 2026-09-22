package webapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHealthIsBoundedAndNeedsNoCredential(t *testing.T) {
	server, _ := testServer(t)
	response, err := server.Client().Get(server.URL + "/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status=%d", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("health body=%#v, want exactly {status: ok}", body)
	}
}

func TestHealthRejectsNonGET(t *testing.T) {
	server, _ := testServer(t)
	response, err := server.Client().Post(server.URL+"/v1/health", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 400 {
		t.Fatalf("POST /v1/health status=%d, want refusal", response.StatusCode)
	}
}
