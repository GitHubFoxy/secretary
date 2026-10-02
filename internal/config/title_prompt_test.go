package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenCreatesTitlePromptWithoutOverwritingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(filepath.Dir(path), DefaultTitlePrompt)
	if manager.Snapshot().TitlePrompt.Path != promptPath {
		t.Fatal("unexpected prompt path")
	}
	info, err := os.Stat(promptPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("prompt file mode: %v", err)
	}
	custom := "Верни короткое название задачи.\n"
	if err := os.WriteFile(promptPath, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Snapshot().TitlePrompt.Content != custom {
		t.Fatal("existing title prompt was overwritten")
	}
}

func TestLegacyTitlePromptUsesEmbeddedDefaultUntilOpen(t *testing.T) {
	path := titleConfigPath(t, "")
	promptPath := filepath.Join(filepath.Dir(path), DefaultTitlePrompt)
	if err := os.Remove(promptPath); err != nil {
		t.Fatal(err)
	}
	before, err := Load(path)
	if err != nil || before.TitlePrompt.Content == "" {
		t.Fatalf("legacy prompt unavailable: %v", err)
	}
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if manager.Snapshot().TitlePrompt != before.TitlePrompt || manager.Snapshot().Version != before.Version {
		t.Fatal("materialization changed the effective prompt")
	}
	if _, err := os.Stat(promptPath); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitTitlePromptPaths(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		path := titleConfigPath(t, "")
		promptPath := filepath.Join(filepath.Dir(path), "custom-title.md")
		if err := os.WriteFile(promptPath, []byte("Составь название задачи.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		configured := "custom-title.md"
		if absolute {
			configured = promptPath
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, []byte("\n[telegram]\ntitle_prompt = \""+configured+"\"\n")...)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := Load(path)
		if err != nil || snapshot.TitlePrompt.Path != promptPath || snapshot.TitlePrompt.Content != "Составь название задачи.\n" {
			t.Fatalf("custom prompt unavailable: %v", err)
		}
	}
}

func TestTitlePromptReloadIsIndependentAndRejectsInvalidFiles(t *testing.T) {
	path := titleConfigPath(t, "[telegram]\ntitle_harness = \"opencode\"\ntitle_prompt = \"title-generation-prompt.md\"\n")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	var recorded map[string]any
	manager.SetChangeRecorder(func(previous, next Snapshot) error { recorded = Diff(previous, next); return nil })
	promptPath := before.TitlePrompt.Path
	if err := os.WriteFile(promptPath, []byte("Новое название.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	active, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if active.Version == before.Version || active.TitlePrompt.Hash == before.TitlePrompt.Hash || !reflect.DeepEqual(before.Profiles, active.Profiles) || !reflect.DeepEqual(before.Config, active.Config) {
		t.Fatal("prompt reload did not preserve runtime profiles/policy")
	}
	want := map[string]any{"telegram.title_prompt": active.TitlePrompt.Hash}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatalf("diff=%#v", recorded)
	}
	for _, content := range [][]byte{nil, []byte(strings.Repeat("x", 64*1024+1)), []byte("invalid\xff")} {
		if err := os.WriteFile(promptPath, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Reload(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid prompt accepted: %v", err)
		}
		if got := manager.Snapshot(); got.Version != active.Version || got.TitlePrompt != active.TitlePrompt {
			t.Fatal("invalid prompt replaced active snapshot")
		}
	}
	if err := os.Remove(promptPath); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing explicit prompt accepted: %v", err)
	}
	if !reflect.DeepEqual(recorded, want) {
		t.Fatal("invalid prompt called recorder")
	}
}
