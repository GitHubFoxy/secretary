//go:build windows

package nativeexec

import (
	"context"
 "encoding/json"
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


func TestT09WindowsNativeProviderStateMigrationPreservesHistory(t *testing.T) {
	root:=t.TempDir()
	legacy:=filepath.Join(root,"legacy")
	worker:=filepath.Join(root,"worker-home")
	for _,name:=range []string{".codex",".claude","opencode-private"}{
		if err:=os.MkdirAll(filepath.Join(legacy,name),0700);err!=nil{t.Fatal(err)}
		if err:=os.WriteFile(filepath.Join(legacy,name,"auth.json"),[]byte("session-"+name),0600);err!=nil{t.Fatal(err)}
	}
	if err:=os.WriteFile(filepath.Join(legacy,".claude.json"),[]byte("{\"history\":1}"),0600);err!=nil{t.Fatal(err)}
	if err:=migrateWindowsNativeProviderHome(legacy,worker);err!=nil{t.Fatal(err)}
	if err:=MigrateWindowsNativeStore(filepath.Join(legacy,"opencode-private"),filepath.Join(worker,"opencode-native"));err!=nil{t.Fatal(err)}
	for _,name:=range []string{".codex",".claude","opencode-native"}{
		b,err:=os.ReadFile(filepath.Join(worker,name,"auth.json"));if err!=nil||!strings.Contains(string(b),"session-"){t.Fatalf("%s auth/history not migrated: %q %v",name,b,err)}
	}
	b,err:=os.ReadFile(filepath.Join(worker,".claude.json"))
	if err!=nil||!strings.Contains(string(b),"history"){t.Fatalf("Claude config not migrated: %s %v",b,err)}
}

func TestT09WindowsPreexistingEmptyProviderDirectoriesAreMigrated(t *testing.T) {
	root:=t.TempDir()
	src:=filepath.Join(root,"legacy")
	dst:=filepath.Join(root,"new-home")
	for _,name:=range []string{".codex",".claude"}{
		_ = os.MkdirAll(filepath.Join(src,name),0700)
		_ = os.MkdirAll(filepath.Join(dst,name),0700)
		if err:=os.WriteFile(filepath.Join(src,name,"history.json"),[]byte("OLD-HISTORY"),0600);err!=nil{t.Fatal(err)}
	}
	if err:=migrateWindowsNativeProviderHome(src,dst);err!=nil{t.Fatal(err)}
	for _,name:=range []string{".codex",".claude"} {
		body,err:=os.ReadFile(filepath.Join(dst,name,"history.json"))
		if err!=nil||string(body)!="OLD-HISTORY"{t.Fatalf("lost %s existing native history: %q %v",name,body,err)}
	}
}


func TestT09WindowsPersistedAccountConfigWithoutEnvironment(t *testing.T) {
	workspace:=os.Getenv("SECRETARY_WORKER_TEST_WORKSPACE")
	protected:=os.Getenv("SECRETARY_WORKER_TEST_PROTECTED_DIR")
	home:=os.Getenv("SECRETARY_WORKER_HOME")
	user:=os.Getenv("SECRETARY_WORKER_USERNAME")
	password:=os.Getenv("SECRETARY_WORKER_PASSWORD")
	if workspace==""||protected==""||home==""||user==""||password==""{t.Skip("separate SID fixture missing")}
	credentials,err:=json.Marshal(map[string]string{"username":user,"password":password,"domain":".","home":home})
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(protected,"worker-account.json"),credentials,0600);err!=nil{t.Fatal(err)}
	// Simulate an existing Node upgrade: legacy provider state is present
	// under the original profile, while the new Worker home was already
	// provisioned. The real dispatch preflight must migrate it safely.
	legacyHome:=filepath.Join(workspace,"original-node-profile")
	for _,provider:=range []string{".codex",".claude"}{
		dir:=filepath.Join(legacyHome,provider)
		if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
		if err:=os.WriteFile(filepath.Join(dir,"t09-upgrade-checkpoint"),[]byte("PREEXISTING-SESSION"),0600);err!=nil{t.Fatal(err)}
	}
	t.Setenv("USERPROFILE",legacyHome)
	t.Setenv("SECRETARY_WORKER_USERNAME","")
	t.Setenv("SECRETARY_WORKER_PASSWORD","")
	t.Setenv("SECRETARY_WORKER_HOME","")
	t.Setenv("SECRETARY_WORKER_DOMAIN","")
	if err:=PreflightWorkerSandbox(protected);err!=nil{t.Fatalf("persisted Worker account preflight/dispatch failed: %v",err)}
	for _,provider:=range []string{".codex",".claude"}{
		content,err:=os.ReadFile(filepath.Join(home,provider,"t09-upgrade-checkpoint"))
		if err!=nil||string(content)!="PREEXISTING-SESSION"{
			t.Fatalf("existing %s checkpoint not preserved during account migration: %v %q",provider,err,content)
		}
	}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:protected,Workspace:home})
	script:="[Console]::WriteLine($env:USERPROFILE)"
	units:=utf16.Encode([]rune(script))
	payload:=make([]byte,0,2*len(units))
	for _,u:=range units{payload=append(payload,byte(u),byte(u>>8))}
	cmd,err:=CommandContext(ctx,"powershell.exe","-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(payload))
	if err!=nil{t.Fatal(err)}
	out,err:=cmd.CombinedOutput()
	if err!=nil||!strings.Contains(strings.ToLower(string(out)),strings.ToLower(home)){
		t.Fatalf("persisted Worker config failed to isolate USERPROFILE: %v %s",err,out)
	}
}


