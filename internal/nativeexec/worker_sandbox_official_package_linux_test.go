//go:build linux

package nativeexec

import (
 "context"
 "os"
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
