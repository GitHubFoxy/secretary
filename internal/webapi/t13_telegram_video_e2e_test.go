package webapi

import (
 "bytes"
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
 secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
 "github.com/beruseruko/secretary/internal/node"
 "github.com/beruseruko/secretary/internal/telegram"
)

func TestT13TelegramVideoToT12FramesNativeWorkerResultAndReplay(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
 defer cancel()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"))
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 api,err:=New(ctx,store,"bootstrap")
 if err!=nil{t.Fatal(err)}
 api.AttachInternalCredential("t13-secret")
 if err=api.AttachVideoOptions(VideoOptions{FPS:2,MaxFrames:12,MaxDurationSeconds:12,TimeoutSeconds:10});err!=nil{t.Fatal(err)}
 release:=make(chan struct{});close(release)
 api.AttachVideoProcessor(&t12Processor{started:make(chan struct{}),release:release})
 capability,err:=store.RotateSecretaryCapability(ctx,api.OwnerID())
 if err!=nil{t.Fatal(err)}
 manager,err:=node.NewServerManager(ctx,store,"t13-pair","t13-admin")
 if err!=nil{t.Fatal(err)}
 manager.SetEventSink(node.NewStoreEventSink(store))
 manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
 api.AttachNodeService(manager)
 svc:=ctl.WorkerService{Store:store,PersonID:api.OwnerID(),Capability:capability,
  WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
  WorkerProfileSource:func()(node.ManagedProfile,error){
   return node.ManagedProfile{Version:"t13",Name:"worker",Content:"Describe video frames",Hash:"t13",Runtime:"fx",AllowTools:[]string{"read"}},nil
  },Runtime:ctl.NodeRuntime{Manager:manager}}
 api.AttachSecretaryWorkerTools(svc)
 api.AttachWorkerResponder(svc)
 conversation,err:=store.ConversationForPerson(ctx,api.OwnerID())
 if err!=nil{t.Fatal(err)}
 secretaryID,err:=store.EnsureSecretaryIdentity(ctx,api.OwnerID(),conversation.ID)
 if err!=nil{t.Fatal(err)}
 if err:=store.SetSecretaryPolicySnapshot(ctx,core.SecretaryPolicySnapshot{
  Version:"t13",Harness:"opencode",Model:"fixture",Reasoning:"high",
  ProfileVersion:"t13",ProfileName:"secretary",ProfileHash:"t13",
  ProfileContent:"Delegate Telegram video to a Worker via MCP.",
  ReplyContractVersion:core.SecretaryReplyContractAddressedV1,
 });err!=nil{t.Fatal(err)}
 harness:=&t13SecretaryHarness{}
 secretaryErrors:=make(chan error,8)
 tool:=mcp.Secretary{Workers:svc,ReplyContractVersion:core.SecretaryReplyContractAddressedV1}
 harness.onPrompt=func(prompt string)(err error){
  defer func(){if err!=nil{select{case secretaryErrors<-err:default:}}}()
  body,turnID,inputID,e:=t13SecretaryPromptIdentity(prompt)
  if e!=nil{return e}
  const userHead="\n\nUser turn:\n"
  inputStart:=strings.LastIndex(body,userHead)
  if inputStart<0{return fmt.Errorf("automatic Secretary prompt has no canonical user turn")}
  input:=strings.TrimSpace(body[inputStart+len(userHead):])
  if !strings.Contains(input,"Telegram video (update 500")||!strings.Contains(input,"Describe the sampled video")||!strings.Contains(input,"artifact://"){
   return fmt.Errorf("automatic Secretary routing lost Telegram video artifact or caption")
  }
  reply,e:=json.Marshal(map[string]any{
   "text":"Video request received.",
   "secretary_turn_id":turnID,"input_id":inputID,
  })
  if e!=nil{return e}
  if _,e=tool.Call(ctx,"reply_to_user",reply);e!=nil{return e}
  args,e:=json.Marshal(map[string]any{
   "intent":input,
   "idempotency_key":inputID,
   "secretary_turn_id":turnID,"input_id":inputID,
   "preferences":map[string]any{"node_id":"t13-remote","harness_kind":"fx"},
  })
  if e!=nil{return e}
  _,e=tool.Call(ctx,"spawn_worker",args)
  return e
 }
 secRuntime:=secretaryruntime.NewRuntime(node.NewLocal(harness),capability)
 secRuntime.AttachConversation(store,conversation.ID)
 secRuntime.AttachIdentity(secretaryID)
 profile:=node.ManagedProfile{Version:"t13",Name:"secretary",Content:"Delegate Telegram video to a Worker via MCP.",Runtime:"opencode",Model:"fixture",Reasoning:"high",
  ReplyContractVersion:core.SecretaryReplyContractAddressedV1}
 profile.Hash=node.HashProfile(profile.Content,nil,profile.Model,profile.Reasoning)
 secRuntime.AttachProfile(func()node.ManagedProfile{return profile})
 dataDir:=t.TempDir()
 if _,e:=store.SaveUserDocument(ctx,filepath.Join(dataDir,"user.md"),"one owner");e!=nil{t.Fatal(e)}
 secRuntime.AttachMCP("fixture",dataDir)
 if err:=secRuntime.Start(ctx);err!=nil{t.Fatal(err)}
 defer secRuntime.Stop(context.Background())
 api.AttachSecretary(secRuntime)
 server:=httptest.NewServer(api.Handler());defer server.Close()
 identity,err:=node.EnrollNode(ctx,server.Client(),server.URL,"t13-pair","t13-remote")
 if err!=nil{t.Fatal(err)}
 runtime:=&t13VideoNativeRuntime{t12Runtime:t12Runtime{seen:make(chan node.StartRequest,3)}}
 inventory:=core.HarnessInventorySnapshot{Node:identity.Node,ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{{
  ID:core.HarnessInstanceID(string(identity.Node)+"/fx"),Node:identity.Node,Kind:core.HarnessFX,
  Version:"test",Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
  Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityImageInput}},
 }}}
 local,err:=node.OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
 if err!=nil{t.Fatal(err)}
 defer local.Close()
 daemon:=&node.Daemon{Identity:identity,Store:local,Runtime:runtime,Inventory:t10InventorySource{snapshot:inventory},
  Capacity:2,HeartbeatInterval:20*time.Millisecond,InventoryInterval:time.Hour,
  OutboxPollInterval:10*time.Millisecond,ReconnectMin:10*time.Millisecond,ReconnectMax:40*time.Millisecond}
 done:=make(chan error,1);go func(){done<-daemon.Run(ctx)}()
 defer func(){cancel();select{case <-done:case <-time.After(time.Second):t.Error("Node stuck")}}()
 for {status,e:=manager.Status(ctx,identity.Node);if e==nil&&status.Online&&!status.LastHeartbeatAt.IsZero(){break}
  select{case <-ctx.Done():t.Fatal("Node not ready");case <-time.After(10*time.Millisecond):}}
 original:=[]byte("good video")
 type projection struct{chat,thread,text string}
 delivered:=make(chan projection,40)
 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path {
  case "/botTOKEN/getFile":_,_=fmt.Fprintf(w,`{"ok":true,"result":{"file_path":"clip.mp4","file_size":%d}}`,len(original))
  case "/file/botTOKEN/clip.mp4":_,_=w.Write(original)
  case "/botTOKEN/createForumTopic":_,_=io.WriteString(w,`{"ok":true,"result":{"message_thread_id":77,"name":"video-worker"}}`)
  case "/botTOKEN/sendMessage":
   if e:=r.ParseForm();e!=nil{t.Errorf("Telegram form: %v",e)}
   delivered<-projection{chat:r.Form.Get("chat_id"),thread:r.Form.Get("message_thread_id"),text:r.Form.Get("text")}
   _,_=io.WriteString(w,`{"ok":true,"result":true}`)
  case "/botTOKEN/sendChatAction":_,_=io.WriteString(w,`{"ok":true,"result":true}`)
  case "/botTOKEN/getUpdates":
   _=r.ParseForm()
   switch r.Form.Get("offset"){
   case "500":
    _,_=fmt.Fprintf(w,`{"ok":true,"result":[{"update_id":500,"message":{"message_id":51,"from":{"id":100},"chat":{"id":100,"type":"private"},"caption":"Describe the sampled video","video":{"file_id":"video-file","file_unique_id":"unique-video","file_name":"clip.mp4","mime_type":"video/mp4","file_size":%d,"duration":2}}}]}`,len(original))
   case "501":
    _,_=fmt.Fprintf(w,`{"ok":true,"result":[{"update_id":501,"message":{"message_id":52,"from":{"id":100},"chat":{"id":-200,"type":"supergroup","is_forum":true},"message_thread_id":77,"caption":"Describe the sampled video","video":{"file_id":"topic-video","file_unique_id":"unique-topic-video","file_name":"clip.mp4","mime_type":"video/mp4","file_size":%d,"duration":2}}}]}`,len(original))
   default:_,_=io.WriteString(w,`{"ok":true,"result":[]}`)
   }
  default:t.Errorf("unexpected Bot endpoint %s",r.URL.Path);http.NotFound(w,r)
  }
 }));defer bot.Close()
 transport:=&telegram.BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 state:=filepath.Join(t.TempDir(),"telegram.json")
 group:=int64(-200)
 adapter,err:=telegram.New(telegram.Config{StatePath:state,OwnerChatID:100,GroupChatID:&group},transport,&telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t13-secret"})
 if err!=nil{t.Fatal(err)}
 updates,err:=transport.GetUpdates(ctx,500,time.Second)
 if err!=nil||len(updates)!=1{t.Fatalf("Telegram Bot API DM video decode: %+v %v",updates,err)}
 update:=updates[0]
 if update.Message==nil||update.Message.Video==nil||update.Message.Video.FileUniqueID!="unique-video"{t.Fatalf("Telegram original video DTO: %+v",update)}
 if err:=adapter.HandleUpdate(ctx,update);err!=nil{t.Fatal(err)}
 if err:=adapter.PollOnce(ctx);err!=nil{t.Fatal(err)}
 var entry core.ConversationEntry
 for entry.ID=="" {
  entries,e:=store.EntriesTail(ctx,conversation.ID,0,30)
  if e!=nil{t.Fatal(e)}
  for _,v:=range entries {if strings.Contains(v.Body,"Telegram video (update 500") {entry=v;break}}
  if entry.ID!=""{break}
  select{case <-ctx.Done():t.Fatal("Telegram video not posted");case <-time.After(10*time.Millisecond):}
 }
 if !strings.Contains(entry.Body,"Describe the sampled video")||!strings.Contains(entry.Body,"file_unique_id unique-video"){t.Fatalf("caption/identity lost: %q",entry.Body)}
 artifactIDs:=node.ArtifactIDs(entry.Body)
 if len(artifactIDs)!=1{t.Fatalf("missing original Artifact: %q",entry.Body)}
 for {
  originalArtifact,body,e:=store.ArtifactForPerson(ctx,api.OwnerID(),artifactIDs[0])
  if e!=nil&&strings.Contains(e.Error(),"database is locked"){
   select{case <-ctx.Done():t.Fatal("artifact read blocked by SQLite writer");case <-time.After(10*time.Millisecond):continue}
  }
  if e!=nil||!bytes.Equal(body,original)||originalArtifact.ProcessingStatus!="ready"||len(originalArtifact.VideoFrames)!=2{
   t.Fatalf("T12 pipeline failed: %+v %v",originalArtifact,e)
  }
  if originalArtifact.VideoFrames[1].TimestampMS!=1000{t.Fatalf("timestamps: %+v",originalArtifact.VideoFrames)}
  break
 }
 select{case native:=<-runtime.seen:
  if len(native.Images)!=2||!strings.Contains(native.Task,"Describe the sampled video") {t.Fatalf("native Worker did not see frames: %+v",native)}
 case e:=<-secretaryErrors:t.Fatalf("automatic Secretary MCP routing: %v",e)
 case e:=<-secRuntime.Errors():t.Fatalf("Secretary runtime failed before routing: %v",e)
 case <-time.After(8*time.Second):
  checkCtx,stop:=context.WithTimeout(context.Background(),time.Second)
  defer stop()
  turns,_:=store.SecretaryTurns(checkCtx,secretaryID.ID)
  workers,_:=store.WorkersForConversation(checkCtx,conversation.ID)
  t.Fatalf("automatic Telegram ingress did not dispatch: SecretaryTurns=%+v Workers=%+v",turns,workers)
 case <-ctx.Done():t.Fatal("Worker never received frames")}
 for {
  entries,e:=store.EntriesTail(ctx,conversation.ID,0,40)
  if e!=nil{t.Fatal(e)}
  found:=false
  for _,v:=range entries{if strings.Contains(v.Body,"Observed sampled frames at 0.000"){found=true;break}}
  if found{break}
  select{case <-ctx.Done():t.Fatal("Worker result not in Personal Conversation");case <-time.After(20*time.Millisecond):}
 }
 if err:=adapter.HandleUpdate(ctx,update);err!=nil{t.Fatal(err)}
 restored,err:=telegram.New(telegram.Config{StatePath:state,OwnerChatID:100,GroupChatID:&group},transport,&telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t13-secret"})
 if err!=nil{t.Fatal(err)}
 if err:=restored.HandleUpdate(ctx,update);err!=nil{t.Fatal(err)}
 select{case <-runtime.seen:t.Fatal("replay started another Worker");case <-time.After(100*time.Millisecond):}
 workers,err:=store.WorkersForConversation(ctx,conversation.ID)
 if err!=nil||len(workers)!=1{t.Fatalf("automatic Telegram ingress did not route to exactly one Worker: %+v %v",workers,err)}
 workerRef:=workers[0].WorkerRef
 if err:=adapter.HandleEvent(ctx,telegram.Event{Kind:"worker.created",WorkerRef:workerRef,Title:"video-worker",TaskPrompt:"Describe the sampled video"});err!=nil{t.Fatal(err)}
 if err:=adapter.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 topic,err:=transport.GetUpdates(ctx,501,time.Second)
 if err!=nil||len(topic)!=1||topic[0].Message.Video==nil{t.Fatalf("Bot API topic ingress failed: %+v %v",topic,err)}
 if err:=adapter.HandleUpdate(ctx,topic[0]);err!=nil{t.Fatal(err)}
 select{case native:=<-runtime.seen:
  if len(native.Images)!=2||!strings.Contains(native.Task,"update 501, chat -200, message 52")||!strings.Contains(native.Task,"1.000 s"){
   t.Fatalf("assigned Topic -> resumed native Worker lost frame mapping: %+v",native)
  }
 case <-ctx.Done():t.Fatal("Telegram Topic video never reached assigned native Worker")}
 var outcome core.Phase4Result
 for {
  details,e:=store.WorkerDetailsForConversation(ctx,conversation.ID,workerRef)
  if e!=nil{t.Fatal(e)}
  for _,result:=range details.Results {
   if strings.Contains(result.Summary,"T13 topic native Worker analyzed sampled video"){outcome=result}
  }
  if outcome.ID!=""{break}
  select{case <-ctx.Done():t.Fatal("native Topic Result not recorded");case <-time.After(20*time.Millisecond):}
 }
 // Bridge the actual durable Server Result event, not a fabricated Result.
 events,err:=store.EventsAfterSeq(ctx,0,500)
 if err!=nil{t.Fatal(err)}
 var terminal telegram.Event
 for _,source:=range events{
  if source.Kind!="attempt.outcome_recorded"&&source.Kind!="result.accepted"{continue}
  if !strings.Contains(string(source.Payload),outcome.Summary){continue}
  ref:=source.WorkerRef
  if ref==""&&source.AggregateType=="worker"{ref=source.AggregateID}
  if ref!=workerRef{continue}
  identity:="turn:"+outcome.TurnID
  if source.CorrelationID!=""{identity="turn:"+source.CorrelationID}
  terminal=telegram.Event{EventID:source.ID,Sequence:source.Seq,Kind:"worker.completed",
   WorkerRef:workerRef,Title:"video-worker",Text:outcome.Summary,
   TerminalIdentity:identity,Payload:source.Payload}
  break
 }
 if terminal.Sequence==0{t.Fatal("no canonical Server terminal event for native Telegram Topic Worker")}
 if err:=adapter.HandleDurableEvent(ctx,terminal);err!=nil{t.Fatal(err)}
 if err:=adapter.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 if err:=adapter.HandleDurableEvent(ctx,terminal);err!=nil{t.Fatal(err)}
 terminal.Sequence++
 if err:=adapter.HandleDurableEvent(ctx,terminal);err!=nil{t.Fatal(err)}
 if err:=adapter.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 counts:=map[string]int{}
 for {select{case p:=<-delivered:
  if strings.Contains(p.text,"T13 topic native Worker analyzed sampled video"){counts[p.chat+":"+p.thread]++}
 default:goto counted}}
 counted:
 if counts["100:"]!=1||counts["-200:"]!=1||counts["-200:77"]!=1{
  t.Fatalf("Topic native Result projections are missing or duplicated: %+v",counts)
 }
 if err:=adapter.HandleUpdate(ctx,topic[0]);err!=nil{t.Fatal(err)}
 afterTopicRestart,err:=telegram.New(telegram.Config{StatePath:state,OwnerChatID:100,GroupChatID:&group},transport,
  &telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t13-secret"})
 if err!=nil{t.Fatal(err)}
 if err:=afterTopicRestart.HandleUpdate(ctx,topic[0]);err!=nil{t.Fatal(err)}
 select{case duplicate:=<-runtime.seen:t.Fatalf("Topic replay produced duplicate native execution: %+v",duplicate)
 case <-time.After(100*time.Millisecond):}
}
