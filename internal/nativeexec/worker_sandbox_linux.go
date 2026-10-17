//go:build linux

package nativeexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
		// A workspace may live *inside* private Node data; only this bound
		// subtree is remounted after the private parent has been hidden.
		strings.HasPrefix(protected, workspace+string(filepath.Separator)) {
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
	// An isolated OpenCode install keeps its model/provider store under Node
	// data. Remount only the explicitly selected native store, never the Node
	// parent directory that contains identity.json and node-state.json.
	var nativeStore string
	if policy.WritableDataHome != "" {
		nativeStore, err = filepath.EvalSymlinks(policy.WritableDataHome)
		if err != nil { return "", nil, fmt.Errorf("worker sandbox: OpenCode data home unavailable: %w", err) }
		nativeStore, err = filepath.Abs(nativeStore)
		if err != nil { return "", nil, err }
		if nativeStore == protected || nativeStore == workspace ||
			strings.HasPrefix(protected, nativeStore+string(filepath.Separator)) ||
			strings.HasPrefix(workspace, nativeStore+string(filepath.Separator)) ||
			strings.HasPrefix(nativeStore, workspace+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: unsafe OpenCode writable data home")
		}
		stat, statErr := os.Stat(nativeStore)
		if statErr != nil || !stat.IsDir() {
			return "", nil, errors.New("worker sandbox: OpenCode data home is unavailable")
		}
	}
	// Default-deny filesystem: a co-located Server database (and its WAL,
	// journal, backups, transport key or alternate data-dir) must never be
	// readable merely because Server and Node run as the same outer UID.
	// In particular, do not bind /, /home, /Users or /tmp wholesale.
	wrapped := []string{
		"--die-with-parent", "--new-session",
		"--unshare-user", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
		"--tmpfs", "/", "--dev", "/dev",
	}
	for _, systemRoot := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc"} {
		if _, statErr := os.Stat(systemRoot); statErr == nil {
			wrapped = append(wrapped, "--ro-bind", systemRoot, systemRoot)
		}
	}
	// Hide the private Node root even if its administrator selected an
	// alternate location INSIDE an otherwise shared system subtree.
	wrapped = append(wrapped, "--proc", "/proc", "--tmpfs", "/tmp")

	// Preserve native CLI authentication and session history while allowing
	// writes ONLY in explicitly selected provider-owned locations.
	// No ancestor directory is shared, including the default Server root
	// ~/.secretary. Node's own private dir is never mounted.
	home, homeErr := os.UserHomeDir()
	if homeErr != nil || !filepath.IsAbs(home) {
		return "", nil, errors.New("worker sandbox: home directory unavailable")
	}
	for _, name := range []string{".codex", ".claude", ".claude.json"} {
		state := filepath.Join(home, name)
		info, statErr := os.Lstat(state)
		if os.IsNotExist(statErr) { continue }
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, errors.New("worker sandbox: native state must not be a symlink")
		}
		if state == protected || strings.HasPrefix(protected, state+string(filepath.Separator)) ||
			state == workspace || strings.HasPrefix(workspace, state+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: native state overlaps protected Node data or Worker workspace")
		}
		wrapped = append(wrapped, "--bind", state, state)
	}
	wrapped = append(wrapped, "--bind", workspace, workspace)

	// Native harnesses and fake CLI test fixtures can live outside /usr;
	// expose only their executable, never their containing user home or /tmp.
	executable := command
	if !filepath.IsAbs(executable) {
		executable, err = exec.LookPath(executable)
		if err != nil { return "", nil, fmt.Errorf("worker sandbox: harness executable unavailable: %w", err) }
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil { return "", nil, fmt.Errorf("worker sandbox: resolve harness executable: %w", err) }
	executable, err = filepath.Abs(executable)
	if err != nil { return "", nil, err }
	if executable == protected || strings.HasPrefix(executable, protected+string(filepath.Separator)) {
		return "", nil, errors.New("worker sandbox: executable cannot reside in Node private state")
	}
	inSystem := false
	for _, prefix := range []string{"/usr/", "/bin/", "/sbin/", "/lib/", "/lib64/"} {
		if strings.HasPrefix(executable, prefix) { inSystem = true; break }
	}
	if !inSystem {
		// User-installed Codex/Claude/OpenCode entrypoints may read sibling
		// assets, a package.json, and hoisted node_modules. A single-file
		// bind loses these dependencies. Share only the trusted package
		// installation subtree, never the containing HOME or Node state.
		install := filepath.Dir(executable)
		components := strings.Split(filepath.Clean(executable), string(filepath.Separator))
		for i, component := range components {
			if component == "node_modules" && i > 0 {
				install = string(filepath.Separator) + filepath.Join(components[:i+1]...)
				break
			}
		}
		if install == "/" || install == protected ||
			strings.HasPrefix(executable, protected+string(filepath.Separator)) ||
			strings.HasPrefix(protected, install+string(filepath.Separator)) && install == string(filepath.Separator) {
			return "", nil, errors.New("worker sandbox: unsafe native CLI install root")
		}
		wrapped = append(wrapped, "--ro-bind", install, install)
	}
	// Mask after ALL shared binds. An arbitrary --data-dir (including one
	// nested under /etc, /usr, or a CLI installation) cannot be exposed by
	// a later mount. Explicit OpenCode and workspace binds were checked.
	wrapped = append(wrapped, "--tmpfs", protected)
	if nativeStore != "" {
		// The narrowly authorized provider state may live within the
		// otherwise hidden Node directory, unlike its signing keys.
		wrapped = append(wrapped, "--bind", nativeStore, nativeStore)
	}
	wrapped = append(wrapped, "--chdir", workspace, "--", executable)
	return bwrap, append(wrapped, args...), nil
}

func applyWorkerSandbox(_ WorkerSandbox, _ *exec.Cmd) error { return nil }
