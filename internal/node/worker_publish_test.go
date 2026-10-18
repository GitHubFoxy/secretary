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

func TestT09PinnedWorkspaceCannotBeReplacedAfterRegistration(t *testing.T) {
	base:=t.TempDir()
	workspace:=filepath.Join(base,"workspace")
	if err:=os.Mkdir(workspace,0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(workspace,"result.zip"),[]byte("AUTHORIZED"),0600);err!=nil{t.Fatal(err)}
	pinned,err:=os.OpenRoot(workspace)
	if err!=nil{t.Fatal(err)}
	defer pinned.Close()
	if err:=os.Rename(workspace,filepath.Join(base,"old-workspace"));err!=nil{t.Fatal(err)}
	if err:=os.Mkdir(workspace,0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(workspace,"result.zip"),[]byte("OUTSIDE-SECRET"),0600);err!=nil{t.Fatal(err)}
	file,err:=openPublishedFromPinnedRoot(pinned,"result.zip")
	if err!=nil{t.Fatal(err)}
	defer file.Close()
	content,err:=os.ReadFile(file.Name())
	// The result is read through the pinned handle, never through the path
	// which has now been replaced by an attacker-controlled directory.
	_ = content;_ = err
	buf:=make([]byte,32)
	n,err:=file.Read(buf)
	if err!=nil && n==0{t.Fatal(err)}
	if string(buf[:n])!="AUTHORIZED"{t.Fatalf("privileged Node read from swapped workspace %q",buf[:n])}
}
