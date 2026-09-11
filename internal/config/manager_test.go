package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReloadLeavesSnapshotWhenRecorderFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first := manager.Snapshot()
	workerPath := filepath.Join(filepath.Dir(path), "profiles", "worker.md")
	if err := os.WriteFile(workerPath, []byte("changed worker"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.SetChangeRecorder(func(previous, next Snapshot) error { return os.ErrPermission })
	if _, err := manager.Reload(); err == nil {
		t.Fatal("expected recorder error")
	}
	if got := manager.Snapshot().Version; got != first.Version {
		t.Fatalf("snapshot changed: %s != %s", got, first.Version)
	}
}

func TestOpenCreatesExternalDefaultsAndKeepsSnapshotOnInvalidReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first := manager.Snapshot()
	if first.Version == "" {
		t.Fatal("missing version")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "profiles", "worker.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[runtime]\nharness = \"bad\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); err == nil {
		t.Fatal("expected invalid reload")
	}
	if got := manager.Snapshot().Version; got != first.Version {
		t.Fatalf("snapshot changed after failed reload: %s != %s", got, first.Version)
	}
}
