//go:build darwin

package nativeexec

import (
	"errors"
	"fmt"
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
	if workspace == protected || strings.HasPrefix(protected, workspace+string(filepath.Separator)) ||
		strings.HasPrefix(workspace, protected+string(filepath.Separator)) {
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
	home, err = filepath.EvalSymlinks(home)
	if err != nil { return "", nil, fmt.Errorf("worker sandbox: resolve native provider home: %w", err) }
	workerTemp := filepath.Join(workspace, ".worker-tmp")
	if err := os.MkdirAll(workerTemp, 0700); err != nil {
		return "", nil, errors.New("worker sandbox: create private Worker temp directory")
	}
	// Do not grant entire /Library, /private/etc or /private/tmp: those
	// locations can hold the co-located Server's SQLite/HMAC state.
	reads := []string{workerTemp, "/opt/homebrew/bin", "/opt/homebrew/lib",
		"/private/var/db/dyld", "/System", "/usr/bin", "/usr/lib", "/bin",
		"/sbin", "/Library/Frameworks", "/Library/Developer", "/Applications",
		"/private/etc/ssl", "/dev", workspace, executable}
	// A configured Node root that is itself a parent of an essential OS
	// library/package mount cannot be masked without breaking native CLI
	// startup. Refuse launch rather than silently exposing its files.
	for _, allowedRoot := range reads {
		if protected == allowedRoot || strings.HasPrefix(allowedRoot,protected+string(filepath.Separator)) {
			return "",nil,errors.New("worker sandbox: protected Node root overlaps a required system runtime directory")
		}
	}
	writes := []string{workspace, workerTemp}
	for _, name := range []string{".codex", ".claude", ".claude.json"} {
		state := filepath.Join(home, name)
		info, statErr := os.Lstat(state)
		if os.IsNotExist(statErr) { continue }
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", nil, errors.New("worker sandbox: native state must not be a symlink")
		}
		if state == protected || strings.HasPrefix(protected, state+string(filepath.Separator)) ||
			strings.HasPrefix(state, protected+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: native state overlaps Node data")
		}
		reads = append(reads, state)
		writes = append(writes, state)
	}
	if policy.WritableDataHome != "" {
		state, err := filepath.EvalSymlinks(policy.WritableDataHome)
		if err != nil { return "", nil, err }
		if state == protected || strings.HasPrefix(protected, state+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: native provider state includes protected Node root")
		}
		// A narrow, purpose-built OpenCode store may be inside the Node
		// parent folder: only that explicit subtree is granted, not the
		// credential and registry files alongside it.
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
	// macOS dyld/shell startup reads the root directory itself. Granting
	// the literal "/" does not grant its descendants, unlike subpath("/").
	profile.WriteString("(allow file-read* (literal \"/\"))\n")
	for _, root := range reads {
		profile.WriteString("(allow file-read* " + quoted(root) + ")\n")
	}
	for _, root := range writes {
		profile.WriteString("(allow file-write* " + quoted(root) + ")\n")
	}
	// Most Node data dirs are not shared by any allow rule; the default-deny
	// profile already protects them while allowing an explicit, narrow
	// OpenCode store beneath the same parent. If the administrator places
	// Node state INSIDE an allowed system directory, add an explicit deny.
	// Fail closed rather than granting an overlapping OpenCode store there.
	if policy.ServerDataDir != "" {
		serverRoot, err := filepath.EvalSymlinks(policy.ServerDataDir)
		if err != nil { return "", nil, fmt.Errorf("worker sandbox: Server data root unavailable: %w",err) }
		if serverRoot == workspace || strings.HasPrefix(serverRoot,workspace+string(filepath.Separator)) {
			return "", nil, errors.New("worker sandbox: Worker workspace overlaps Server data")
		}
		for _, shared := range reads {
			if shared == serverRoot || strings.HasPrefix(serverRoot,shared+string(filepath.Separator)) {
				// Deny the Server root regardless of which allowlisted system
				// package subtree contains its SQLite secret material.
				profile.WriteString("(deny file-read* (subpath " + strconv.Quote(serverRoot) + "))\n")
				profile.WriteString("(deny file-write* (subpath " + strconv.Quote(serverRoot) + "))\n")
				break
			}
		}
	}
	exposed := false
	for _, root := range reads {
		if root == protected || strings.HasPrefix(protected, root+string(filepath.Separator)) {
			exposed = true
			break
		}
	}
	if exposed {
		if policy.WritableDataHome != "" {
			nativePath, nativeErr := filepath.EvalSymlinks(policy.WritableDataHome)
			if nativeErr != nil { return "", nil, nativeErr }
			if strings.HasPrefix(nativePath, protected+string(filepath.Separator)) {
				return "", nil, errors.New("worker sandbox: move native provider state out of a broadly shared Node system root")
			}
		}
		profile.WriteString("(deny file-read* (subpath " + strconv.Quote(protected) + "))\n")
		profile.WriteString("(deny file-write* (subpath " + strconv.Quote(protected) + "))\n")
	}
	return seatbelt, append([]string{"-p", profile.String(), executable}, args...), nil
}

func applyWorkerSandbox(_ WorkerSandbox, _ *exec.Cmd) error { return nil }
