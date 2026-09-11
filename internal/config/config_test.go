package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCompilesExternalProfilesAndSkills(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"secretary.md": "secretary", "worker.md": "worker", "child.md": "child", "skill.md": "skill"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(dir, "config.toml")
	text := `skills = ["` + filepath.Join(dir, "skill.md") + `"]
[profiles]
secretary = "secretary.md"
worker = "worker.md"
child_worker = "child.md"
[tools]
allow_tools = ["bash", "read"]
[models]
secretary = "openai/gpt"
fast = "fast"
smart = "smart"
cheap = "cheap"
[runtime]
harness = "opencode"
reasoning = "high"
`
	if err := os.WriteFile(configPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Version == "" || snapshot.Profiles["worker"].Content != "worker" || snapshot.Profiles["worker"].Model != "smart" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if got := snapshot.Profiles["secretary"].Skills; len(got) != 1 || got[0].Content != "skill" {
		t.Fatalf("skills = %#v", got)
	}
}

func TestLoadRejectsInvalidConfigWithoutSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	text := `[profiles]
secretary = "s"
worker = "w"
child_worker = "c"
[tools]
allow_tools = ["read"]
[models]
secretary = "s"
fast = "f"
smart = "s"
cheap = "c"
[runtime]
harness = "unknown"
`
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "runtime.harness") {
		t.Fatalf("err = %v", err)
	}
}
