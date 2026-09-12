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
