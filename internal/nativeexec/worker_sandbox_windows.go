//go:build windows

package nativeexec

import (
	"errors"
	"encoding/base64"
	"encoding/json"
	"unicode/utf16"
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
	spec, err := json.Marshal(struct {
		Command string
		Arguments string
		Workspace string
	}{command, windowsArgumentLine(args), policy.Workspace})
	if err != nil { return "", nil, err }
	encodedSpec := base64.StdEncoding.EncodeToString(spec)
	psCommand := "$ErrorActionPreference = 'Stop'\ntry {\n    $raw = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:SECRETARY_WORKER_LAUNCH_SPEC))\n    $spec = $raw | ConvertFrom-Json\n    $p = New-Object System.Diagnostics.ProcessStartInfo\n    $p.FileName = [string]$spec.Command\n    $p.Arguments = [string]$spec.Arguments\n    $p.WorkingDirectory = [string]$spec.Workspace\n    $p.UseShellExecute = $false\n    $p.RedirectStandardInput = $true\n    $p.RedirectStandardOutput = $true\n    $p.RedirectStandardError = $true\n    $p.UserName = $env:SECRETARY_WORKER_USERNAME\n    $p.Domain = $env:SECRETARY_WORKER_DOMAIN\n    $securePassword = New-Object System.Security.SecureString\n    foreach ($ch in ($env:SECRETARY_WORKER_PASSWORD).ToCharArray()) { $securePassword.AppendChar($ch) }\n    $securePassword.MakeReadOnly()\n    $p.Password = $securePassword\n    $p.LoadUserProfile = $true\n    $p.EnvironmentVariables.Clear()\n    foreach ($entry in [Environment]::GetEnvironmentVariables().GetEnumerator()) {\n        $name = [string]$entry.Key\n        if ($name.StartsWith('SECRETARY_') -and $name -ne 'SECRETARY_ARTIFACT_BROKER_URL' -and -not $name.StartsWith('SECRETARY_CLAUDE_MCP_')) {\n            continue\n        }\n        $p.EnvironmentVariables[$name] = [string]$entry.Value\n    }\n\n    # A process tree must die with its proxy even when CommandContext kills\n    # the proxy without running its finally block. The kernel owns this rule.\n    Add-Type -TypeDefinition @'\nusing System;\nusing System.Runtime.InteropServices;\npublic static class SecretaryWorkerJob {\n [DllImport(\"kernel32.dll\", SetLastError=true, CharSet=CharSet.Unicode)]\n public static extern IntPtr CreateJobObject(IntPtr attributes, string name);\n [DllImport(\"kernel32.dll\", SetLastError=true)]\n public static extern bool SetInformationJobObject(IntPtr job, int infoClass, byte[] info, uint length);\n [DllImport(\"kernel32.dll\", SetLastError=true)]\n public static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);\n [DllImport(\"kernel32.dll\", SetLastError=true)]\n public static extern bool CloseHandle(IntPtr handle);\n}\n'@\n    $job = [SecretaryWorkerJob]::CreateJobObject([IntPtr]::Zero, $null)\n    if ($job -eq [IntPtr]::Zero) { throw 'CreateJobObject failed' }\n    $jobInfo = New-Object byte[] 144\n    $jobInfo[17] = 0x20 # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (0x2000)\n    if (-not [SecretaryWorkerJob]::SetInformationJobObject($job, 9, $jobInfo, 144)) {\n        throw 'cannot set JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE'\n    }\n    $child = [System.Diagnostics.Process]::Start($p)\n    if (-not $child) { throw 'native Worker child failed to start' }\n    if (-not [SecretaryWorkerJob]::AssignProcessToJobObject($job, $child.Handle)) {\n        $child.Kill()\n        throw 'AssignProcessToJobObject failed; native Worker killed'\n    }\n    $out = $child.StandardOutput.BaseStream.CopyToAsync([Console]::OpenStandardOutput())\n    $err = $child.StandardError.BaseStream.CopyToAsync([Console]::OpenStandardError())\n    $inputStream = [Console]::OpenStandardInput()\n    $inputTask = $inputStream.CopyToAsync($child.StandardInput.BaseStream)\n    $child.WaitForExit()\n    $out.GetAwaiter().GetResult()\n    $err.GetAwaiter().GetResult()\n    $exitStatus = $child.ExitCode\n    [void][SecretaryWorkerJob]::CloseHandle($job)\n    $job = [IntPtr]::Zero\n    exit $exitStatus\n} catch {\n    [Console]::Error.WriteLine('Worker launch blocked at line ' + $_.InvocationInfo.ScriptLineNumber + ': ' + $_.Exception.Message)\n    if ($job -and $job -ne [IntPtr]::Zero) {\n        [void][SecretaryWorkerJob]::CloseHandle($job)\n    }\n    exit 1\n}"
	utf16Command := utf16.Encode([]rune(psCommand))
	rawCommand := make([]byte,0,len(utf16Command)*2)
	for _, value := range utf16Command { rawCommand=append(rawCommand,byte(value),byte(value>>8)) }
	encodedPS := base64.StdEncoding.EncodeToString(rawCommand)
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		[]string{"-NoLogo","-NoProfile","-NonInteractive","-EncodedCommand",encodedPS,encodedSpec}, nil
}

func windowsArgumentLine(args []string) string {
	escaped := make([]string,0,len(args))
	for _, arg := range args { escaped = append(escaped,syscall.EscapeArg(arg)) }
	return strings.Join(escaped," ")
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
	if err := impersonateWorkerToken(token); err != nil { return err }
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
	if err := revertWorkerToken(); err != nil { return err }
	if guardErr != nil { return guardErr }
	if cmd.SysProcAttr == nil { cmd.SysProcAttr = &syscall.SysProcAttr{} }
	// The Go os/exec Token path uses CreateProcessAsUser, which is not
	// available to ordinary Windows Node users (SE_INCREASE_QUOTA absent).
	// A trusted PowerShell/.NET proxy uses CreateProcessWithLogonW instead,
	// retaining stream redirection while the child runs as the separate SID.
	if len(cmd.Args) < 2 { return errors.New("worker sandbox: missing launch specification") }
	spec := cmd.Args[len(cmd.Args)-1]
	cmd.Args = cmd.Args[:len(cmd.Args)-1]
	cmd.Env = append(cmd.Env,
		"SECRETARY_WORKER_LAUNCH_SPEC="+spec,
		"SECRETARY_WORKER_USERNAME="+username,
		"SECRETARY_WORKER_PASSWORD="+password,
		"SECRETARY_WORKER_DOMAIN="+domain,
		"SECRETARY_WORKER_HOME="+workerHome,
	)
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

func impersonateWorkerToken(token windows.Token) error {
	fn := syscall.NewLazyDLL("advapi32.dll").NewProc("ImpersonateLoggedOnUser")
	ok, _, callErr := fn.Call(uintptr(token))
	if ok == 0 {
		if callErr != syscall.Errno(0) { return callErr }
		return errors.New("worker sandbox: Windows impersonation denied")
	}
	return nil
}

func revertWorkerToken() error {
	fn := syscall.NewLazyDLL("advapi32.dll").NewProc("RevertToSelf")
	ok, _, callErr := fn.Call()
	if ok == 0 {
		if callErr != syscall.Errno(0) { return callErr }
		return errors.New("worker sandbox: Windows revert impersonation failed")
	}
	return nil
}
