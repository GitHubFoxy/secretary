package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUserDocumentRevisionIsDurableAndInvalidFileKeepsSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	documentPath := filepath.Join(t.TempDir(), "user.md")
	first, err := store.LoadUserDocument(ctx, documentPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.SaveUserDocument(ctx, documentPath, "# User\n\nPrefer short answers.\n")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision+1 {
		t.Fatalf("revisions=%d,%d", first.Revision, second.Revision)
	}
	if _, err := store.SaveUserDocument(ctx, documentPath, "bad\x00document"); err == nil {
		t.Fatal("invalid document accepted")
	}
	if err := os.WriteFile(documentPath, []byte("bad\x00document"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadUserDocument(ctx, documentPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != second.Revision || loaded.Content != second.Content {
		t.Fatalf("invalid file replaced snapshot: %#v", loaded)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restarted, err := store.LoadUserDocument(ctx, documentPath)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Revision != second.Revision || restarted.Content != second.Content {
		t.Fatalf("restart snapshot=%#v", restarted)
	}
}
