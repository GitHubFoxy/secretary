package webapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestClientActivityDTORedactsRuntimeAndCredentialIdentifiers(t *testing.T) {
	event := core.Event{Payload: json.RawMessage(`{"task_id":"legacy-task","runtime_session_id":"native-session","node_token":"node-secret","credential":"cli-secret","callback":"callback-secret","safe":"kept"}`)}
	encoded, err := json.Marshal(sanitizePublicEvent(event))
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, leaked := range []string{"legacy-task", "native-session", "node-secret", "cli-secret", "callback-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("sensitive activity value leaked: %s", body)
		}
	}
	if !strings.Contains(body, "kept") {
		t.Fatalf("safe activity payload was removed: %s", body)
	}
}

func TestClientPublicSanitizerRedactsNestedForbiddenDTOKeys(t *testing.T) {
	value := map[string]any{
		"conversation": map[string]any{
			"safe": "conversation-kept",
			"nested": map[string]any{
				"secret":              "generic-secret",
				"client_secret":       "client-secret",
				"node_secret":         "node-secret",
				"task":                "legacy-task",
				"task_id":             "legacy-task-id",
				"runtime_session_id":  "native-session",
				"token":               "token-secret",
				"access_token":        "access-token",
				"credential":          "credential-secret",
				"callback":            "callback-secret",
				"callback_capability": "callback-capability",
				"client_name":         "allowed-client",
			},
		},
		"worker_activity": []any{
			map[string]any{"safe": "worker-kept", "node_secret": "worker-secret", "task": "worker-task"},
		},
		"secretary_activity": []any{
			map[string]any{"safe": "secretary-kept", "credential_hash": "credential-hash", "task_id": "secretary-task"},
		},
	}

	sanitized, ok := sanitizePublicJSON(value).(map[string]any)
	if !ok {
		t.Fatalf("sanitized value has type %T", sanitizePublicJSON(value))
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, leaked := range []string{
		"generic-secret", "client-secret", "node-secret", "legacy-task", "legacy-task-id", "native-session",
		"token-secret", "access-token", "credential-secret", "callback-secret", "callback-capability",
		"worker-secret", "worker-task", "credential-hash", "secretary-task",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("forbidden nested value leaked: %s in %s", leaked, body)
		}
	}
	for _, kept := range []string{"conversation-kept", "allowed-client", "worker-kept", "secretary-kept"} {
		if !strings.Contains(body, kept) {
			t.Fatalf("allowed product value was removed: %s", kept)
		}
	}
}
