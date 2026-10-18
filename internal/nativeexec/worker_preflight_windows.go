//go:build windows

package nativeexec

import (
 "context"
 "encoding/json"
 "encoding/base64"
 "unicode/utf16"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "time"
)

func PreflightWorkerSandbox(protectedDir string) error {
 home:=strings.TrimSpace(os.Getenv("SECRETARY_WORKER_HOME"))
 if home=="" {
  raw,err:=os.ReadFile(filepath.Join(protectedDir,"worker-account.json"))
  if err!=nil{return errors.New("Windows Worker account not set up: run scripts/setup-windows-worker.ps1 elevated once")}
  var c struct {Home string}
  if err:=json.Unmarshal(raw,&c);err!=nil{return err}
  home=c.Home
 }
 if home==""{return errors.New("Windows Worker account home missing")}
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 ctx=WithWorkerSandbox(ctx,WorkerSandbox{ProtectedDir:protectedDir,Workspace:home})
 encoded:=utf16.Encode([]rune("exit 0"))
	command:=make([]byte,0,len(encoded)*2)
	for _,unit:=range encoded{command=append(command,byte(unit),byte(unit>>8))}
	cmd,err:=CommandContext(ctx,"powershell.exe","-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(command))
 if err!=nil{return fmt.Errorf("Windows Worker security preflight: %w",err)}
 output,err:=cmd.CombinedOutput()
 if err!=nil{return fmt.Errorf("Windows Worker cross-SID process preflight failed: %w: %s",err,output)}
 return nil
}

func ManagedWorkerWorkspaceRoot(protectedDir string)(string,error){
	// Test/in-process harness Daemons (and already queued text-only paths)
	// may have a broker but never start a native subprocess. Preserve their
	// original workspace behavior before Windows Worker provisioning.
	// The native process launcher independently fails closed without a SID.
	if strings.TrimSpace(os.Getenv("SECRETARY_WORKER_HOME"))=="" {
		if _,statErr:=os.Stat(filepath.Join(protectedDir,"worker-account.json"));os.IsNotExist(statErr) {
			return "",nil
		}else if statErr!=nil{return "",statErr}
	}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:protectedDir})
	home,err:=WindowsWorkerHome(ctx)
	if err!=nil{return "",err}
	if home==""{return "",errors.New("Windows Worker is not provisioned")}
	dir:=filepath.Join(home,"workspaces")
	if err:=os.MkdirAll(dir,0700);err!=nil{return "",err}
	return dir,nil
}
