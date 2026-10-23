//go:build linux

package nativeexec

import (
 "bufio"
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// Validate the actual, pinned CLI without host-global npm or isolation dependencies.
func TestT09PinnedOfficialCodexStartResume(t *testing.T) {
 cli:=os.Getenv("T09_REAL_CODEX")
 if cli=="" {t.Skip("pinned official Codex CLI only in CI")}
 t.Setenv("CODEX_PATH",cli)
 t.Setenv("SECRETARY_ACP_COMMAND","codex-acp")
 for _,args:=range [][]string{{"--help"},{"exec","resume","--help"}} {
  cmd,err:=CommandContext(context.Background(),cli,args...)
  if err!=nil {t.Fatal(err)}
  cmd.Dir=t.TempDir()
  output,err:=cmd.CombinedOutput()
  if err!=nil {t.Fatalf("direct Codex launch %v: %v: %s",args,err,output)}
  if len(output)==0 {t.Fatalf("Codex %v produced no output",args)}
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
   message:=map[string]any{"id":"msg_t09","type":"message","role":"assistant","status":"completed","content":[]any{map[string]any{"type":"output_text","text":text,"annotations":[]any{}}}}
   send("response.created",map[string]any{"type":"response.created","response":base})
   send("response.in_progress",map[string]any{"type":"response.in_progress","response":base})
   send("response.output_item.added",map[string]any{"type":"response.output_item.added","output_index":0,"item":map[string]any{"id":"msg_t09","type":"message","role":"assistant","status":"in_progress","content":[]any{}}})
   send("response.content_part.added",map[string]any{"type":"response.content_part.added","output_index":0,"content_index":0,"part":map[string]any{"type":"output_text","text":"","annotations":[]any{}}})
   send("response.output_text.delta",map[string]any{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":text})
   send("response.output_text.done",map[string]any{"type":"response.output_text.done","output_index":0,"content_index":0,"text":text})
   send("response.content_part.done",map[string]any{"type":"response.content_part.done","output_index":0,"content_index":0,"part":map[string]any{"type":"output_text","text":text,"annotations":[]any{}}})
   send("response.output_item.done",map[string]any{"type":"response.output_item.done","output_index":0,"item":message})
   send("response.completed",map[string]any{"type":"response.completed","response":base})
 }))
 defer server.Close()
 root:=t.TempDir()
 home:=filepath.Join(root,"home")
 ws:=filepath.Join(root,"workspace")
 for _,dir:=range []string{home,ws,filepath.Join(home,".codex")} {
   if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
 }
 config:=fmt.Sprintf("model = \"t09-local\"\nmodel_provider = \"t09mock\"\napproval_policy = \"never\"\nmodel_reasoning_effort = \"low\"\n[features]\nplugins = false\nremote_plugin = false\n[model_providers.t09mock]\nname = \"T09 CI Fake Responses Backend\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\n",server.URL+"/v1")
 if err:=os.WriteFile(filepath.Join(home,".codex","config.toml"),[]byte(config),0600);err!=nil{t.Fatal(err)}
 t.Setenv("HOME",home)
 runCtx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
 defer cancel()
 ctx:=runCtx
 run:=func(args ...string) string{
  t.Helper()
  cmd,err:=CommandContext(ctx,cli,args...)
  if err!=nil{t.Fatal(err)}
  cmd.Dir=ws
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
 second:=run("exec","resume","--json","--skip-git-repo-check",session,"Second turn: continue the previous conversation without repeating its marker")
 if !strings.Contains(second,"T09-CONTINUED-2"){t.Fatalf("same-session native Resume did not continue: %s",second)}
 resumedID:=""
 for scan:=bufio.NewScanner(strings.NewReader(second));scan.Scan();{
  var event struct{Type string `json:"type"`;ThreadID string `json:"thread_id"`}
  if json.Unmarshal(scan.Bytes(),&event)==nil && event.Type=="thread.started"{resumedID=event.ThreadID}
 }
 if resumedID!=session{t.Fatalf("resumed a different thread: start=%q resume=%q output=%s",session,resumedID,second)}
 if len(inputs)!=2{t.Fatalf("expected exactly two real provider calls, got %d",len(inputs))}
 if strings.Contains(inputs[1],"Second turn: confirm ALPHA"){t.Fatal("resume prompt accidentally leaked marker")}
 if !strings.Contains(inputs[0],"ALPHA"){t.Fatalf("first request did not contain marker: %s",inputs[0])}
 if !strings.Contains(inputs[1],"ALPHA"){t.Fatalf("restored backend input omitted marker found only in first prompt: %s",inputs[1])}
  pluginTmp:=filepath.Join(home,".codex",".tmp")
  if entries,err:=os.ReadDir(pluginTmp);err==nil{
    for _,entry:=range entries{if strings.HasPrefix(entry.Name(),"plugins-clone"){t.Fatalf("plugin marketplace clone was started in isolated HOME: %s",entry.Name())}}
  } else if !os.IsNotExist(err){t.Fatal(err)}

}
