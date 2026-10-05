package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeNativeStoreSelectionIsStableAndInstallationPrivate(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "home", ".local", "share")
	selected, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), false, legacy)
	if err != nil {
		t.Fatal("clean Node did not select a private native store")
	}
	want := OpenCodeNativeDataHome(filepath.Join(root, "node-data"))
	if selected.Legacy || selected.MigrationRequired || selected.DataHome != want {
		t.Fatal("clean Node selected a legacy or unexpected OpenCode store")
	}
	nativeDB := filepath.Join(want, "opencode", "opencode.db")
	if err := os.WriteFile(nativeDB, []byte("existing native sessions"), 0o600); err != nil {
		t.Fatal("private native DB fixture unavailable")
	}
	if err := os.MkdirAll(filepath.Join(legacy, "opencode"), 0o700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(legacy, "opencode", "opencode.db")
	if err := os.WriteFile(canary, []byte("personal native history"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), true, legacy)
	if err != nil || reopened.DataHome != want || reopened.Legacy || reopened.MigrationRequired {
		t.Fatal("reopening setup switched or reset the selected native store")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "personal native history" {
		t.Fatal("clean Node store selection touched the personal OpenCode database")
	}
	nativeData, err := os.ReadFile(nativeDB)
	if err != nil || string(nativeData) != "existing native sessions" {
		t.Fatal("reopening setup reset the existing native DB")
	}
	for _, path := range []string{want, filepath.Join(want, "opencode")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Fatal("private native store directory is not mode 0700")
		}
	}
	selectionInfo, err := os.Stat(filepath.Join(root, "node-data", opencodeStoreSelectionFile))
	if err != nil || selectionInfo.Mode().Perm() != 0o600 {
		t.Fatal("Node native store selection is not mode 0600")
	}
}

func TestOpenCodeNativeStorePreservesLegacySessionsUntilApprovedMigration(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "legacy-data")
	if err := os.MkdirAll(filepath.Join(legacy, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(legacy, "opencode", "opencode.db")
	if err := os.WriteFile(canary, []byte("legacy session database"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), true, legacy)
	if err != nil || !selected.Legacy || !selected.MigrationRequired || selected.DataHome != legacy {
		t.Fatal("old Node state did not preserve its existing native data path")
	}
	reopened, err := SelectOpenCodeNativeStore(filepath.Join(root, "node-data"), false, filepath.Join(root, "another-store"))
	if err != nil || !reopened.Legacy || !reopened.MigrationRequired || reopened.DataHome != legacy {
		t.Fatal("restart silently changed a pinned legacy native store")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "legacy session database" {
		t.Fatal("legacy store contents were changed during selection")
	}
	info, err := os.Stat(legacy)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("legacy user's data directory permissions were changed")
	}
	if _, err := os.Stat(OpenCodeNativeDataHome(filepath.Join(root, "node-data"))); !os.IsNotExist(err) {
		t.Fatal("legacy selection created a misleading empty replacement store")
	}
}

func TestOpenCodeNativeStoreSelectionTempSymlinkCannotTruncatePersonalDB(t *testing.T) {
	root := t.TempDir()
	nodeData := filepath.Join(root, "node-data")
	if err := os.MkdirAll(nodeData, 0o700); err != nil {
		t.Fatal(err)
	}
	personalDB := filepath.Join(root, "personal-opencode.db")
	if err := os.WriteFile(personalDB, []byte("personal sessions"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempPath := filepath.Join(nodeData, opencodeStoreSelectionFile+".tmp")
	if err := os.Symlink(personalDB, tempPath); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectOpenCodeNativeStore(nodeData, false, ""); err == nil {
		t.Fatal("selection followed a temp symlink and overwrote a personal DB")
	}
	data, err := os.ReadFile(personalDB)
	if err != nil || string(data) != "personal sessions" {
		t.Fatal("failed selection changed personal native DB")
	}
}

func TestOpenCodeNativeStoreRejectsSymlinkToPersonalStore(t *testing.T) {
	root := t.TempDir()
	personal := filepath.Join(root, "personal-opencode")
	if err := os.MkdirAll(personal, 0o700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(personal, "opencode.db")
	if err := os.WriteFile(canary, []byte("must not be touched"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodeData := filepath.Join(root, "node-data")
	if err := os.MkdirAll(nodeData, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(personal, OpenCodeNativeDataHome(nodeData)); err != nil {
		t.Fatal(err)
	}
	if _, err := SelectOpenCodeNativeStore(nodeData, false, ""); err == nil {
		t.Fatal("native store selection followed a symlink to personal data")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "must not be touched" {
		t.Fatal("rejected symlink changed the personal store")
	}
	if _, err := os.Stat(filepath.Join(personal, "opencode")); !os.IsNotExist(err) {
		t.Fatal("rejected symlink caused native files to be created in personal data")
	}
}
