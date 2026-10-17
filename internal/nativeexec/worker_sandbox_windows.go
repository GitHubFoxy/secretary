//go:build windows

package nativeexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows isolates native Worker shells and child processes with a separate
// non-administrative OS account, not with a restricted environment variable.
// The Node account's private data directory must deny this separate SID.
var windowsWorkerToken struct {
	sync.Mutex
	key string
	token windows.Token
	home string
}

func sandboxInvocation(policy WorkerSandbox, command string, args []string) (string, []string, error) {
	if policy.ProtectedDir == "" || policy.Workspace == "" {
		return "", nil, errors.New("worker sandbox: Node private directory and Worker workspace required")
	}
	return command, args, nil
}

func applyWorkerSandbox(policy WorkerSandbox, cmd *exec.Cmd) error {
	username := strings.TrimSpace(os.Getenv("SECRETARY_WORKER_USERNAME"))
	password := os.Getenv("SECRETARY_WORKER_PASSWORD")
	domain := strings.TrimSpace(os.Getenv("SECRETARY_WORKER_DOMAIN"))
	if username == "" || password == "" {
		return errors.New("worker sandbox: a separate Windows Worker account must be configured")
	}
	if domain == "" { domain = "." }
	key := domain + "\\" + username
	windowsWorkerToken.Lock()
	defer windowsWorkerToken.Unlock()
	if windowsWorkerToken.token == 0 || windowsWorkerToken.key != key {
		if windowsWorkerToken.token != 0 { _ = windowsWorkerToken.token.Close() }
		token, err := logonWindowsWorker(username, domain, password)
		if err != nil { return fmt.Errorf("worker sandbox: Windows Worker account logon failed: %w", err) }
		windowsWorkerToken.token = token
		windowsWorkerToken.key = key
	}
	token := windowsWorkerToken.token
	if token.IsElevated() {
		return errors.New("worker sandbox: privileged Windows Worker account is forbidden")
	}
	workerHome := strings.TrimSpace(os.Getenv("SECRETARY_WORKER_HOME"))
	if workerHome == "" || !filepath.IsAbs(workerHome) {
		return errors.New("worker sandbox: persistent SECRETARY_WORKER_HOME is required")
	}
	if filepath.Clean(workerHome) == filepath.Clean(policy.ProtectedDir) ||
		strings.HasPrefix(strings.ToLower(filepath.Clean(policy.ProtectedDir)), strings.ToLower(filepath.Clean(workerHome)+string(filepath.Separator))) {
		return errors.New("worker sandbox: private Node data overlaps Worker home")
	}
	trustedUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil { return err }
	workerUser, err := token.GetTokenUser()
	if err != nil { return err }
	if trustedUser.User.Sid.Equals(workerUser.User.Sid) {
		return errors.New("worker sandbox: Worker and Node use the same Windows account")
	}
	// Validate access as the actual Worker SID before starting any native
	// process. A world-readable identity/state directory is a hard error.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.ImpersonateLoggedOnUser(token); err != nil { return err }
	var guardErr error
	if handle, readErr := os.Open(policy.ProtectedDir); readErr == nil {
		_ = handle.Close()
		guardErr = errors.New("worker sandbox: Node data directory readable by Worker account")
	}
	if guardErr == nil {
		// Credentials may be stored in direct files under protected root;
		// read-denial must be enforced by the actual Windows ACL.
		for _, filename := range []string{"identity.json", "node-state.json"} {
			if handle, readErr := os.Open(filepath.Join(policy.ProtectedDir, filename)); readErr == nil {
				_ = handle.Close()
				guardErr = errors.New("worker sandbox: Node credential or state readable by Worker account")
				break
			}
		}
	}
	if guardErr == nil {
		fd, homeErr := os.Open(workerHome)
		if homeErr != nil {
			guardErr = fmt.Errorf("worker sandbox: Windows Worker home inaccessible to separate account: %w", homeErr)
		} else { _ = fd.Close() }
	}
	if guardErr == nil {
		fd, workspaceErr := os.Open(policy.Workspace)
		if workspaceErr != nil {
			guardErr = fmt.Errorf("worker sandbox: Worker account cannot access assigned workspace: %w", workspaceErr)
		} else { _ = fd.Close() }
	}
	if err := windows.RevertToSelf(); err != nil { return err }
	if guardErr != nil { return guardErr }
	if cmd.SysProcAttr == nil { cmd.SysProcAttr = &syscall.SysProcAttr{} }
	cmd.SysProcAttr.Token = syscall.Token(token)
	return nil
}

func logonWindowsWorker(username, domain, password string) (windows.Token, error) {
	user, err := syscall.UTF16PtrFromString(username)
	if err != nil { return 0, err }
	dom, err := syscall.UTF16PtrFromString(domain)
	if err != nil { return 0, err }
	pass, err := syscall.UTF16PtrFromString(password)
	if err != nil { return 0, err }
	var token syscall.Token
	fn := syscall.NewLazyDLL("advapi32.dll").NewProc("LogonUserW")
	// LOGON32_LOGON_INTERACTIVE=2, LOGON32_PROVIDER_DEFAULT=0.
	success, _, callErr := fn.Call(
		uintptr(unsafe.Pointer(user)), uintptr(unsafe.Pointer(dom)),
		uintptr(unsafe.Pointer(pass)), 2, 0, uintptr(unsafe.Pointer(&token)),
	)
	if success == 0 {
		if callErr != syscall.Errno(0) { return 0, callErr }
		return 0, errors.New("Windows Worker logon rejected")
	}
	return windows.Token(token), nil
}
