package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT09WorkerPublicationRejectsUnsafeLocalFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "safe.zip"), []byte("PK"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "large.zip"), make([]byte, core.MaxArtifactBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "safe.zip"), filepath.Join(root, "alias.zip")); err != nil {
		t.Logf("symlinks unavailable: %v", err)
	}
	pub := ArtifactPublisher{Node: "remote", WorkerRef: "worker", AttemptID: "attempt", Workspace: root, Capability: "scoped"}
	cases := []struct{ path, name string }{
		{"../outside.zip", "outside.zip"},
		{filepath.Join(root, "safe.zip"), "absolute.zip"},
		{".env", "secret.txt"},
		{"folder/../safe.zip", "safe.zip"},
		{"folder", "folder.zip"},
		{"large.zip", "large.zip"},
		{"safe.zip", "secret.exe"},
		{"safe.zip", "../outside.zip"},
	}
	if _, err := os.Lstat(filepath.Join(root, "alias.zip")); err == nil {
		cases = append(cases, struct{ path, name string }{"alias.zip", "alias.zip"})
	}
	for _, tc := range cases {
		if _, err := pub.Publish(context.Background(), tc.path, tc.name); err == nil {
			t.Errorf("published unsafe file path=%q name=%q", tc.path, tc.name)
		}
	}
	if !ValidPublishedName("output.zip") || ValidPublishedName(".env") || ValidPublishedName("bad.exe") ||
		ValidPublishedName("other/secret.txt") {
		t.Fatal("published filename validation broken")
	}
}
