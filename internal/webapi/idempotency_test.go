package webapi

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/node"
)

func TestNodeCommandFailureResponseSanitizesMetadata(t *testing.T) {
	response := httptest.NewRecorder()
	err := &node.NodeCommandError{Outcome: node.CommandOutcome{
		ErrorCode:       "sk-private-code",
		ErrorMessage:    `{"password":"private-value"}`,
		FailureCategory: "sk-private-category",
		ProviderCode:    "sk-private-provider",
		Technical:       `{"access_token":"opaque-value","session_id":"native-value"}`,
	}}
	writeClientMutationError(response, err)
	if response.Code != 400 {
		t.Fatalf("status=%d, want 400", response.Code)
	}
	var payload struct {
		Error        string `json:"error"`
		Category     string `json:"category"`
		ProviderCode string `json:"provider_code"`
		Technical    string `json:"technical"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid response JSON: %v (%s)", err, response.Body.String())
	}
	if payload.Category != "internal_error" || payload.ProviderCode != "" {
		t.Fatalf("unsafe typed metadata in response: %#v", payload)
	}
	for _, secret := range []string{"private-value", "opaque-value", "native-value", "sk-private"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("public mutation response leaked %q: %s", secret, response.Body.String())
		}
	}
}

func TestUncertainNodeCommandResponseDoesNotLookLikeDenial(t *testing.T) {
	response := httptest.NewRecorder()
	err := errors.Join(node.ErrCommandOutcomeUnknown, &node.NodeCommandError{Outcome: node.CommandOutcome{
		ErrorCode:       "steering_failed",
		ErrorMessage:    "context deadline exceeded",
		FailureCategory: "internal_error",
	}})
	writeClientMutationError(response, err)
	if response.Code != 503 {
		t.Fatalf("status=%d, want 503 for uncertain outcome", response.Code)
	}
	var payload struct {
		Outcome   string `json:"outcome"`
		Retryable bool   `json:"retryable"`
		Error     string `json:"error"`
		Category  string `json:"category"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid uncertainty response: %v (%s)", err, response.Body.String())
	}
	if payload.Outcome != "uncertain" || !payload.Retryable || payload.Category != "" {
		t.Fatalf("uncertainty was exposed as a typed denial: %#v", payload)
	}
}
