package webapi

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/ctl"
 "github.com/beruseruko/secretary/internal/node"
 "github.com/beruseruko/secretary/internal/telegram"
)

type t15NativeDocumentRuntime struct{
 expected []byte
 seen chan node.StartRequest
}
func(r *t15NativeDocumentRuntime)Start(_ context.Context,request node.StartRequest)(node.Session,error){
 if len(request.Images)!=0||!strings.Contains(request.Task,"Telegram document (update 800")||
 !strings.Contains(request.Task,"report.pdf")||!strings.Contains(request.Task,"Read this report")||
 !strings.Contains(request.Task,".secretary-artifacts"){
  return nil,fmt.Errorf("T15 document not staged into native Worker Task: %+v",request)
 }
 match:=" => "
 index:=strings.LastIndex(request.Task,match)
 if index<0{return nil,fmt.Errorf("native prompt omitted stable local staging path: %s",request.Task)}
 path:=strings.TrimSpace(strings.SplitN(request.Task[index+len(match):],"\n",2)[0])
 data,err:=os.ReadFile(path)
 if err!=nil||!bytes.Equal(data,r.expected){return nil,fmt.Errorf("remote Node original document lost or changed: %v",err)}
 r.seen<-request
 result:=make(chan node.Result,1)
 result<-node.Result{Status:"succeeded",Summary:"T15 native Worker read the original Telegram document"}
 return &artifactWorkerSession{result:result},nil
}

