//go:build windows

package nativeexec

import (
 "context"
 "encoding/json"
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
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
 defer cancel()
 ctx=WithWorkerSandbox(ctx,WorkerSandbox{ProtectedDir:protectedDir,Workspace:home})
 cmd,err:=CommandContext(ctx,"cmd.exe","/C","exit","0")
 if err!=nil{return fmt.Errorf("Windows Worker security preflight: %w",err)}
 output,err:=cmd.CombinedOutput()
 if err!=nil{return fmt.Errorf("Windows Worker cross-SID process preflight failed: %w: %s",err,output)}
 return nil
}
