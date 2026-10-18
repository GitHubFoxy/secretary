//go:build linux || darwin

package node

import (
 "context"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "testing"

 "github.com/beruseruko/secretary/internal/nativeexec"
)

type t09NativeSandboxRuntime struct {
 command string
 secret string
}
func (r *t09NativeSandboxRuntime) execute(ctx context.Context, request StartRequest) (Session,error) {
 if !nativeexec.IsWorkerSandboxed(ctx) { return nil,fmt.Errorf("local native runtime received unsandboxed context") }
 script := fmt.Sprintf("if cat '%s' >/dev/null 2>&1; then exit 66; fi; echo SANDBOX_OK",r.secret)
 cmd,err:=nativeexec.CommandContext(ctx,"/bin/sh","-c",script)
 if err!=nil{return nil,err}
 out,err:=cmd.CombinedOutput()
 if err!=nil{return nil,fmt.Errorf("Worker can read Server secret or host sandbox is unavailable: %w: %s",err,out)}
 if !strings.Contains(string(out),"SANDBOX_OK"){return nil,fmt.Errorf("sandbox did not execute native subprocess: %s",out)}
 return &t09SandboxSession{},nil
}
func (r *t09NativeSandboxRuntime) Start(ctx context.Context,request StartRequest)(Session,error){return r.execute(ctx,request)}
func (r *t09NativeSandboxRuntime) Resume(ctx context.Context,request StartRequest,_ string)(Session,error){return r.execute(ctx,request)}
type t09SandboxSession struct{}
func (*t09SandboxSession) ID()string{return "t09-native-sandbox-session"}
func (*t09SandboxSession) Prompt(context.Context,string)error{return nil}
func (*t09SandboxSession) Steer(context.Context,string)(bool,error){return true,nil}
func (*t09SandboxSession) Cancel(context.Context)error{return nil}
func (*t09SandboxSession) Activity()<-chan Activity{c:=make(chan Activity);close(c);return c}
func (*t09SandboxSession) Result()<-chan Result{c:=make(chan Result);close(c);return c}
func (*t09SandboxSession) Close()error{return nil}

func TestT09ProductionLocalNodeNativeStartAndResumeDenySameUIDServerSecrets(t *testing.T) {
 if runtime.GOOS=="linux" {
  if _,err:=exec.LookPath("bwrap");err!=nil{t.Skip("bubblewrap not available outside CI")}
 }
 root:=t.TempDir()
 server:=filepath.Join(root,"server")
 protected:=filepath.Join(server,"node","data")
 workspace:=filepath.Join(root,"workspace")
 for _,path:=range []string{server,protected,workspace}{if err:=os.MkdirAll(path,0700);err!=nil{t.Fatal(err)}}
 secret:=filepath.Join(server,"secretary.db")
 if err:=os.WriteFile(secret,[]byte("SQLITE-CONTAINS-NODE-HMAC"),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(server,"transport.secret"),[]byte("HMAC-SIGNING-SECRET"),0600);err!=nil{t.Fatal(err)}
 harness:=&t09NativeSandboxRuntime{secret:secret}
 local:=NewLocal(harness)
 defer local.Close()
 if err:=local.SetNativeWorkerSandbox(nativeexec.WorkerSandbox{ProtectedDir:protected,ServerDataDir:server});err!=nil{t.Fatal(err)}
 start:=StartRequest{WorkerRef:"t09-native-local",AttemptID:"t09-start",Workspace:workspace}
 if _,err:=local.Dispatch(context.Background(),start);err!=nil{
  if runtime.GOOS=="linux" && (strings.Contains(err.Error(),"Operation not permitted")||strings.Contains(err.Error(),"Creating new namespace failed")){t.Skipf("host kernel prohibits bubblewrap: %v",err)}
  t.Fatal(err)
 }
 if err:=local.Remove("t09-native-local");err!=nil{t.Fatal(err)}
 resumed:=StartRequest{WorkerRef:"t09-native-local",AttemptID:"t09-resume",Workspace:workspace}
 if _,err:=local.Resume(context.Background(),resumed,"t09-native-sandbox-session");err!=nil{t.Fatal(err)}
}
