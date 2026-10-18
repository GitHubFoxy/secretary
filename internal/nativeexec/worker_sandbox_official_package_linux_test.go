//go:build linux

package nativeexec

import (
 "context"
 "bufio"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "strings"
 "time"
 "os"
 "os/exec"
 "path/filepath"
 "testing"
 "time"
 "net/http"
 "net/http/httptest"
 "encoding/json"
 "fmt"
 "strings"
 "bufio"
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

func TestT09RealCodexPersistedSessionResume(t *testing.T) {
 cli:=os.Getenv("T09_REAL_CODEX")
 if cli=="" {t.Skip("pinned official package is required in CI")}
 var inputs []string
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if r.URL.Path!="/v1/responses" {http.Error(w,"unexpected endpoint "+r.URL.Path,http.StatusNotFound);return}
   var request map[string]any
   if err:=json.NewDecoder(r.Body).Decode(&request);err!=nil{http.Error(w,err.Error(),400);return}
   input,_:=json.Marshal(request["input"])
   inputs=append(inputs,string(input))
   turn:=len(inputs)
   responseID:=fmt.Sprintf("resp_t09_%d",turn)
   text:=fmt.Sprintf("T09-CONTINUED-%d",turn)
   w.Header().Set("Content-Type","text/event-stream")
   w.Header().Set("Cache-Control","no-cache")
   f,_:=w.(http.Flusher)
   send:=func(kind string,payload any) {
     b,_:=json.Marshal(payload)
     fmt.Fprintf(w,"event: %s\ndata: %s\n\n",kind,b)
     if f!=nil{f.Flush()}
   }
   base:=map[string]any{"id":responseID,"object":"response","created_at":1730000000,"model":"t09-local","status":"completed","output":[]any{map[string]any{"id":"msg_t09","type":"message","role":"assistant","status":"completed","content":[]any{map[string]any{"type":"output_text","text":text,"annotations":[]any{}}}}},"usage":map[string]any{"input_tokens":10,"output_tokens":5,"total_tokens":15}}
   send("response.created",map[string]any{"type":"response.created","response":base})
   send("response.output_item.added",map[string]any{"type":"response.output_item.added","output_index":0,"item":map[string]any{"id":"msg_t09","type":"message","role":"assistant","content":[]any{}}})
   send("response.output_text.delta",map[string]any{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":text})
   send("response.completed",map[string]any{"type":"response.completed","response":base})
 }))
 defer server.Close()
 root:=t.TempDir()
 home:=filepath.Join(root,"home")
 ws:=filepath.Join(root,"workspace")
 node:=filepath.Join(root,"node")
 for _,dir:=range []string{home,ws,node,filepath.Join(home,".codex")} {
   if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 }
 config:=fmt.Sprintf("model = \"t09-local\"\nmodel_provider = \"t09mock\"\napproval_policy = \"never\"\nmodel_reasoning_effort = \"low\"\n[model_providers.t09mock]\nname = \"T09 CI Fake Responses Backend\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\n",server.URL+"/v1")
 if err:=os.WriteFile(filepath.Join(home,".codex","config.toml"),[]byte(config),0600);err!=nil{t.Fatal(err)}
 t.Setenv("HOME",home)
 runCtx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 ctx:=WithWorkerSandbox(runCtx,WorkerSandbox{ProtectedDir:node,Workspace:ws})
 run:=func(args ...string) string{
  t.Helper()
  cmd,err:=CommandContext(ctx,cli,args...)
  if err!=nil{t.Fatal(err)}
  output,err:=cmd.CombinedOutput()
  if err!=nil{t.Fatalf("real Codex command %v failed: %v\n%s",args,err,output)}
  return string(output)
 }
 first:=run("exec","--json","--skip-git-repo-check","First turn: remember ALPHA")
 session:=""
 for scan:=bufio.NewScanner(strings.NewReader(first));scan.Scan();{
  var event struct{Type string `json:"type"`;ThreadID string `json:"thread_id"`}
  if json.Unmarshal(scan.Bytes(),&event)==nil && event.Type=="thread.started"{session=event.ThreadID}
 }
 if session==""{t.Fatalf("real Codex did not create a persisted thread ID: %s",first)}
 if !strings.Contains(first,"T09-CONTINUED-1"){t.Fatalf("first real session did not complete: %s",first)}
 entries,err:=os.ReadDir(filepath.Join(home,".codex","sessions"))
 if err!=nil || len(entries)==0{t.Fatalf("no persisted Codex sessions after Start: %v",err)}
 second:=run("exec","resume","--json","--skip-git-repo-check",session,"Second turn: confirm ALPHA was in our earlier exchange")
 if !strings.Contains(second,"T09-CONTINUED-2"){t.Fatalf("same-session native Resume did not continue: %s",second)}
 if len(inputs)!=2{t.Fatalf("expected exactly two real provider calls, got %d",len(inputs))}
 if !strings.Contains(inputs[1],"ALPHA"){t.Fatalf("resumed model context omitted the prior turn: %s",inputs[1])}
}
