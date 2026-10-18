//go:build windows

package nativeexec

import (
	"errors"
	"encoding/base64"
	"encoding/json"
 "context"
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
	}{command, windowsWorkerCommandLine(command,args), policy.Workspace})
	if err != nil { return "", nil, err }
	encodedSpec := base64.StdEncoding.EncodeToString(spec)
	psCommand := "$ErrorActionPreference = 'Stop'\ntry {\n    $raw = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:SECRETARY_WORKER_LAUNCH_SPEC))\n    $spec = $raw | ConvertFrom-Json\n    $p = New-Object System.Diagnostics.ProcessStartInfo\n    # Run a trusted, input-gated launcher as the alternate SID. The actual\n    # native CLI is not executed until *after* AssignProcessToJobObject.\n    # This closes the Process.Start -> job assignment escape window.\n    $stage = @'\n$ErrorActionPreference = 'Stop'\n$spec = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('@@SPEC@@')) | ConvertFrom-Json\n$env:SECRETARY_WORKER_STAGE = $null\n# Only the trusted Node proxy can signal this named kernel Event. The\n# Worker SID has Synchronize rights but no EventWaitHandle.Modify rights.\n$gateName='@@GATE@@'\n$gate=[System.Threading.EventWaitHandle]::OpenExisting(\n  $gateName,[System.Security.AccessControl.EventWaitHandleRights]::Synchronize)\nif (-not $gate.WaitOne(30000)) {\n  [Console]::Error.WriteLine('Worker job-assignment event timed out')\n  exit 77\n}\n$gate.Dispose()\n$pipe = [Console]::OpenStandardInput()\n$native = New-Object System.Diagnostics.ProcessStartInfo\n$native.FileName = [string]$spec.Command\n$native.Arguments = [string]$spec.Arguments\n$native.WorkingDirectory = [string]$spec.Workspace\n$native.UseShellExecute = $false\n$native.RedirectStandardInput = $true\n$native.RedirectStandardOutput = $true\n$native.RedirectStandardError = $true\n$process = [System.Diagnostics.Process]::Start($native)\nif (-not $process) { throw 'Worker binary failed to launch after job assignment' }\n$stdout = $process.StandardOutput.BaseStream.CopyToAsync([Console]::OpenStandardOutput())\n$stderr = $process.StandardError.BaseStream.CopyToAsync([Console]::OpenStandardError())\n$stdin = $pipe.CopyToAsync($process.StandardInput.BaseStream)\n$process.WaitForExit()\n$stdout.GetAwaiter().GetResult()\n$stderr.GetAwaiter().GetResult()\nexit $process.ExitCode\n'@\n    $stageSpec = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($raw))\n    $stage = $stage.Replace('@@SPEC@@', $stageSpec)\n    $gateName='Local\\SecretaryT09-'+[Guid]::NewGuid().ToString('N')\n    $eventSecurity = New-Object System.Security.AccessControl.EventWaitHandleSecurity\n    $world = New-Object System.Security.Principal.SecurityIdentifier('S-1-1-0')\n    $currentSid=[System.Security.Principal.WindowsIdentity]::GetCurrent().User\n    $syncRule = New-Object System.Security.AccessControl.EventWaitHandleAccessRule(\n      $world,[System.Security.AccessControl.EventWaitHandleRights]::Synchronize,\n      [System.Security.AccessControl.AccessControlType]::Allow)\n    $ownerRule = New-Object System.Security.AccessControl.EventWaitHandleAccessRule(\n      $currentSid,[System.Security.AccessControl.EventWaitHandleRights]::FullControl,\n      [System.Security.AccessControl.AccessControlType]::Allow)\n    $eventSecurity.AddAccessRule($syncRule)\n    $eventSecurity.AddAccessRule($ownerRule)\n    $createdGate=$false\n    $gate=[System.Threading.EventWaitHandle]::new(\n      $false,[System.Threading.EventResetMode]::ManualReset,\n      $gateName,[ref]$createdGate,$eventSecurity)\n    $stage = $stage.Replace('@@GATE@@', $gateName)\n    # Keep the CreateProcessWithLogonW command line short; the gated native\n    # stage is passed in a private child-only environment variable.\n    $stageEncoded = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($stage))\n    $launchStub = '[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($env:SECRETARY_WORKER_STAGE)) | Invoke-Expression'\n    $stubEncoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($launchStub))\n    $p.FileName = [IO.Path]::Combine($env:SystemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe')\n    $p.Arguments = '-NoLogo -NoProfile -NonInteractive -EncodedCommand ' + $stubEncoded\n    $p.WorkingDirectory = [string]$spec.Workspace\n    $p.UseShellExecute = $false\n    $p.RedirectStandardInput = $true\n    $p.RedirectStandardOutput = $true\n    $p.RedirectStandardError = $true\n    $p.UserName = $env:SECRETARY_WORKER_USERNAME\n    $p.Domain = $env:SECRETARY_WORKER_DOMAIN\n    $securePassword = New-Object System.Security.SecureString\n    foreach ($ch in ($env:SECRETARY_WORKER_PASSWORD).ToCharArray()) { $securePassword.AppendChar($ch) }\n    $securePassword.MakeReadOnly()\n    $p.Password = $securePassword\n    $p.LoadUserProfile = $true\n    $p.EnvironmentVariables.Clear()\n    foreach ($entry in [Environment]::GetEnvironmentVariables().GetEnumerator()) {\n        $name = [string]$entry.Key\n        if ($name.StartsWith('SECRETARY_') -and $name -ne 'SECRETARY_ARTIFACT_BROKER_URL' -and -not $name.StartsWith('SECRETARY_CLAUDE_MCP_')) {\n            continue\n        }\n        $p.EnvironmentVariables[$name] = [string]$entry.Value\n    }\n    $p.EnvironmentVariables['SECRETARY_WORKER_STAGE'] = $stageEncoded\n\n    # A process tree must die with its proxy even when CommandContext kills\n    # the proxy without running its finally block. The kernel owns this rule.\n    Add-Type -TypeDefinition @'\nusing System;\nusing System.Runtime.InteropServices;\npublic static class SecretaryWorkerJob {\n [DllImport(\"kernel32.dll\", SetLastError=true, CharSet=CharSet.Unicode)]\n public static extern IntPtr CreateJobObject(IntPtr attributes, string name);\n [DllImport(\"kernel32.dll\", SetLastError=true)]\n public static extern bool SetInformationJobObject(IntPtr job, int infoClass, byte[] info, uint length);\n [DllImport(\"kernel32.dll\", SetLastError=true)]\n public static extern bool AssignProcessToJobObject(IntPtr job, IntPtr process);\n [DllImport(\"kernel32.dll\", SetLastError=true)]\n public static extern bool CloseHandle(IntPtr handle);\n}\n'@\n    $job = [SecretaryWorkerJob]::CreateJobObject([IntPtr]::Zero, $null)\n    if ($job -eq [IntPtr]::Zero) { throw 'CreateJobObject failed' }\n    $jobInfo = New-Object byte[] 144\n    $jobInfo[17] = 0x20 # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (0x2000)\n    if (-not [SecretaryWorkerJob]::SetInformationJobObject($job, 9, $jobInfo, 144)) {\n        throw 'cannot set JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE'\n    }\n    $child = [System.Diagnostics.Process]::Start($p)\n    if (-not $child) { throw 'native Worker child failed to start' }\n    if (-not [SecretaryWorkerJob]::AssignProcessToJobObject($job, $child.Handle)) {\n        $child.Kill()\n        throw 'AssignProcessToJobObject failed; native Worker killed'\n    }\n    # Release gate only after process is definitely assigned to the kernel\n    # Job Object; all subsequently spawned CLI descendants inherit the job.\n    [void]$gate.Set()\n    $gate.Dispose()\n    $out = $child.StandardOutput.BaseStream.CopyToAsync([Console]::OpenStandardOutput())\n    $err = $child.StandardError.BaseStream.CopyToAsync([Console]::OpenStandardError())\n    $inputStream = [Console]::OpenStandardInput()\n    $inputTask = $inputStream.CopyToAsync($child.StandardInput.BaseStream)\n    $child.WaitForExit()\n    $out.GetAwaiter().GetResult()\n    $err.GetAwaiter().GetResult()\n    $exitStatus = $child.ExitCode\n    [void][SecretaryWorkerJob]::CloseHandle($job)\n    $job = [IntPtr]::Zero\n    exit $exitStatus\n} catch {\n    [Console]::Error.WriteLine('Worker launch blocked at line ' + $_.InvocationInfo.ScriptLineNumber + ': ' + $_.Exception.Message)\n    if ($job -and $job -ne [IntPtr]::Zero) {\n        [void][SecretaryWorkerJob]::CloseHandle($job)\n    }\n    exit 1\n}"
	utf16Command := utf16.Encode([]rune(psCommand))
	rawCommand := make([]byte,0,len(utf16Command)*2)
	for _, value := range utf16Command { rawCommand=append(rawCommand,byte(value),byte(value>>8)) }
	encodedPS := base64.StdEncoding.EncodeToString(rawCommand)
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
		[]string{"-NoLogo","-NoProfile","-NonInteractive","-EncodedCommand",encodedPS,encodedSpec}, nil
}

