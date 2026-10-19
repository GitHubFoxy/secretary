package main

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestServerBackupRetainsCanonicalPreferencesAndLegacyFiles(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := core.Open(ctx, filepath.Join(dataDir, "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	files := map[string]string{
		"config.toml":        "[secretary]\n",
		"user.md":            "legacy context",
		"memory.md":          "- Remember the acceptance test.\n",
		"user_preferences.md": "- Respond concisely.\n",
		"preferences.md":      "historical preferences file",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dataDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	extras, err := serverBackupExtraFiles(dataDir, filepath.Join(dataDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(extras) != len(files) {
		t.Fatalf("backup selected %d files, want %d: %v", len(extras), len(files), extras)
	}
	archive := filepath.Join(t.TempDir(), "server.zip")
	if err := store.CreateServerBackup(ctx, archive, extras); err != nil {
		t.Fatal(err)
	}

	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]bool)
	for _, file := range reader.File {
		entries[file.Name] = true
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	for name := range files {
		if !entries["extras/"+name] {
			t.Fatalf("backup archive omits %s", name)
		}
	}

	restored := filepath.Join(t.TempDir(), "restored")
	if err := core.RestoreServerBackupAuto(ctx, archive, restored); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(restored, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("restored %s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(restored, "secretary.db")); err != nil {
		t.Fatal(err)
	}
}

func TestServerBackupRejectsDirectoryNamedAsPreferenceFile(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dataDir, "user_preferences.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := serverBackupExtraFiles(dataDir, filepath.Join(dataDir, "config.toml")); err == nil {
		t.Fatal("backup did not reject invalid preference file")
	}
}
