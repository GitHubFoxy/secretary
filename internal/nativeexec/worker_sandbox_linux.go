//go:build linux

package nativeexec

import (
	"debug/elf"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
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
		if err := mountRuntimeExecutable(&wrapped, runtime); err != nil { return "", nil, err }
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
	if err := mountRuntimeExecutable(&wrapped, executable); err != nil { return "", nil, err }
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

// A root-provisioned, immutable manifest records byte digests from a
// verified provider distribution. The runtime never trusts a manifest
// obtained from the user-writable package tree.
type cliRuntimeManifest struct {
 Provider string `json:"provider"`
 Version string `json:"version"`
 Files map[string]string `json:"files"`
}

func mountRuntimePackageFiles(wrapped *[]string, packageRoot string) error {
 packageName := filepath.Base(packageRoot)
 parent := filepath.Base(filepath.Dir(packageRoot))
 if strings.HasPrefix(parent, "@") { packageName = parent + "/" + packageName }
 provider := ""
 switch packageName {
 case "@openai/codex": provider="codex"
 case "@anthropic-ai/claude-code": provider="claude"
 case "opencode-ai": provider="opencode"
 default:
  // Other CLI layouts must be reviewed and provisioned explicitly.
  return fmt.Errorf("worker sandbox: unsupported CLI package %q",packageName)
 }
 packageJSON, err:=os.ReadFile(filepath.Join(packageRoot,"package.json"))
 if err!=nil{return fmt.Errorf("worker sandbox: package identity missing: %w",err)}
 var metadata struct {Name string `json:"name"`; Version string `json:"version"`}
 if err=json.Unmarshal(packageJSON,&metadata);err!=nil{return err}
 if metadata.Name!=packageName || metadata.Version=="" || strings.ContainsAny(metadata.Version,"/\\\\.") && strings.Contains(metadata.Version,"..") {
   return errors.New("worker sandbox: invalid CLI package identity")
 }
 // Root ownership is mandatory even when the Node and Server use the same UID.
 // Provision this outside user-writable npm caches, with a package archive
 // verified against its published registry integrity before hashing files.
 manifestPath:=filepath.Join("/etc/secretary/cli-manifests",provider,metadata.Version+".json")
 info,err:=os.Lstat(manifestPath)
 if err!=nil{return fmt.Errorf("worker sandbox: trusted CLI provenance unavailable: %w",err)}
 stat,ok:=info.Sys().(*syscall.Stat_t)
 if !ok || stat.Uid!=0 || info.Mode()&os.ModeSymlink!=0 || info.Mode().Perm()&022!=0 {
   return errors.New("worker sandbox: untrusted CLI provenance manifest")
 }
 bytes,err:=os.ReadFile(manifestPath);if err!=nil{return err}
 var manifest cliRuntimeManifest
 if err=json.Unmarshal(bytes,&manifest);err!=nil{return err}
 if manifest.Provider!=packageName || manifest.Version!=metadata.Version || len(manifest.Files)==0 || len(manifest.Files)>8192 {
   return errors.New("worker sandbox: CLI provenance identity or file count mismatch")
 }
 // Only signed-off paths in the published package are allowed. A malicious
 // same-extension file, including transport-secret.js or .wasm, cannot enter.
 for rel,expected:=range manifest.Files {
   if rel=="" || filepath.IsAbs(rel) || filepath.Clean(rel)!=rel || rel==".." || strings.HasPrefix(rel,"../") ||
     strings.Contains(rel,"\\\\") || len(expected)!=64 {
      return errors.New("worker sandbox: invalid CLI manifest entry")
   }
   file:=filepath.Join(packageRoot,rel)
   info,err:=os.Lstat(file);if err!=nil{return err}
   if !info.Mode().IsRegular(){return errors.New("worker sandbox: nonregular CLI asset")}
   fd,err:=os.Open(file);if err!=nil{return err}
   digest:=sha256.New()
   if info.Size()>128<<20 { fd.Close(); return errors.New("worker sandbox: CLI asset exceeds size limit") }
   _,copyErr:=io.Copy(digest,fd)
   closeErr:=fd.Close()
   if copyErr!=nil{return copyErr};if closeErr!=nil{return closeErr}
   wanted,err:=hex.DecodeString(expected)
   if err!=nil || !equalDigests(digest.Sum(nil),wanted){return fmt.Errorf("worker sandbox: CLI package content mismatches trusted manifest: %s",rel)}
   if err:=mountRuntimeFile(wrapped,file);err!=nil{return err}
 }
 return nil
}

func equalDigests(a,b []byte) bool {
 if len(a)!=len(b){return false}
 var value byte
 for i:=range a { value |= a[i]^b[i] }
 return value==0
}

// Recursively expose the ELF interpreter and DT_NEEDED shared libraries as
// individual files. Do not export the parent lib directory: it may contain
// an unrelated Server database owned by the same UID.
func mountRuntimeExecutable(wrapped *[]string, executable string) error {
  if _, err := os.Lstat(executable); os.IsNotExist(err) {
    // Optional runtime candidates differ by distribution. The actual
    // selected command must still exist (checked by sandboxInvocation).
    return nil
  } else if err != nil { return err }
  seen := make(map[string]bool)
  var visit func(string) error
  visit = func(path string) error {
    resolved, err := filepath.EvalSymlinks(path)
    if err != nil { return err }
    if seen[resolved] { return mountRuntimeFile(wrapped,path) }
    seen[resolved] = true
    if err := mountRuntimeFile(wrapped,path); err != nil { return err }
    binary, err := elf.Open(resolved)
    if err != nil { return nil } // scripts/assets are bound as files
    defer binary.Close()
    for _,prog := range binary.Progs {
      if prog.Type != elf.PT_INTERP { continue }
      buf := make([]byte,prog.Filesz)
      if _,err:=prog.ReadAt(buf,0);err!=nil {return err}
      interpreter:=strings.TrimRight(string(buf),"\x00")
      if err:=visit(interpreter);err!=nil{return err}
    }
    libs,err:=binary.ImportedLibraries()
    if err!=nil {return fmt.Errorf("worker sandbox: ELF dependency list: %w",err)}
    for _,lib:=range libs {
      // Resolve against trusted architecture-specific linker roots only.
      // Search includes typical Debian/Ubuntu, Fedora, and Alpine layouts.
      found:=""
      for _,dir:=range []string{"/lib/x86_64-linux-gnu","/usr/lib/x86_64-linux-gnu",
        "/lib/aarch64-linux-gnu","/usr/lib/aarch64-linux-gnu",
        "/lib64","/usr/lib64","/lib","/usr/lib"} {
        candidate:=filepath.Join(dir,lib)
        if info,e:=os.Stat(candidate);e==nil&&info.Mode().IsRegular(){found=candidate;break}
      }
      if found=="" {return fmt.Errorf("worker sandbox: unresolved runtime library %q for %s",lib,path)}
      if err:=visit(found);err!=nil{return err}
    }
    return nil
  }
  return visit(executable)
}
