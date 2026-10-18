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
	// Do not mount /usr/local/lib or /usr/local/bin wholesale: an independent\n\t// Server root such as /usr/local/lib/secretary must stay invisible even\n\t// when ServerDataDir is unspecified. Native executables and their runtime\n\t// assets are mounted selectively below.\n\t// Do not share user-writable system configuration roots wholesale.
	// A Server --data-dir can be /etc/secretary or /usr/local/share/secretary.
	for _, systemRoot := range []string{"/etc/hosts", "/etc/resolv.conf", "/etc/nsswitch.conf",
		"/etc/passwd", "/etc/group"} {
		if _, statErr := os.Stat(systemRoot); statErr == nil {
			wrapped = append(wrapped, "--ro-bind", systemRoot, systemRoot)
		}
	}
	// Mount individual executable/runtime files only. No system library or
	// JavaScript package directory is ever shared wholesale: the Server may
	// have an independent data root anywhere under those directories.
	for _, runtime := range []string{"/bin/sh", "/usr/bin/env", "/usr/bin/node", "/usr/bin/nodejs", "/usr/bin/bash", "/usr/bin/cat", "/usr/bin/dirname", "/usr/bin/test"} {
		if err := mountRuntimeFile(&wrapped, runtime); err != nil { return "", nil, err }
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
	// Workspace is remounted AFTER the protected Node root is hidden, so
	// a workspace located inside the private root stays writable without
	// accidentally exposing sibling credential files.
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
	// Entrypoint and its own package assets are explicit file mounts. Never
	// bind the entrypoint's parent (e.g. ~/.local/bin) or all node_modules.
	if err := mountRuntimeFile(&wrapped, executable); err != nil { return "", nil, err }
	if !strings.HasPrefix(executable, "/usr/") && !strings.HasPrefix(executable, "/bin/") {
		components := strings.Split(filepath.Clean(executable), string(filepath.Separator))
		for i, component := range components {
			if component == "node_modules" && i+1 < len(components) {
				end := i+2
				if strings.HasPrefix(components[i+1], "@") && i+2 < len(components) { end++ }
				pkg := string(filepath.Separator)+filepath.Join(components[:end]...)
				if err := mountRuntimePackageFiles(&wrapped, pkg); err != nil { return "", nil, err }
				break
			}
		}
	}
	// Portable user installs may keep only an entrypoint in ~/.local/bin,
	// with trusted runtime assets under ~/.local/share/<harness>. Mount
	// per-harness packages, never ~/.local/share wholesale (which can
	// contain unrelated Server/Node credentials for the same OS UID).
	harnessName := strings.ToLower(strings.TrimSuffix(filepath.Base(executable),filepath.Ext(executable)))
	if harnessName == "codex" || harnessName == "claude" || harnessName == "opencode" {
		for _, packageName := range []string{harnessName} {
			assets := filepath.Join(home,".local","share",packageName)
			resolved, resolveErr := filepath.EvalSymlinks(assets)
			if os.IsNotExist(resolveErr) {continue}
			if resolveErr != nil {return "",nil,fmt.Errorf("worker sandbox: inspect user-installed CLI runtime assets: %w",resolveErr)}
			if resolved==protected || strings.HasPrefix(protected,resolved+string(filepath.Separator)) ||
				strings.HasPrefix(resolved,protected+string(filepath.Separator)) {
				return "",nil,errors.New("worker sandbox: CLI runtime assets overlap Node signing data")
			}
			if policy.ServerDataDir != "" {
				serverRoot, serverErr:=filepath.EvalSymlinks(policy.ServerDataDir)
				if serverErr!=nil {return "",nil,serverErr}
				if resolved==serverRoot || strings.HasPrefix(serverRoot,resolved+string(filepath.Separator)) ||
					strings.HasPrefix(resolved,serverRoot+string(filepath.Separator)) {
					return "",nil,errors.New("worker sandbox: CLI runtime assets overlap Server signing data")
				}
			}
			if err := mountRuntimePackageFiles(&wrapped, resolved); err != nil { return "", nil, err }
		}
	}
	// Mask after ALL shared binds. An arbitrary --data-dir (including one
	// nested under /etc, /usr, or a CLI installation) cannot be exposed by
	// a later mount. Explicit OpenCode and workspace binds were checked.
	wrapped = append(wrapped, "--tmpfs", protected)
	if policy.ServerDataDir != "" {
		serverRoot, resolveErr := filepath.EvalSymlinks(policy.ServerDataDir)
		if resolveErr != nil { return "", nil, fmt.Errorf("worker sandbox: Server private data root unavailable: %w",resolveErr) }
		if serverRoot == "/" || serverRoot == workspace || strings.HasPrefix(serverRoot,workspace+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: Server data root overlaps Worker workspace")
		}
		wrapped = append(wrapped, "--tmpfs", serverRoot)
	}
	if nativeStore != "" {
		// The narrowly authorized provider state may live within the
		// otherwise hidden Node directory, unlike its signing keys.
		wrapped = append(wrapped, "--bind", nativeStore, nativeStore)
	}
	wrapped = append(wrapped, "--bind", workspace, workspace)
	wrapped = append(wrapped, "--chdir", workspace, "--", executable)
	return bwrap, append(wrapped, args...), nil
}

func applyWorkerSandbox(_ WorkerSandbox, _ *exec.Cmd) error { return nil }

// mountRuntimeFile exposes a single regular file, never its containing
// directory. Resolved symlinks (such as /bin/sh -> /usr/bin/dash) are mounted
// at both the requested location and the concrete runtime path.
func mountRuntimeFile(wrapped *[]string, path string) error {
  info, err := os.Stat(path)
  if os.IsNotExist(err) { return nil }
  if err != nil { return err }
  if !info.Mode().IsRegular() { return fmt.Errorf("worker sandbox: runtime is not a regular file: %s", path) }
  resolved, err := filepath.EvalSymlinks(path)
  if err != nil { return err }
  for _, dest := range []string{resolved, path} {
    *wrapped = append(*wrapped, "--ro-bind", resolved, dest)
  }
  return nil
}

// A CLI package may use adjacent code/assets. Enumerate individual files,
// rejecting symlink traversal; do not share any directory, even read-only.
func mountRuntimePackageFiles(wrapped *[]string, packageRoot string) error {
  const maxFiles = 8192
  count := 0
  return filepath.WalkDir(packageRoot, func(path string, entry os.DirEntry, err error) error {
    if err != nil { return err }
    if entry.Type()&os.ModeSymlink != 0 { return fmt.Errorf("worker sandbox: symlink in CLI package: %s", path) }
    if entry.IsDir() { return nil }
    if !entry.Type().IsRegular() { return fmt.Errorf("worker sandbox: unsupported CLI package asset: %s", path) }
    // Known data/credential file formats are never exported as runtime assets.
    ext := strings.ToLower(filepath.Ext(path))
    switch ext {
    case ".db", ".sqlite", ".sqlite3", ".key", ".pem", ".env":
      return fmt.Errorf("worker sandbox: private data inside CLI package: %s", path)
    }
    count++
    if count > maxFiles { return errors.New("worker sandbox: CLI package exceeds bounded runtime manifest") }
    return mountRuntimeFile(wrapped, path)
  })
}
