package nativeexec

import (
 "context"
 "encoding/json"
 "os"
 "os/user"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// The subprocess runs the current test binary. This tests real OS identity,
// inherited environment, workspace, and access outside that workspace.
func TestDirectWorkerHelperProcess(t *testing.T) {
 if os.Getenv("SECRETARY_DIRECT_HELPER")!="1" {return}
 marker:=-1
 for i,arg:=range os.Args {if arg=="--" {marker=i;break}}
 if marker<0||marker+2>=len(os.Args){os.Exit(2)}
 if os.Args[marker+1]=="sleep" {time.Sleep(30*time.Second);return}
 current,err:=user.Current();if err!=nil{os.Exit(3)}
 target:=os.Args[marker+2]
 data,err:=os.ReadFile(target);if err!=nil{os.Exit(4)}
 if err:=os.WriteFile(target,append(data,[]byte("-worker")...),0600);err!=nil{os.Exit(5)}
 cwd,err:=os.Getwd();if err!=nil{os.Exit(6)}
 _=json.NewEncoder(os.Stdout).Encode(map[string]string{
  "uid":current.Uid,"cwd":cwd,"code":os.Getenv("CODEX_PATH"),
  "acp":os.Getenv("SECRETARY_ACP_COMMAND"),"custom":os.Getenv("SECRETARY_DIRECT_TEST_SECRET"),
 })
}

func TestDirectWorkerInheritsIdentityEnvironmentAndHostFileAccess(t *testing.T) {
 current,err:=user.Current();if err!=nil{t.Fatal(err)}
 root:=t.TempDir();workspace:=filepath.Join(root,"workspace");host:=filepath.Join(root,"host")
 if err:=os.MkdirAll(workspace,0700);err!=nil{t.Fatal(err)}
 if err:=os.MkdirAll(host,0700);err!=nil{t.Fatal(err)}
 target:=filepath.Join(host,"outside-workspace.txt")
 if err:=os.WriteFile(target,[]byte("node"),0600);err!=nil{t.Fatal(err)}
 t.Setenv("SECRETARY_DIRECT_HELPER","1")
 t.Setenv("CODEX_PATH",filepath.Join(root,"managed-codex"))
 t.Setenv("SECRETARY_ACP_COMMAND",filepath.Join(root,"managed-acp"))
 t.Setenv("SECRETARY_DIRECT_TEST_SECRET","inherited")
 cmd,err:=CommandContext(context.Background(),os.Args[0],"-test.run=^TestDirectWorkerHelperProcess$","--","access",target)
 if err!=nil{t.Fatal(err)}
 cmd.Dir=workspace
 output,err:=cmd.CombinedOutput();if err!=nil{t.Fatalf("worker failed: %v (%s)",err,output)}
 var got map[string]string;if err:=json.Unmarshal(output,&got);err!=nil{t.Fatalf("invalid helper response %s: %v",output,err)}
 if got["uid"]!=current.Uid || filepath.Clean(got["cwd"])!=filepath.Clean(workspace) {t.Fatalf("different principal or cwd: %+v",got)}
 if got["code"]!=os.Getenv("CODEX_PATH") || got["acp"]!=os.Getenv("SECRETARY_ACP_COMMAND") || got["custom"]!="inherited" {t.Fatalf("environment filtered: %+v",got)}
 b,err:=os.ReadFile(target);if err!=nil{t.Fatal(err)}
 if string(b)!="node-worker" {t.Fatalf("worker cannot modify user-accessible host file: %q",b)}
}

func TestDirectWorkerContextCancellation(t *testing.T) {
 t.Setenv("SECRETARY_DIRECT_HELPER","1")
 ctx,cancel:=context.WithTimeout(context.Background(),250*time.Millisecond)
 defer cancel()
 cmd,err:=CommandContext(ctx,os.Args[0],"-test.run=^TestDirectWorkerHelperProcess$","--","sleep","unused")
 if err!=nil{t.Fatal(err)}
 started:=time.Now()
 err=cmd.Run()
 if err==nil||(!strings.Contains(err.Error(),"killed") && !strings.Contains(err.Error(),"exit status") && ctx.Err()==nil){t.Fatalf("cancel not enforced: %v",err)}
 if time.Since(started)>10*time.Second {t.Fatal("cancel did not stop native process promptly")}
}
