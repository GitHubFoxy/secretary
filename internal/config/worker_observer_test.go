package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerObserverToolActivityDefaultAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !manager.Snapshot().Config.WorkerObserver.ShowToolActivity {
		t.Fatal("default Worker tool activity visibility is disabled")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(content), "\n[worker_observer]\nshow_tool_activity = true\n", "", 1)
	if legacy == string(content) {
		t.Fatal("bundled config is missing the Worker observer setting")
	}
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Config.WorkerObserver.ShowToolActivity {
		t.Fatal("legacy config without Worker observer setting did not preserve visibility")
	}

	updated := strings.Replace(legacy, "[telegram]", "[worker_observer]\nshow_tool_activity = false\n\n[telegram]", 1)
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Config.WorkerObserver.ShowToolActivity {
		t.Fatal("explicit false was not applied on reload")
	}
	if _, ok := Diff(loaded, reloaded)["worker_observer"]; !ok {
		t.Fatal("Worker observer preference missing from config diff")
	}
}
