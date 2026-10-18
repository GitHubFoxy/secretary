//go:build linux

package nativeexec

import (
 "context"
 "os"
 "os/exec"
 "path/filepath"
 "testing"
)

func TestT09PinnedOfficialCodexStartResume(t *testing.T) {
 cli:=os.Getenv("T09_REAL_CODEX")
 if cli=="" {t.Skip("pinned official npm package only in CI")}
 root:=t.TempDir()
 node:=filepath.Join(root,"node")
 ws:=filepath.Join(root,"workspace")
 for _,p:=range []string{node,ws}{if err:=os.MkdirAll(p,0700);err!=nil{t.Fatal(err)}}
 // Inject executable-looking, unprovenanced Server marker files beside
 // the official entrypoint. They must not be mounted regardless of extension.
 packageRoot:=filepath.Dir(filepath.Dir(cli))
 for _,name:=range []string{"transport-secret.js","transport-secret.wasm"}{
  if err:=os.WriteFile(filepath.Join(packageRoot,name),[]byte("SERVER-TRANSPORT-SECRET"),0600);err!=nil{t.Fatal(err)}
 }
 wrapped,argv,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,Workspace:ws},cli,[]string{"--help"})
 if err!=nil{t.Fatal(err)}
 for i:=0;i+2<len(argv);i++ {
  if argv[i]=="--ro-bind" {
   for _,name:=range []string{"transport-secret.js","transport-secret.wasm"} {
    if argv[i+2]==filepath.Join(packageRoot,name){t.Fatalf("unverified secret was mounted: %s",name)}
   }
  }
 }
 // Exercise a genuine shell inside the SAME official-package namespace:
 // the marker files must be inaccessible, not merely absent from arg text.
 split:=-1
 for i,x:=range argv {if x=="--"{split=i;break}}
 if split<0{t.Fatal("sandbox command separator absent")}
 shellArgs:=append(append([]string{},argv[:split+1]...),"/bin/sh","-c",
   "test ! -r \"$1\" && test ! -r \"$2\"","t09",
   filepath.Join(packageRoot,"transport-secret.js"),filepath.Join(packageRoot,"transport-secret.wasm"))
 proof:=exec.Command(wrapped,shellArgs...)
 if output,err:=proof.CombinedOutput();err!=nil{t.Fatalf("Worker could read unverified package Server secret: %v %s",err,output)}
 // Real distribution/entrypoint, never a shell fixture. Exercise startup
 // and Resume subcommand parsing without credentials/network calls.
 ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:node,Workspace:ws})
 for _,args:=range [][]string{{"--help"},{"exec","resume","--help"}} {
  cmd,err:=CommandContext(ctx,cli,args...)
  if err!=nil{t.Fatal(err)}
  output,err:=cmd.CombinedOutput()
  if err!=nil{t.Fatalf("official Codex launch %v failed in Worker sandbox: %v: %s",args,err,output)}
  if len(output)==0{t.Fatalf("official Codex launch %v produced no output",args)}
 }
}
