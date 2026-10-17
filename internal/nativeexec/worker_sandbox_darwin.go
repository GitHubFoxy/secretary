//go:build darwin

package nativeexec

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The Apple Seatbelt sandbox applies to the harness process AND all of its
// children (shell, MCP and native provider runtime). Deny filesystem reads
// by default; in particular no parent user home or Secretary data is shared.
func sandboxInvocation(policy WorkerSandbox, command string, args []string) (string, []string, error) {
	if policy.ProtectedDir == "" || policy.Workspace == "" {
		return "", nil, errors.New("worker sandbox: private Node directory and workspace required")
	}
	const seatbelt = "/usr/bin/sandbox-exec"
	if st, err := os.Stat(seatbelt); err != nil || !st.Mode().IsRegular() {
		return "", nil, errors.New("worker sandbox: macOS Seatbelt sandbox unavailable (no unsandboxed fallback)")
	}
	protected, err := filepath.EvalSymlinks(policy.ProtectedDir)
	if err != nil { return "", nil, err }
	workspace, err := filepath.EvalSymlinks(policy.Workspace)
	if err != nil { return "", nil, err }
	if workspace == protected || strings.HasPrefix(protected, workspace+string(filepath.Separator)) {
		return "", nil, errors.New("worker sandbox: Node data root cannot be shared as a Worker workspace")
	}
	executable := command
	if !filepath.IsAbs(executable) {
		executable, err = exec.LookPath(executable)
		if err != nil { return "", nil, err }
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil { return "", nil, err }
	home, err := os.UserHomeDir()
	if err != nil { return "", nil, err }
	workerTemp := filepath.Join(workspace, ".worker-tmp")
	if err := os.MkdirAll(workerTemp, 0700); err != nil {
		return "", nil, errors.New("worker sandbox: create private Worker temp directory")
	}
	reads := []string{workerTemp, "/private/tmp", "/tmp", "/opt/homebrew", "/private/var/db", "/System", "/usr", "/bin", "/sbin", "/Library", "/Applications", "/private/etc", "/private/var/db/dyld", "/dev", workspace, executable}
	writes := []string{workspace, workerTemp, "/private/tmp", "/tmp"}
	for _, name := range []string{".codex", ".claude", ".claude.json"} {
		state := filepath.Join(home, name)
		info, statErr := os.Lstat(state)
		if os.IsNotExist(statErr) { continue }
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, errors.New("worker sandbox: native state must not be a symlink")
		}
		if state == protected || strings.HasPrefix(protected, state+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: native state overlaps Node data")
		}
		reads = append(reads, state)
		writes = append(writes, state)
	}
	if policy.WritableDataHome != "" {
		state, err := filepath.EvalSymlinks(policy.WritableDataHome)
		if err != nil { return "", nil, err }
		if state == protected || strings.HasPrefix(protected, state+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: native provider state overlaps protected root")
		}
		reads = append(reads, state)
		writes = append(writes, state)
	}
	// Escape as Scheme quoted literals; no interpolation into a shell.
	quoted := func(path string) string { return "(subpath " + strconv.Quote(path) + ")" }
	var profile strings.Builder
	profile.WriteString("(version 1)\n(deny default)\n(allow process*)\n(allow network*)\n(allow mach-lookup)\n(allow mach-host*)\n(allow sysctl-read)\n(allow iokit-open)\n(allow ipc-posix*)\n(allow file-read-metadata)\n(allow file-ioctl)\n(allow signal)\n")
	// Seatbelt filters inside one allow clause are conjunctive. Combining
	// disjoint subpaths there silently denies *all* reads and aborts dyld.
	// Each permitted root must be its own rule (a union of grants).
	for _, root := range reads {
		profile.WriteString("(allow file-read* " + quoted(root) + ")\n")
	}
	for _, root := range writes {
		profile.WriteString("(allow file-write* " + quoted(root) + ")\n")
	}
	return seatbelt, append([]string{"-p", profile.String(), executable}, args...), nil
}

func applyWorkerSandbox(_ WorkerSandbox, _ *exec.Cmd) error { return nil }
