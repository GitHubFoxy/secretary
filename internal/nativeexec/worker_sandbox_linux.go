//go:build linux

package nativeexec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func sandboxInvocation(policy WorkerSandbox, command string, args []string) (string, []string, error) {
	if policy.ProtectedDir == "" || policy.Workspace == "" {
		return "", nil, errors.New("worker sandbox: missing private Node data directory or workspace")
	}
	protected, err := filepath.EvalSymlinks(policy.ProtectedDir)
	if err != nil { return "", nil, fmt.Errorf("worker sandbox: private Node directory unavailable: %w", err) }
	workspace, err := filepath.EvalSymlinks(policy.Workspace)
	if err != nil { return "", nil, fmt.Errorf("worker sandbox: workspace unavailable: %w", err) }
	protected, err = filepath.Abs(protected)
	if err != nil { return "", nil, err }
	workspace, err = filepath.Abs(workspace)
	if err != nil { return "", nil, err }
	if protected == "/" || workspace == "/" ||
		protected == workspace ||
		strings.HasPrefix(protected, workspace+string(filepath.Separator)) ||
		strings.HasPrefix(workspace, protected+string(filepath.Separator)) {
		return "", nil, errors.New("worker sandbox: private Node data and Worker workspace must be disjoint")
	}
	// No PATH lookup: a Worker-controlled executable cannot replace the
	// trusted namespace helper. A missing OS boundary is a hard failure.
	var bwrap string
	for _, candidate := range []string{"/usr/bin/bwrap", "/bin/bwrap"} {
		stat, statErr := os.Stat(candidate)
		if statErr != nil || !stat.Mode().IsRegular() || stat.Mode().Perm()&022 != 0 { continue }
		detail, ok := stat.Sys().(*syscall.Stat_t)
		if !ok || detail.Uid != 0 { continue }
		bwrap = candidate
		break
	}
	if bwrap == "" {
		return "", nil, errors.New("worker sandbox: trusted bubblewrap is required (no unsandboxed fallback)")
	}
	wrapped := []string{
		"--die-with-parent", "--new-session",
		"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
		"--ro-bind", "/", "/",
		"--proc", "/proc", "--dev", "/dev",
		"--tmpfs", "/tmp",
		"--tmpfs", protected,
		"--bind", workspace, workspace,
		"--chdir", workspace,
		"--", command,
	}
	return bwrap, append(wrapped, args...), nil
}
