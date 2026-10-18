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
  Version:"t13",Harness:"fx",Model:"fixture",Reasoning:"high",
  ProfileVersion:"t13",ProfileName:"secretary",ProfileHash:"t13",
  ProfileContent:"Delegate Telegram video to a Worker via MCP.",
  ReplyContractVersion:core.SecretaryReplyContractAddressedV1,
 });err!=nil{t.Fatal(err)}
 harness:=&t13SecretaryHarness{}
 tool:=mcp.Secretary{Workers:svc,ReplyContractVersion:core.SecretaryReplyContractAddressedV1}
 harness.onPrompt=func(prompt string)error{
  body,turnID,inputID,e:=t13SecretaryPromptIdentity(prompt)
  if e!=nil{return e}
  if !strings.Contains(body,"Telegram video (update 500")||!strings.Contains(body,"Describe the sampled video"){
   return fmt.Errorf("automatic Secretary routing lost Telegram ingress context")
  }
  args,e:=json.Marshal(map[string]any{
   "intent":"Describe the sampled video",
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
 profile:=node.ManagedProfile{Version:"t13",Name:"secretary",Content:"Delegate Telegram video to a Worker via MCP.",Runtime:"fx",
  ReplyContractVersion:core.SecretaryReplyContractAddressedV1}
 profile.Hash=node.HashProfile(profile.Content,nil,profile.Model,profile.Reasoning)
 secRuntime.AttachProfile(func()node.ManagedProfile{return profile})
 secRuntime.AttachMCP("fixture",t.TempDir())
 if err:=secRuntime.Start(ctx);err!=nil{t.Fatal(err)}
 defer secRuntime.Stop(context.Background())
 api.AttachSecretary(secRuntime)
 server:=httptest.NewServer(api.Handler());defer server.Close()
 identity,err:=node.EnrollNode(ctx,server.Client(),server.URL,"t13-pair","t13-remote")
 if err!=nil{t.Fatal(err)}
 runtime:=&t12Runtime{seen:make(chan node.StartRequest,3)}
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
 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path {
  case "/botTOKEN/getFile":_,_=fmt.Fprintf(w,`{"ok":true,"result":{"file_path":"clip.mp4","file_size":%d}}`,len(original))
  case "/file/botTOKEN/clip.mp4":_,_=w.Write(original)
  case "/botTOKEN/sendMessage","/botTOKEN/sendChatAction":_,_=io.WriteString(w,`{"ok":true,"result":true}`)
  case "/botTOKEN/getUpdates":_,_=io.WriteString(w,`{"ok":true,"result":[]}`)
  default:t.Errorf("unexpected Bot endpoint %s",r.URL.Path);http.NotFound(w,r)
  }
 }));defer bot.Close()
 transport:=&telegram.BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 state:=filepath.Join(t.TempDir(),"telegram.json")
 adapter,err:=telegram.New(telegram.Config{StatePath:state,OwnerChatID:100},transport,&telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t13-secret"})
 if err!=nil{t.Fatal(err)}
 update:=telegram.Update{ID:500,Message:&telegram.Message{ChatID:100,FromID:100,MessageID:51,Caption:"Describe the sampled video",
  Video:&telegram.Video{FileID:"video-file",FileUniqueID:"unique-video",FileName:"clip.mp4",MimeType:"video/mp4",FileSize:int64(len(original)),Duration:2}}}
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
 originalArtifact,body,err:=store.ArtifactForPerson(ctx,api.OwnerID(),artifactIDs[0])
 if err!=nil||!bytes.Equal(body,original)||originalArtifact.ProcessingStatus!="ready"||len(originalArtifact.VideoFrames)!=2{t.Fatalf("T12 pipeline failed: %+v %v",originalArtifact,err)}
 if originalArtifact.VideoFrames[1].TimestampMS!=1000{t.Fatalf("timestamps: %+v",originalArtifact.VideoFrames)}
 select{case native:=<-runtime.seen:
  if len(native.Images)!=2||!strings.Contains(native.Task,"Describe the sampled video") {t.Fatalf("native Worker did not see frames: %+v",native)}
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
 restored,err:=telegram.New(telegram.Config{StatePath:state,OwnerChatID:100},transport,&telegram.HTTPServerClient{BaseURL:server.URL,Credential:"t13-secret"})
 if err!=nil{t.Fatal(err)}
 if err:=restored.HandleUpdate(ctx,update);err!=nil{t.Fatal(err)}
 select{case <-runtime.seen:t.Fatal("replay started another Worker");case <-time.After(100*time.Millisecond):}
}
