package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerReasoningIsIndependentAndVisibleInConfigDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	original := manager.Snapshot()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "reasoning = \"default\"", "reasoning = \"low\"", 1)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	separate, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if separate.Profiles["secretary"].Reasoning != "low" || separate.Profiles["worker"].Reasoning != "default" || separate.Profiles["worker"].Model != "default" {
		t.Fatal("Worker inherited Secretary settings")
	}
	text = strings.Replace(text, "reasoning = \"default\"", "reasoning = \"high\"", 1)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Diff(separate, changed)["worker_policy"]; !ok {
		t.Fatal("Worker reasoning absent from config diff")
	}
	if separate.Profiles["secretary"].Hash != changed.Profiles["secretary"].Hash || separate.Profiles["worker"].Hash == changed.Profiles["worker"].Hash || original.Profiles["worker"].Model != "default" {
		t.Fatal("independent profile hashes not preserved")
	}
	text = strings.Replace(text, "reasoning = \"high\"", "reasoning = \"unsupported\"", 1)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "worker_policy.reasoning") {
		t.Fatalf("invalid Worker reasoning error=%v", err)
	}
}
