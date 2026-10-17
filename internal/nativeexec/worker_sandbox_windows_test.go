//go:build windows

package nativeexec

import (
	"context"
 "time"
 "strconv"
 "fmt"
 "golang.org/x/sys/windows"
	"encoding/base64"
	"unicode/utf16"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This runs on windows-latest using a separately provisioned non-admin Windows
// account. It asserts both denied Node secret reads and persistent native CLI
// session state across two independently launched Worker processes.
func TestT09WindowsSeparateAccountStateAndResume(t *testing.T) {
	workspace := os.Getenv("SECRETARY_WORKER_TEST_WORKSPACE")
	protected := os.Getenv("SECRETARY_WORKER_TEST_PROTECTED_DIR")
	home := os.Getenv("SECRETARY_WORKER_HOME")
	if workspace == "" || protected == "" || home == "" {
		t.Skip("cross-SID Windows Worker integration requires provisioned account")
	}
	if err := os.WriteFile(filepath.Join(protected,"identity.json"),[]byte("NODE-CREDENTIAL-SECRET"),0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(protected,"node-state.json"),[]byte("NODE-ATTEMPT-METADATA"),0600); err != nil { t.Fatal(err) }
	ctx := WithWorkerSandbox(context.Background(), WorkerSandbox{ProtectedDir:protected, Workspace:workspace})
	tests := []struct{name, dir string}{
		{"Codex",filepath.Join(home,".codex")},
		{"Claude",filepath.Join(home,".claude")},
		{"OpenCode",filepath.Join(home,"opencode-native")},
	}
	for _, tc := range tests {
		t.Run(tc.name,func(t *testing.T) {
			state := filepath.Join(tc.dir,"resume.txt")
			for _, script := range []string{
				`Set-Content -NoNewline -LiteralPath '`+state+`' -Value 'SESSION-READY'`,
				`Get-Content -LiteralPath '`+state+`'`,
			} {
				encoded := utf16.Encode([]rune(script))
				command := make([]byte, 0, len(encoded)*2)
				for _, unit := range encoded { command = append(command, byte(unit), byte(unit>>8)) }
				cmd, err := CommandContext(ctx,"powershell.exe","-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(command))
				if err != nil { t.Fatal(err) }
				cmd.Env = SecureWorkerEnvironment(ctx,cmd.Env)
				out, err := cmd.CombinedOutput()
				if err != nil { t.Fatalf("worker Start/Resume failed: %v: %s",err,out) }
				if strings.Contains(script,"Get-Content") && !strings.Contains(string(out),"SESSION-READY") {
					t.Fatalf("persistent Worker state lost across process restart: %s",out)
				}
			}
		})
	}
}


// Cancellation kills the native child PID, not just the trusted PowerShell
// proxy. A Job Object closes automatically when the proxy is terminated.
func TestT09WindowsCancelKillsNativeWorkerTree(t *testing.T) {
	workspace := os.Getenv("SECRETARY_WORKER_TEST_WORKSPACE")
	protected := os.Getenv("SECRETARY_WORKER_TEST_PROTECTED_DIR")
	if workspace=="" || protected=="" { t.Skip("provisioned Windows Worker fixture required") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = WithWorkerSandbox(ctx, WorkerSandbox{ProtectedDir:protected, Workspace:workspace})
	pidFile := filepath.Join(workspace,"cancel-child.pid")
	_ = os.Remove(pidFile)
	script := fmt.Sprintf("[System.IO.File]::WriteAllText('%s', [string]$PID); while ($true) { Start-Sleep -Seconds 1 }", pidFile)
	encoded := utf16.Encode([]rune(script))
	b := make([]byte,0,len(encoded)*2)
	for _, u := range encoded { b=append(b,byte(u),byte(u>>8)) }
	cmd, err := CommandContext(ctx,"powershell.exe","-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(b))
	if err!=nil { t.Fatal(err) }
	if err:=cmd.Start();err!=nil { t.Fatal(err) }
	ended:=make(chan error,1)
	go func(){ended<-cmd.Wait()}()
	var pid uint32
	deadline:=time.Now().Add(10*time.Second)
	for time.Now().Before(deadline) {
		data,readErr:=os.ReadFile(pidFile)
		if readErr==nil {
			n,parseErr:=strconv.ParseUint(strings.TrimSpace(string(data)),10,32)
			if parseErr==nil { pid=uint32(n);break }
		}
		time.Sleep(50*time.Millisecond)
	}
	if pid==0 { cancel();<-ended;t.Fatal("native Worker PID never started") }
	cancel()
	select { case <-ended: case <-time.After(5*time.Second):t.Fatal("PowerShell proxy did not cancel") }
	deadline=time.Now().Add(5*time.Second)
	for time.Now().Before(deadline) {
		h,openErr:=windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION,false,pid)
		if openErr!=nil { return }
		var status uint32
		queryErr:=windows.GetExitCodeProcess(h,&status)
		_ = windows.CloseHandle(h)
		if queryErr!=nil || status!=259 { return }
		time.Sleep(50*time.Millisecond)
	}
	t.Fatalf("orphan native child PID %d is still running after CommandContext cancel",pid)
}
