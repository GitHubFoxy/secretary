package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretaryAndWorkerHarnessPoliciesAreIndependent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"secretary.md", "worker.md", "child.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.toml")
	content := `[profiles]
secretary = "secretary.md"
worker = "worker.md"
child_worker = "child.md"
[tools]
allow_tools = ["read"]
[models]
secretary = "secretary-model"
fast = "fast"
smart = "smart"
cheap = "cheap"
[secretary]
harness = "codex"
model = "secretary-model"
reasoning = "high"
[worker_policy]
default_harness = "fx"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Config.Secretary.Harness != "codex" || snapshot.Config.Secretary.Model != "secretary-model" || snapshot.Config.Secretary.Reasoning != "high" {
		t.Fatalf("secretary policy=%#v", snapshot.Config.Secretary)
	}
	if snapshot.Config.WorkerPolicy.DefaultHarness != "fx" {
		t.Fatalf("worker policy=%#v", snapshot.Config.WorkerPolicy)
	}
	if snapshot.Profiles["secretary"].Runtime != "codex" || snapshot.Profiles["worker"].Runtime != "fx" {
		t.Fatalf("profiles=%#v", snapshot.Profiles)
	}
}

func TestInvalidReloadPreservesActiveConfigSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	if err := os.WriteFile(path, []byte("[secretary]\nharness = \"unknown\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); err == nil || !strings.Contains(err.Error(), "harness") {
		t.Fatalf("reload error=%v", err)
	}
	if manager.Snapshot().Version != before.Version {
		t.Fatal("invalid config replaced active snapshot")
	}
}
