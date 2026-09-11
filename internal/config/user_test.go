package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserDocumentValidWriteRevisionAndInvalidReloadPreservesSnapshot(t *testing.T) {
	dir := t.TempDir()
	manager, err := OpenUserDocument(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := manager.Snapshot()
	second, err := manager.Save("# Preferences\n- concise\n")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision+1 || second.Content == first.Content {
		t.Fatalf("revisions first=%#v second=%#v", first, second)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "user.md")); err != nil || string(got) != second.Content {
		t.Fatalf("user.md=%q err=%v", got, err)
	}
	if _, err := manager.Save("bad\x00markdown"); err == nil {
		t.Fatal("invalid user.md accepted")
	}
	if manager.Snapshot().Revision != second.Revision || manager.Snapshot().Content != second.Content {
		t.Fatal("invalid write replaced active snapshot")
	}
	if err := os.WriteFile(filepath.Join(dir, "user.md"), []byte("bad\x00markdown"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if manager.Snapshot().Content != second.Content || manager.Snapshot().Revision != second.Revision {
		t.Fatal("invalid reload replaced active snapshot")
	}
}
