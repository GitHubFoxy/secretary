package webapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestPublicWorkerFailureCategoryIsAllowlisted(t *testing.T) {
	details := core.WorkerDetails{
		Worker: core.Worker{ID: "worker-id", WorkerRef: "worker-ref"},
		Results: []core.Result{{
			ID: "result-id", Status: core.ResultFailed,
			Summary:         "Bearer private-summary-secret",
			FailureCategory: "sk-private-secret", FailureCode: "sk-private-secret",
			Failure: &core.WorkerFailure{
				Category: "sk-private-secret", ProviderCode: "sk-private-secret",
				Message:   `{"password":"private-value"}`,
				Technical: `{"access_token":"opaque-value","session_id":"native-value"}`,
			},
		}},
	}
	encoded, err := json.Marshal(publicWorkerDTOFromDetails(details))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	result := payload["result"].(map[string]any)
	if result["failure_category"] != string(core.FailureCategoryInternalError) {
		t.Fatalf("unsafe public failure category: %s", encoded)
	}
	for _, secret := range []string{"private-summary-secret", "sk-private-secret", "private-value", "opaque-value", "native-value"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public Worker JSON leaked failure metadata: %s", encoded)
		}
	}
}
