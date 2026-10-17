package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSecretaryProfileRoutesOwnerAnswersToExistingWorker(t *testing.T) {
	manager, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	profile := manager.Snapshot().Profiles["secretary"].Content
	for _, required := range []string{
		"Result ends only the current Attempt, not the Task",
		"message_worker with the existing worker_ref",
		"Never spawn a new Worker merely to answer an existing Worker's question",
		"ask one brief clarification",
		"relevant earlier discussion and decisions",
	} {
		if !strings.Contains(profile, required) {
			t.Fatalf("Secretary's default profile is missing the Worker Follow-up instruction %q", required)
		}
	}
}
