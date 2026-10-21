//go:build !windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/node"
)

func TestNodeInstanceLockHelperProcess(t *testing.T) {
	mode := os.Getenv("SECRETARY_NODE_LOCK_TEST_MODE")
	if mode == "" {
		return
	}
	release, err := claimNodeInstance(os.Getenv("SECRETARY_NODE_LOCK_TEST_DIR"))
	if err != nil {
		fmt.Fprintln(os.Stdout, "BUSY")
		return
	}
	defer release()
	fmt.Fprintln(os.Stdout, "CLAIMED")
	if mode == "hold" {
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
}

func TestUnixNodeDataDirectoryOwnedByOneProcessAndReleasedOnExit(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "node-state")
	symlink := filepath.Join(t.TempDir(), "alias")
	if err := os.MkdirAll(dataDir, 0o700); err != nil { t.Fatal(err) }
	if err := os.Symlink(dataDir, symlink); err != nil { t.Fatal(err) }

	spawn := func(mode, dir string) *exec.Cmd {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestNodeInstanceLockHelperProcess$")
		cmd.Env = append(os.Environ(),
			"SECRETARY_NODE_LOCK_TEST_MODE="+mode,
			"SECRETARY_NODE_LOCK_TEST_DIR="+dir)
		return cmd
	}
	holder := spawn("hold", dataDir)
	stdin, err := holder.StdinPipe()
	if err != nil { t.Fatal(err) }
	stdout, err := holder.StdoutPipe()
	if err != nil { t.Fatal(err) }
	if err := holder.Start(); err != nil { t.Fatal(err) }
	holderDone := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if !holderDone {
			_ = holder.Process.Kill()
			_ = holder.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "CLAIMED" {
		t.Fatalf("first Node did not take ownership: %q err=%v", line, err)
	}

	competitor := spawn("try", symlink)
	output, err := competitor.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "BUSY") || strings.Contains(string(output), "CLAIMED") {
		t.Fatalf("second Node acquired the same directory through symlink: output=%q err=%v", output, err)
	}

	lockPath := filepath.Join(dataDir, ".secretary-node.lock")
	before, err := os.Stat(lockPath)
	if err != nil || before.Mode().Perm() != 0o600 {
		t.Fatalf("Node lock file missing/private mode incorrect: %#v err=%v", before, err)
	}
	if err := stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) { t.Fatal(err) }
	if err := holder.Wait(); err != nil { t.Fatal(err) }
	holderDone = true

	after, err := os.Stat(lockPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("Node lock inode must persist across exits to prevent split-brain: before=%#v after=%#v err=%v", before, after, err)
	}
	successor := spawn("try", dataDir)
	output, err = successor.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "CLAIMED") {
		t.Fatalf("ownership lock was not released after first process exit: output=%q err=%v", output, err)
	}
}

func TestUnixNodeCanonicalStatePathUnaffectedBySymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	alias := filepath.Join(root, "current")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o700); err != nil { t.Fatal(err) }
	}
	if err := os.Symlink(first, alias); err != nil { t.Fatal(err) }
	canonical, release, err := claimNodeDataDirectory(alias)
	if err != nil { t.Fatal(err) }
	defer release()
	if canonical != first { t.Fatalf("claimed %q instead of original directory %q", canonical, first) }
	if err := os.Remove(alias); err != nil { t.Fatal(err) }
	if err := os.Symlink(second, alias); err != nil { t.Fatal(err) }
	store, err := node.OpenLocalStore(filepath.Join(canonical, "node-state.json"))
	if err != nil { t.Fatal(err) }
	if _, err := store.ReserveEventSequence(); err != nil { t.Fatal(err) }
	if err := store.Close(); err != nil { t.Fatal(err) }
	if _, err := os.Stat(filepath.Join(first, "node-state.json")); err != nil {
		t.Fatalf("Node state not written into originally locked directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(second, "node-state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retargeted symlink received Node state: %v", err)
	}
	reused, err := claimNodeInstance(first)
	if err == nil {
		reused()
		t.Fatal("concurrent daemon was allowed to open original Node state directory")
	}
	otherRoot, otherRelease, err := claimNodeDataDirectory(alias)
	if err != nil { t.Fatal(err) }
	defer otherRelease()
	if otherRoot != second {
		t.Fatalf("retargeted alias must identify a separate locked directory: %q", otherRoot)
	}
}