func TestT09WindowsProjectlessWorkspaceStaysInProvisionedWorkerHome(t *testing.T) {
	home:=os.Getenv("SECRETARY_WORKER_HOME")
	protected:=os.Getenv("SECRETARY_WORKER_TEST_PROTECTED_DIR")
	user:=os.Getenv("SECRETARY_WORKER_USERNAME")
	password:=os.Getenv("SECRETARY_WORKER_PASSWORD")
	if home==""||protected==""||user==""||password==""{t.Skip("separate SID fixture required")}
	encoded,err:=json.Marshal(map[string]string{"username":user,"password":password,"domain":".","home":home})
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(protected,"worker-account.json"),encoded,0600);err!=nil{t.Fatal(err)}
	t.Setenv("SECRETARY_WORKER_HOME","")
	root,err:=ManagedWorkerWorkspaceRoot(protected)
	if err!=nil{t.Fatal(err)}
	want:=filepath.Join(home,"workspaces")
	if !strings.EqualFold(root,want){t.Fatalf("projectless Workspaces unexpectedly moved into trusted Node filesystem: %s",root)}
	workspace,err:=os.MkdirTemp(root,"projectless-")
	if err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(workspace,"resume.txt"),[]byte("persisted"),0600);err!=nil{t.Fatal(err)}
	if b,err:=os.ReadFile(filepath.Join(workspace,"resume.txt"));err!=nil||string(b)!="persisted"{t.Fatalf("projectless resume state lost: %q %v",b,err)}
}

func TestT09WindowsNativeCmdShimWithSpacesUnderSeparateSID(t *testing.T) {
	workspace:=os.Getenv("SECRETARY_WORKER_TEST_WORKSPACE")
	protected:=os.Getenv("SECRETARY_WORKER_TEST_PROTECTED_DIR")
	if workspace==""||protected==""{t.Skip("provisioned native Worker SID required")}
	bin:=filepath.Join(workspace,"installed CLI with spaces")
	if err:=os.MkdirAll(bin,0700);err!=nil{t.Fatal(err)}
	shim:=filepath.Join(bin,"provider.cmd")
	if err:=os.WriteFile(shim,[]byte("@echo off\r\necho ARG_ONE:%~1 ARG_TWO:%~2\r\n"),0600);err!=nil{t.Fatal(err)}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:protected,Workspace:workspace})
	cmd,err:=CommandContext(ctx,shim,"first argument","second argument")
	if err!=nil{t.Fatal(err)}
	out,err:=cmd.CombinedOutput()
	if err!=nil{t.Fatalf("Windows native npm .cmd launch failed: %v %s",err,out)}
	if !strings.Contains(string(out),"ARG_ONE:first argument ARG_TWO:second argument"){
		t.Fatalf("native .cmd shim arguments were re-escaped: %q",out)
	}
}
