package webapi

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/ctl"
 "github.com/beruseruko/secretary/internal/mcp"
 "github.com/beruseruko/secretary/internal/node"
 secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
 "github.com/beruseruko/secretary/internal/telegram"
)

func TestT13DMTelegramVideoUnsupportedAutoWorkerWarnsBothPersonalProjections(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
 defer cancel()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"));if err!=nil{t.Fatal(err)}
 defer store.Close()
 api,err:=New(ctx,store,"bootstrap");if err!=nil{t.Fatal(err)}
 api.AttachInternalCredential("t13-no-image-secret")
 if err:=api.AttachVideoOptions(VideoOptions{FPS:2,MaxFrames:12,MaxDurationSeconds:12,TimeoutSeconds:10});err!=nil{t.Fatal(err)}
 release:=make(chan struct{});close(release)
 api.AttachVideoProcessor(&t12Processor{started:make(chan struct{}),release:release})
 capability,err:=store.RotateSecretaryCapability(ctx,api.OwnerID());if err!=nil{t.Fatal(err)}
 if _,err=store.EnrollNode(ctx,"no-image-node");err!=nil{t.Fatal(err)}
 instance:=core.HarnessInstance{ID:"no-image-node/fx",Node:"no-image-node",Kind:core.HarnessFX,
  Version:"test",Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
  Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityShell}}}
 if err=store.UpdateNodeHeartbeat(ctx,"no-image-node",core.HarnessInventorySnapshot{
  Node:"no-image-node",ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{instance},
 },core.NodeHeartbeat{Capacity:2});err!=nil{t.Fatal(err)}
 svc:=ctl.WorkerService{Store:store,PersonID:api.OwnerID(),Capability:capability,
  WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
  WorkerProfileSource:func()(node.ManagedProfile,error){
   return node.ManagedProfile{Version:"t13",Name:"worker",Content:"Video frame analysis",Hash:"t13",Runtime:"fx",AllowTools:[]string{"read"}},nil
  }}
 conversation,err:=store.ConversationForPerson(ctx,api.OwnerID());if err!=nil{t.Fatal(err)}
 secretaryID,err:=store.EnsureSecretaryIdentity(ctx,api.OwnerID(),conversation.ID);if err!=nil{t.Fatal(err)}
 if err=store.SetSecretaryPolicySnapshot(ctx,core.SecretaryPolicySnapshot{
  Version:"t13",Harness:"opencode",Model:"fixture",Reasoning:"high",ProfileVersion:"t13",
  ProfileName:"secretary",ProfileHash:"t13",ProfileContent:"Delegate a video as a Worker task.",
  ReplyContractVersion:core.SecretaryReplyContractAddressedV1,
 });err!=nil{t.Fatal(err)}
 harness:=&t13SecretaryHarness{}
 tool:=mcp.Secretary{Workers:svc,ReplyContractVersion:core.SecretaryReplyContractAddressedV1}
 failed:=make(chan error,1)
 harness.onPrompt=func(prompt string)error{
  body,turnID,inputID,e:=t13SecretaryPromptIdentity(prompt);if e!=nil{return e}
  userHead:="\n\nUser turn:\n"
  pos:=strings.LastIndex(body,userHead);if pos<0{return fmt.Errorf("missing Telegram user turn")}
  userInput:=strings.TrimSpace(body[pos+len(userHead):])
  args,e:=json.Marshal(map[string]any{"intent":userInput,"idempotency_key":inputID,
   "secretary_turn_id":turnID,"input_id":inputID,
   "preferences":map[string]any{"node_id":"no-image-node","harness_kind":"fx"}})
  if e!=nil{return e}
  _,e=tool.Call(ctx,"spawn_worker",args)
  select{case failed<-e:default:}
  // The failed tool must not take down the scripted Secretary session.
  return nil
 }
 runtime:=secretaryruntime.NewRuntime(node.NewLocal(harness),capability)
 runtime.AttachConversation(store,conversation.ID)
 runtime.AttachIdentity(secretaryID)
 profile:=node.ManagedProfile{Version:"t13",Name:"secretary",Content:"Delegate a video as a Worker task.",
  Runtime:"opencode",Model:"fixture",Reasoning:"high",ReplyContractVersion:core.SecretaryReplyContractAddressedV1}
 profile.Hash=node.HashProfile(profile.Content,nil,profile.Model,profile.Reasoning)
 runtime.AttachProfile(func()node.ManagedProfile{return profile})
 dir:=t.TempDir()
 if _,err=store.SaveUserDocument(ctx,filepath.Join(dir,"user.md"),"one owner");err!=nil{t.Fatal(err)}
 runtime.AttachMCP("fixture",dir)
 if err=runtime.Start(ctx);err!=nil{t.Fatal(err)}
 defer runtime.Stop(context.Background())
 api.AttachSecretary(runtime)
 server:=httptest.NewServer(api.Handler());defer server.Close()

 sent:=make(chan struct{chat,text string},10)
 original:=[]byte("good video")
 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/botTOKEN/getUpdates":
   _,_=fmt.Fprintf(w,`{"ok":true,"result":[{"update_id":700,"message":{"message_id":70,"from":{"id":100},"chat":{"id":100,"type":"private"},"caption":"Analyze unsupported video","video":{"file_id":"no-cap","file_unique_id":"original","file_name":"clip.mp4","mime_type":"video/mp4","file_size":%d,"duration":2}}}]}`,len(original))
  case "/botTOKEN/getFile":
   _,_=fmt.Fprintf(w,`{"ok":true,"result":{"file_path":"clip.mp4","file_size":%d}}`,len(original))
  case "/file/botTOKEN/clip.mp4":_,_=w.Write(original)
  case "/botTOKEN/sendMessage":
   _=r.ParseForm()
   sent<-struct{chat,text string}{chat:r.Form.Get("chat_id"),text:r.Form.Get("text")}
   _,_=io.WriteString(w,`{"ok":true,"result":true}`)
  case "/botTOKEN/sendChatAction":_,_=io.WriteString(w,`{"ok":true,"result":true}`)
  default:http.NotFound(w,r)
  }
 }));defer bot.Close()
 group:=int64(-200)
 tr:=&telegram.BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 adapter,err:=telegram.New(telegram.Config{StatePath:filepath.Join(t.TempDir(),"telegram.json"),
  OwnerChatID:100,GroupChatID:&group},tr,&telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t13-no-image-secret"})
 if err!=nil{t.Fatal(err)}
 updates,err:=tr.GetUpdates(ctx,700,time.Second)
 if err!=nil||len(updates)!=1{t.Fatalf("Bot DM video: %+v %v",updates,err)}
 if err:=adapter.HandleUpdate(ctx,updates[0]);err!=nil{t.Fatal(err)}
 select{case e:=<-failed:
  if e==nil||!strings.Contains(e.Error(),"capabilit"){t.Fatalf("automatic unsupported Worker was not rejected: %v",e)}
 case <-ctx.Done():t.Fatal("Secretary did not select unsupported Worker")}
 workers,err:=store.WorkersForConversation(ctx,conversation.ID)
 if err!=nil||len(workers)!=0{t.Fatalf("unsupported Worker was created: %+v %v",workers,err)}
 projection:=func(source core.Event)telegram.Event{
  if mapped,ok:=telegram.ProjectCoreWorkerEvent(source);ok{return mapped}
  out:=telegram.Event{EventID:source.ID,Sequence:source.Seq}
  var payload map[string]any
  _=json.Unmarshal(source.Payload,&payload)
  if source.Kind=="conversation.entry"&&payload["kind"]==string(core.EntrySecretary){
   out.Kind=core.SecretaryTextDeltaEvent
   out.Text,_=payload["body"].(string)
  }
  if source.Kind==core.SecretaryTurnFinishedEvent{out.Kind=source.Kind}
  return out
 }
 for {
  if _,err=telegram.RelayCoreEventsOnce(ctx,store,adapter,projection);err!=nil{t.Fatal(err)}
  if err=adapter.Flush(ctx);err!=nil{t.Fatal(err)}
  counts:=map[string]int{}
  drain:=true
  for drain {select{case m:=<-sent:
   if strings.Contains(m.text,"CapabilityImageInput"){counts[m.chat]++}
  default:drain=false}}
  if counts["100"]==1&&counts["-200"]==1{
   if _,err=telegram.RelayCoreEventsOnce(ctx,store,adapter,projection);err!=nil{t.Fatal(err)}
   return
  }
  select{case <-ctx.Done():t.Fatalf("missing Telegram rejection in DM/General: %+v",counts)
  case <-time.After(20*time.Millisecond):}
 }
}
