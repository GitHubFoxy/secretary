//go:build linux || darwin

package nativeexec

import (
 "context"
 "fmt"
 "os"
 "path/filepath"
 "testing"
)

func TestT09NativeProviderHistorySurvivesDirectProcessRestart(t *testing.T) {
 root:=t.TempDir()
 workspace:=filepath.Join(root,"workspace")
 if err:=os.MkdirAll(workspace,0700);err!=nil{t.Fatal(err)}
 for _,provider:=range []string{"codex","claude","opencode"} {
  t.Run(provider,func(t *testing.T){
   dir:=filepath.Join(root,provider)
   if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
   checkpoint:=filepath.Join(dir,"native-session.checkpoint")
   for _,script:=range []string{fmt.Sprintf("printf '%s' > '%s'",provider,checkpoint),fmt.Sprintf("test \"$(cat '%s')\" = '%s'",checkpoint,provider)} {
    cmd,err:=CommandContext(context.Background(),"/bin/sh","-c",script)
    if err!=nil{t.Fatal(err)}
    cmd.Dir=workspace
    if output,err:=cmd.CombinedOutput();err!=nil{t.Fatalf("direct native checkpoint: %v: %s",err,output)}
   }
  })
 }
}