// cmd.exe .cmd shims require their already-quoted /S /C command string.
 // Re-quoting this string with Windows argv rules changes npm arguments.
func windowsWorkerCommandLine(command string,args []string) string {
	if strings.EqualFold(filepath.Base(command),"cmd.exe") &&
		len(args)==4 && args[0]=="/d" && args[1]=="/s" && args[2]=="/c" {
		return "/d /s /c " + args[3]
	}
	return windowsArgumentLine(args)
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
	workerHomeConfig := strings.TrimSpace(os.Getenv("SECRETARY_WORKER_HOME"))
	if username=="" || password=="" || workerHomeConfig=="" {
		// Offline one-time Windows provisioning persists configuration only
		// inside the ACL-protected Node directory, never in Worker env.
		raw,readErr:=os.ReadFile(filepath.Join(policy.ProtectedDir,"worker-account.json"))
		if readErr==nil {
			var saved struct { Username string `json:"username"`; Password string `json:"password"`; Domain string `json:"domain"`; Home string `json:"home"` }
			if json.Unmarshal(raw,&saved)!=nil {return errors.New("worker sandbox: invalid protected worker-account.json")}
			if username=="" {username=saved.Username}
			if password=="" {password=saved.Password}
			if domain=="" {domain=saved.Domain}
			if workerHomeConfig=="" {workerHomeConfig=saved.Home}
		}
	}
	if username == "" || password == "" {
		return errors.New("worker sandbox: secure Windows Worker account is not provisioned; run the Windows Node setup/preflight and migrate provider state before Dispatch")
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
	workerHome := workerHomeConfig
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
	// Migrate the current Node user's existing Codex/Claude auth and
	// checkpoint history into the separate account's persistent profile.
	// Copy only selected provider trees, never Node identity or Server DB.
	legacyHome, homeErr := os.UserHomeDir()
	if homeErr != nil { return homeErr }
	if err := migrateWindowsNativeProviderHome(legacyHome, workerHome); err != nil { return err }
	if cmd.SysProcAttr == nil { cmd.SysProcAttr = &syscall.SysProcAttr{} }
	// The Go os/exec Token path uses CreateProcessAsUser, which is not
	// available to ordinary Windows Node users (SE_INCREASE_QUOTA absent).
	// A trusted PowerShell/.NET proxy uses CreateProcessWithLogonW instead,
	// retaining stream redirection while the child runs as the separate SID.
	if len(cmd.Args) < 2 { return errors.New("worker sandbox: missing launch specification") }
	spec := cmd.Args[len(cmd.Args)-1]
	cmd.Args = cmd.Args[:len(cmd.Args)-1]
	cmd.Env = windowsWorkerProfileEnvironment(cmd.Env, workerHome)
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

// WindowsWorkerHome loads the persisted isolated Worker profile location
// from the protected Node state when environment overrides are absent.
func WindowsWorkerHome(ctx context.Context) (string,error) {
	policy,ok:=workerSandboxFromContext(ctx)
	if !ok{return "",nil}
	home:=strings.TrimSpace(os.Getenv("SECRETARY_WORKER_HOME"))
	if home!="" {return home,nil}
	raw,err:=os.ReadFile(filepath.Join(policy.ProtectedDir,"worker-account.json"))
	if err!=nil{return "",fmt.Errorf("Worker profile not provisioned: %w",err)}
	var config struct {Home string}
	if err:=json.Unmarshal(raw,&config);err!=nil{return "",err}
	if !filepath.IsAbs(config.Home){return "",errors.New("Windows Worker home must be absolute")}
	return config.Home,nil
}
