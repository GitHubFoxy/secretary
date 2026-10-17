//go:build linux

package node

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/nativeexec"
)

func TestT09CoLocatedServerNodeWorkerCannotReadSQLiteOrHMAC(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil { t.Skip("bubblewrap unavailable") }
	ctx := context.Background()
	root := t.TempDir()
	serverDir := filepath.Join(root,"server")
	nodeDir := filepath.Join(root,"node")
	workspace := filepath.Join(root,"workspace")
	for _, p := range []string{serverDir,nodeDir,workspace} {
		if err := os.MkdirAll(p,0700); err != nil { t.Fatal(err) }
	}
	db := filepath.Join(serverDir,"secretary.db")
	store, err := core.Open(ctx,db)
	if err != nil { t.Fatal(err) }
	defer store.Close()
	if _, err := store.EnsureNodeTransportSecret(ctx); err != nil { t.Fatal(err) }
	if err := store.ConfigureNodePairingToken(ctx,"pair-once"); err != nil { t.Fatal(err) }
	if _, err := store.EnrollNodeWithPairing(ctx,"pair-once","worker-host",[]byte(strings.Repeat("X",32))); err != nil { t.Fatal(err) }
	for _, name := range []string{"identity.json","node-state.json"} {
		if err := os.WriteFile(filepath.Join(nodeDir,name),[]byte("NODE-CREDENTIAL"),0600); err != nil { t.Fatal(err) }
	}
	visibleFile := filepath.Join(workspace,"output.txt")
	if err := os.WriteFile(visibleFile,[]byte("workspace-only"),0600); err != nil { t.Fatal(err) }
	// A co-located Server and Node can have exactly the same outer UID. The
	// sandbox still cannot open Server SQLite (or its WAL/journal/secret key),
	// Node credential, or Node state; it can read its assigned workspace.
	script := `set -eu
		test ! -r "$1"
		test ! -r "$2"
		test ! -r "$3"
		test ! -r "$4"
		test ! -r "$5"
		test "$(cat "$6")" = "workspace-only"`
	nodeState := filepath.Join(nodeDir,"node-state.json")
	nodeIdentity := filepath.Join(nodeDir,"identity.json")
	sandbox := nativeexec.WithWorkerSandbox(ctx,nativeexec.WorkerSandbox{ProtectedDir:nodeDir,Workspace:workspace})
	cmd, err := nativeexec.CommandContext(sandbox,"/bin/sh","-c",script,"worker-test",db,db+"-wal",db+"-shm",nodeIdentity,nodeState,visibleFile)
	if err != nil { t.Fatal(err) }
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output),"Operation not permitted") {
			t.Skipf("host denied kernel namespaces (production must fail closed): %s",output)
		}
		t.Fatalf("Worker escaped co-located Server/Node secret isolation: %v: %s",err,output)
	}
}