func TestT15TelegramBotDocumentServerRemoteNativeWorkerAndReplay(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
 defer cancel()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"));if err!=nil{t.Fatal(err)}
 defer store.Close()
 api,err:=New(ctx,store,"bootstrap");if err!=nil{t.Fatal(err)}
 api.AttachInternalCredential("t15-document-secret")
 capability,err:=store.RotateSecretaryCapability(ctx,api.OwnerID());if err!=nil{t.Fatal(err)}
 manager,err:=node.NewServerManager(ctx,store,"t15-pair","t15-admin");if err!=nil{t.Fatal(err)}
 manager.SetEventSink(node.NewStoreEventSink(store))
 manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
 api.AttachNodeService(manager)
 service:=ctl.WorkerService{Store:store,PersonID:api.OwnerID(),Capability:capability,
  WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
  WorkerProfileSource:func()(node.ManagedProfile,error){
   return node.ManagedProfile{Version:"t15",Name:"worker",Content:"Inspect attached documents",Hash:"t15",Runtime:"fx",AllowTools:[]string{"read"}},nil
  },Runtime:ctl.NodeRuntime{Manager:manager}}
 api.AttachSecretaryWorkerTools(service)
 api.AttachWorkerResponder(service)
 server:=httptest.NewServer(api.Handler());defer server.Close()
 identity,err:=node.EnrollNode(ctx,server.Client(),server.URL,"t15-pair","t15-remote")
 if err!=nil{t.Fatal(err)}
 inventory:=core.HarnessInventorySnapshot{Node:identity.Node,ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{{
  ID:core.HarnessInstanceID(string(identity.Node)+"/fx"),Node:identity.Node,Kind:core.HarnessFX,
  Version:"test",Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
  Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityShell}},
 }}}
 original:=[]byte("%PDF-1.7\nT15 synthetic original file bytes\n")
 runtime:=&t15NativeDocumentRuntime{expected:original,seen:make(chan node.StartRequest,2)}
 local,err:=node.OpenLocalStore(filepath.Join(t.TempDir(),"node.json"));if err!=nil{t.Fatal(err)}
 defer local.Close()
 daemon:=&node.Daemon{Identity:identity,Store:local,Runtime:runtime,Inventory:t10InventorySource{snapshot:inventory},
  Capacity:2,HeartbeatInterval:20*time.Millisecond,InventoryInterval:time.Hour,
  OutboxPollInterval:10*time.Millisecond,ReconnectMin:10*time.Millisecond,ReconnectMax:40*time.Millisecond}
 done:=make(chan error,1);go func(){done<-daemon.Run(ctx)}()
 defer func(){cancel();select{case <-done:case <-time.After(time.Second):t.Error("Node did not stop")}}()
 for {status,e:=manager.Status(ctx,identity.Node);if e==nil&&status.Online&&!status.LastHeartbeatAt.IsZero(){break}
  select{case <-ctx.Done():t.Fatal("native Node offline");case <-time.After(10*time.Millisecond):}}

 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/botTOKEN/getUpdates":
   _,_=fmt.Fprintf(w,`{"ok":true,"result":[{"update_id":800,"message":{"message_id":60,"from":{"id":100},"chat":{"id":100,"type":"private"},"caption":"Read this report","document":{"file_id":"original-document","file_unique_id":"unique-document","file_name":"report.pdf","mime_type":"application/pdf","file_size":%d}}}]}`,len(original))
  case "/botTOKEN/getFile":
   _,_=fmt.Fprintf(w,`{"ok":true,"result":{"file_path":"original.pdf","file_size":%d}}`,len(original))
  case "/file/botTOKEN/original.pdf":_,_=w.Write(original)
  case "/botTOKEN/sendMessage","/botTOKEN/sendChatAction":_,_=io.WriteString(w,`{"ok":true,"result":true}`)
  default:t.Errorf("unexpected Telegram Bot API %s",r.URL.Path);http.NotFound(w,r)
  }
 }));defer bot.Close()
 transport:=&telegram.BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 path:=filepath.Join(t.TempDir(),"telegram.json")
 adapter,err:=telegram.New(telegram.Config{StatePath:path,OwnerChatID:100},transport,
  &telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t15-document-secret"})
 if err!=nil{t.Fatal(err)}
 updates,err:=transport.GetUpdates(ctx,800,time.Second)
 if err!=nil||len(updates)!=1||updates[0].Message.Document==nil{t.Fatalf("Telegram Bot document DTO: %+v %v",updates,err)}
 if err:=adapter.HandleUpdate(ctx,updates[0]);err!=nil{t.Fatal(err)}
 if err:=adapter.PollOnce(ctx);err!=nil{t.Fatal(err)}
 conversation,err:=store.ConversationForPerson(ctx,api.OwnerID());if err!=nil{t.Fatal(err)}
 var entry core.ConversationEntry
 for entry.ID==""{
  entries,e:=store.EntriesTail(ctx,conversation.ID,0,40)
  if e!=nil{t.Fatal(e)}
  for _,v:=range entries{if strings.Contains(v.Body,"Telegram document (update 800"){entry=v;break}}
  if entry.ID!=""{break}
  select{case <-ctx.Done():t.Fatal("Telegram document not stored in Personal Conversation");case <-time.After(10*time.Millisecond):}
 }
 if !strings.Contains(entry.Body,"Read this report")||!strings.Contains(entry.Body,"unique-document"){t.Fatalf("caption/source metadata lost: %s",entry.Body)}
 ids:=node.ArtifactIDs(entry.Body)
 if len(ids)!=1{t.Fatalf("missing stable Artifact reference: %s",entry.Body)}
 originalArtifact,content,e:=store.ArtifactForPerson(ctx,api.OwnerID(),ids[0])
 if e!=nil||originalArtifact.Name!="report.pdf"||originalArtifact.MediaType!="application/pdf"||!bytes.Equal(content,original){t.Fatalf("stored bytes/metadata: %+v %v",originalArtifact,e)}
 secretaryID,e:=store.EnsureSecretaryIdentity(ctx,api.OwnerID(),conversation.ID);if e!=nil{t.Fatal(e)}
 if e=store.SetSecretaryPolicySnapshot(ctx,core.SecretaryPolicySnapshot{Version:"t15",Harness:"fx",Model:"fixture",Reasoning:"high",
  ProfileVersion:"t15",ProfileName:"secretary",ProfileHash:"t15",ProfileContent:"delegate file"});e!=nil{t.Fatal(e)}
 turn,duplicate,e:=store.EnqueueSecretaryTurnForEntry(ctx,secretaryID.ID,entry.Body,entry,nil)
 if e!=nil||duplicate{t.Fatalf("Secretary document turn: %v duplicate=%v",e,duplicate)}
 if _,e=store.StartSecretaryTurn(ctx,turn.ID);e!=nil{t.Fatal(e)}
 action,_:=json.Marshal(map[string]any{"name":"spawn_worker","arguments":map[string]any{
  "intent":entry.Body,"idempotency_key":"t15-document-spawn",
  "preferences":map[string]any{"node_id":identity.Node,"harness_kind":"fx"},
 }})
 request,e:=http.NewRequestWithContext(ctx,http.MethodPost,server.URL+"/v1/internal/secretary/tools/call",bytes.NewReader(action))
 if e!=nil{t.Fatal(e)}
 request.Header.Set("Authorization","Bearer "+capability)
 request.Header.Set("Content-Type","application/json")
 response,e:=server.Client().Do(request)
 if e!=nil{t.Fatal(e)}
 var out struct{Error string `json:"error"`}
 if e=json.NewDecoder(response.Body).Decode(&out);e!=nil{t.Fatal(e)}
 response.Body.Close()
 if response.StatusCode!=http.StatusOK||out.Error!=""{t.Fatalf("Worker dispatch HTTP %d: %s",response.StatusCode,out.Error)}
 select{case native:=<-runtime.seen:
  if !strings.Contains(native.Task,"artifact://"+ids[0]){t.Fatal("native Worker did not get original Artifact ID")}
 case <-ctx.Done():t.Fatal("remote native Worker did not receive staged document")}
 if err=adapter.HandleUpdate(ctx,updates[0]);err!=nil{t.Fatal(err)}
 restarted,e:=telegram.New(telegram.Config{StatePath:path,OwnerChatID:100},transport,
  &telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t15-document-secret"})
 if e!=nil{t.Fatal(e)}
 if err=restarted.HandleUpdate(ctx,updates[0]);err!=nil{t.Fatal(err)}
 select{case <-runtime.seen:t.Fatal("duplicate document replay launched another Worker");case <-time.After(100*time.Millisecond):}
}
