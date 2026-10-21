//go:build windows

package acp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/nativeexec"
)

// A dedicated helper ensures an actual process subtree, not only a cmd shim.
func TestACPWindowsProcessTreeHelper(t *testing.T) {
	if os.Getenv("ACP_WIN_TREE_HELPER") != "1" {
		return
	}
	sep := -1
	for i, value := range os.Args {
		if value == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+2 >= len(os.Args) {
		os.Exit(2)
	}
	mode, target := os.Args[sep+1], os.Args[sep+2]
	if mode == "child" {
		time.Sleep(1300 * time.Millisecond)
		_ = os.WriteFile(target, []byte("orphan"), 0600)
		os.Exit(0)
	}
	if mode != "parent" {
		os.Exit(3)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestACPWindowsProcessTreeHelper$", "--", "child", target)
	if err := child.Start(); err != nil {
		os.Exit(4)
	}
	if err := os.WriteFile(target+".ready", []byte("spawned"), 0600); err != nil {
		os.Exit(5)
	}
	_ = child.Wait()
	os.Exit(0)
}

func TestACPWindowsCancelStopsDescendants(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "unexpected-child-output")
	t.Setenv("ACP_WIN_TREE_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := nativeexec.CommandContext(ctx, os.Args[0], "-test.run=^TestACPWindowsProcessTreeHelper$", "--", "parent", target)
	if err != nil {
		t.Fatal(err)
	}
	configureACPProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(target + ".ready"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = cmd.Wait()
			t.Fatal("native helper did not spawn its child")
		}
		time.Sleep(15 * time.Millisecond)
	}
	cancel()
	if err := cmd.Wait(); err == nil {
		t.Fatal("cancelled ACP parent exited successfully")
	}
	time.Sleep(1450 * time.Millisecond)
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Windows native descendant survived cancellation: %v", err)
	}
}
