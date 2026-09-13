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

func TestClientPublicSanitizerRedactsNestedToolCoTInSecretaryAndWorkerPublicPayloads(t *testing.T) {
	value := map[string]any{
		"secretary": map[string]any{
			"kind":      "secretary.tool_call",
			"arguments": `{"command":"ls","nested":{"analysis":"secretary-analysis","safe":"secretary-safe","thought":"secretary-thought"}}`,
			"result":    `{"status":"ok","nested":{"reasoning":"secretary-reasoning","chain_of_thought":"secretary-chain","safe":"secretary-result-safe"}}`,
		},
		"worker": map[string]any{
			"kind": "attempt.activity",
			"payload": map[string]any{
				"tool_call": map[string]any{
					"name": "shell", "arguments": map[string]any{"command": "pwd", "nested": map[string]any{"analysis": "worker-analysis", "safe": "worker-safe", "thought": "worker-thought"}},
				},
				"tool_result": map[string]any{
					"output": `{"status":"ok","nested":{"reasoning":"worker-reasoning","chain_of_thought":"worker-chain","safe":"worker-result-safe"}}`,
				},
			},
		},
	}
	encoded, err := json.Marshal(sanitizePublicJSON(value))
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, forbidden := range []string{"secretary-analysis", "secretary-thought", "secretary-reasoning", "secretary-chain", "worker-analysis", "worker-thought", "worker-reasoning", "worker-chain"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("nested CoT leaked %q: %s", forbidden, body)
		}
	}
	for _, safe := range []string{"ls", "secretary-safe", "secretary-result-safe", "pwd", "worker-safe", "worker-result-safe"} {
		if !strings.Contains(body, safe) {
			t.Fatalf("ordinary public payload was removed %q: %s", safe, body)
		}
	}
}

func TestClientPublicSanitizerRedactsUppercaseAndAcronymKeyVariantsRecursively(t *testing.T) {
	value := map[string]any{
		"safeProduct": "kept",
		"nested": []any{
			map[string]any{
				"SECRET":              "upper-secret",
				"sEcReT":              "mixed-secret",
				"TASK_ID":             "upper-task",
				"TASKID":              "acronym-task",
				"RUNTIME_SESSION_ID":  "upper-session",
				"RUNTIMESESSIONID":    "acronym-session",
				"ACCESS_TOKEN":        "upper-token",
				"ACCESSTOKEN":         "acronym-token",
				"CALLBACK_CAPABILITY": "upper-callback",
				"CALLBACKCAPABILITY":  "acronym-callback",
				"allowed_field":       "allowed",
			},
		},
	}
	sanitized := sanitizePublicJSON(value)
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, leaked := range []string{
		"upper-secret", "mixed-secret", "upper-task", "acronym-task", "upper-session", "acronym-session",
		"upper-token", "acronym-token", "upper-callback", "acronym-callback",
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("uppercase/acronym forbidden value leaked: %s in %s", leaked, body)
		}
	}
	for _, kept := range []string{"kept", "allowed"} {
		if !strings.Contains(body, kept) {
			t.Fatalf("allowed product value was removed: %s in %s", kept, body)
		}
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
				"clientSecret":        "camel-client-secret",
				"nodeSecret":          "camel-node-secret",
				"taskId":              "camel-task-id",
				"runtimeSessionId":    "camel-session",
				"accessToken":         "camel-access-token",
				"callbackCapability":  "camel-callback",
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
		"worker-secret", "worker-task", "credential-hash", "secretary-task", "camel-client-secret", "camel-node-secret",
		"camel-task-id", "camel-session", "camel-access-token", "camel-callback",
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
