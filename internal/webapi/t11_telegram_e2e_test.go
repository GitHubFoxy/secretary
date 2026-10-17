package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/telegram"
)

type t11ImageRuntime struct {
	expected []byte
	seen chan node.StartRequest
}

func (rt *t11ImageRuntime) Start(_ context.Context, request node.StartRequest) (node.Session,error) {
	if len(request.Images)!=1 || request.Images[0].MediaType!="image/jpeg" || !bytes.Equal(request.Images[0].Data,rt.expected) || !strings.Contains(request.Task,"artifact://"+request.Images[0].ArtifactID) || !strings.Contains(request.Task,"What is in the picture?") {
		return nil,fmt.Errorf("Telegram image bytes/caption not bound to native Worker: %#v",request)
	}
	rt.seen <- request
	out:=make(chan node.Result,1)
	out<-node.Result{Status:"succeeded",Summary:"T11 native Worker analyzed the Telegram photo"}
	return &artifactWorkerSession{result:out},nil
}

type t11FollowupSession struct {
	*artifactWorkerSession
	expected []byte
	seen chan node.StartRequest
}

func (s *t11FollowupSession) PromptImages(_ context.Context, prompt string, images []node.ImageInput) error {
	if len(images)!=1 || images[0].MediaType!="image/jpeg" || !bytes.Equal(images[0].Data,s.expected) || !strings.Contains(prompt,"artifact://"+images[0].ArtifactID) {
		return fmt.Errorf("continued native Worker received no Telegram image: %d",len(images))
	}
	s.seen <- node.StartRequest{Task:prompt,Images:images}
	s.result <- node.Result{Status:"succeeded",Summary:"T11 continued Worker analyzed the Telegram Topic photo"}
	return nil
}

func (rt *t11ImageRuntime) Resume(_ context.Context, _ node.StartRequest, runtimeSessionID string) (node.Session,error) {
	if runtimeSessionID!="artifact-worker-session" {
		return nil,fmt.Errorf("unexpected native session identity: %s",runtimeSessionID)
	}
	return &t11FollowupSession{
		artifactWorkerSession:&artifactWorkerSession{result:make(chan node.Result,1)},
		expected:rt.expected,seen:rt.seen,
	},nil
}

func TestT11RealTelegramBotAPIAdapterServerRemoteNodeAndTopic(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
	defer cancel()
	store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"))
	if err!=nil {t.Fatal(err)}
	defer store.Close()
	api,err:=New(ctx,store,"bootstrap")
	if err!=nil {t.Fatal(err)}
	api.AttachInternalCredential("t11-telegram-secret")
	capability,err:=store.RotateSecretaryCapability(ctx,api.OwnerID())
	if err!=nil {t.Fatal(err)}
	manager,err:=node.NewServerManager(ctx,store,"t11-pair","t11-admin")
	if err!=nil {t.Fatal(err)}
	manager.SetEventSink(node.NewStoreEventSink(store))
	manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	api.AttachNodeService(manager)
	service:=ctl.WorkerService{
		Store:store,PersonID:api.OwnerID(),Capability:capability,
		WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
		WorkerProfileSource:func()(node.ManagedProfile,error){
			return node.ManagedProfile{Version:"t11",Name:"worker",Content:"Describe incoming Telegram photo.",Hash:"t11",Runtime:"fx",AllowTools:[]string{"read"}},nil
		},
		Runtime:ctl.NodeRuntime{Manager:manager},
	}
	api.AttachSecretaryWorkerTools(service)
	api.AttachWorkerResponder(service)
	httpServer:=httptest.NewServer(api.Handler())
	defer httpServer.Close()
	identity,err:=node.EnrollNode(ctx,httpServer.Client(),httpServer.URL,"t11-pair","t11-remote")
	if err!=nil {t.Fatal(err)}
	instance:=core.HarnessInstance{
		ID:core.HarnessInstanceID(string(identity.Node)+"/fx"),Node:identity.Node,Kind:core.HarnessFX,
		Version:"test",Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityCancel,core.CapabilityImageInput}},
	}
	inventory:=core.HarnessInventorySnapshot{Node:identity.Node,ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{instance}}
	picture:=image.NewRGBA(image.Rect(0,0,3,3))
	picture.Set(1,1,color.RGBA{R:255,A:255})
	var encoded bytes.Buffer
	if err:=jpeg.Encode(&encoded,picture,&jpeg.Options{Quality:90});err!=nil {t.Fatal(err)}
	original:=encoded.Bytes()
	runtime:=&t11ImageRuntime{expected:original,seen:make(chan node.StartRequest,3)}
	localStore,err:=node.OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err!=nil {t.Fatal(err)}
	defer localStore.Close()
	daemon:=&node.Daemon{
		Identity:identity,Store:localStore,Runtime:runtime,Inventory:t10InventorySource{snapshot:inventory},
		Capacity:2,HeartbeatInterval:20*time.Millisecond,InventoryInterval:time.Hour,
		OutboxPollInterval:10*time.Millisecond,ReconnectMin:10*time.Millisecond,ReconnectMax:40*time.Millisecond,
	}
	done:=make(chan error,1)
	go func(){done<-daemon.Run(ctx)}()
	defer func(){
		cancel()
		select {case err:=<-done:if err!=nil {t.Errorf("daemon: %v",err)}
		case <-time.After(time.Second):t.Error("daemon did not stop")}
	}()
	for {
		status,err:=manager.Status(ctx,identity.Node)
		if err==nil && status.Online && !status.LastHeartbeatAt.IsZero(){break}
		select {case <-ctx.Done():t.Fatalf("Node never ready: %v",ctx.Err())
		case <-time.After(10*time.Millisecond):}
	}
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path {
		case "/botTOKEN/getUpdates":
			_ = r.ParseForm()
			var update string
			switch r.Form.Get("offset"){
			case "500":update=`[{"update_id":500,"message":{"message_id":51,"from":{"id":100},"chat":{"id":100,"type":"private"},"caption":"What is in the picture?","photo":[{"file_id":"photo-one","file_unique_id":"unique-photo-one","file_size":1024,"width":3,"height":3}]}}]`
			case "501":update=`[{"update_id":501,"message":{"message_id":52,"from":{"id":100},"chat":{"id":-200,"type":"supergroup","is_forum":true},"message_thread_id":77,"caption":"What is in the picture?","photo":[{"file_id":"photo-two","file_unique_id":"unique-photo-two","file_size":1024,"width":3,"height":3}]}}]`
			default:update="[]"
			}
			_,_=io.WriteString(w,`{"ok":true,"result":`+update+"}")
		case "/botTOKEN/getFile":
			_,_=io.WriteString(w,fmt.Sprintf(`{"ok":true,"result":{"file_path":"original.jpg","file_size":%d}}`,len(original)))
		case "/file/botTOKEN/original.jpg":
			w.Header().Set("Content-Type","image/jpeg")
			_,_=w.Write(original)
		case "/botTOKEN/createForumTopic":
			_,_=io.WriteString(w,`{"ok":true,"result":{"message_thread_id":77,"name":"photo-worker"}}`)
		case "/botTOKEN/sendMessage","/botTOKEN/sendChatAction":
			_,_=io.WriteString(w,`{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected Bot API path: %s",r.URL.Path)
			http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	group:=int64(-200)
	transport:=&telegram.BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
	adapterPath:=filepath.Join(t.TempDir(),"telegram-state.json")
	adapter,err:=telegram.New(telegram.Config{StatePath:adapterPath,OwnerChatID:100,GroupChatID:&group},transport,
		&telegram.HTTPServerClient{BaseURL:httpServer.URL,Credential:"t11-telegram-secret"})
	if err!=nil {t.Fatal(err)}
	inbound,err:=transport.GetUpdates(ctx,500,time.Second)
	if err!=nil || len(inbound)!=1 {t.Fatalf("Bot photo decode: %v %+v",err,inbound)}
	if err:=adapter.HandleUpdate(ctx,inbound[0]);err!=nil {t.Fatal(err)}
	conversation,err:=store.ConversationForPerson(ctx,api.OwnerID())
	if err!=nil {t.Fatal(err)}
	entries,err:=store.EntriesTail(ctx,conversation.ID,0,30)
	if err!=nil {t.Fatal(err)}
	var entry core.ConversationEntry
	for _,v:=range entries {if strings.Contains(v.Body,"update 500, chat 100, message 51") {entry=v;break}}
	if entry.ID=="" || !strings.Contains(entry.Body,"What is in the picture?") || !strings.Contains(entry.Body,"artifact://art_") {t.Fatalf("Telegram image did not reach canonical conversation: %+v",entries)}
	secretaryID,err:=store.EnsureSecretaryIdentity(ctx,api.OwnerID(),conversation.ID)
	if err!=nil {t.Fatal(err)}
	if err:=store.SetSecretaryPolicySnapshot(ctx,core.SecretaryPolicySnapshot{
		Version:"t11",Harness:"fx",Model:"fixture",Reasoning:"high",ProfileVersion:"t11",ProfileName:"secretary",ProfileHash:"t11",ProfileContent:"test",
	});err!=nil {t.Fatal(err)}
	turn,duplicate,err:=store.EnqueueSecretaryTurnForEntry(ctx,secretaryID.ID,entry.Body,entry,nil)
	if err!=nil || duplicate {t.Fatalf("Secretary input: %v duplicate=%v",err,duplicate)}
	if _,err:=store.StartSecretaryTurn(ctx,turn.ID);err!=nil {t.Fatal(err)}
	action,_:=json.Marshal(map[string]any{"name":"spawn_worker","arguments":map[string]any{
		"intent":"What is in the picture?","idempotency_key":"t11-bound-action",
		"preferences":map[string]any{"node_id":identity.Node,"harness_kind":"fx"},
	}})
	call:=func()map[string]any{
		t.Helper()
		req,err:=http.NewRequestWithContext(ctx,http.MethodPost,httpServer.URL+"/v1/internal/secretary/tools/call",bytes.NewReader(action))
		if err!=nil {t.Fatal(err)}
		req.Header.Set("Authorization","Bearer "+capability)
		req.Header.Set("Content-Type","application/json")
		res,err:=httpServer.Client().Do(req)
		if err!=nil {t.Fatal(err)}
		defer res.Body.Close()
		var out struct {Value map[string]any `json:"value"`;Error string `json:"error"`}
		if err:=json.NewDecoder(res.Body).Decode(&out);err!=nil {t.Fatal(err)}
		if res.StatusCode!=http.StatusOK || out.Error!="" {t.Fatalf("spawn_worker: HTTP %d %s",res.StatusCode,out.Error)}
		return out.Value
	}
	created:=call()
	worker:=created["worker"].(map[string]any)["worker_ref"].(string)
	select {case request:=<-runtime.seen:
		if !strings.Contains(entry.Body,"file_unique_id unique-photo-one") || !strings.Contains(request.Task,"artifact://"+request.Images[0].ArtifactID) {t.Fatalf("original image identity not preserved: entry=%q task=%q",entry.Body,request.Task)}
	case <-ctx.Done():t.Fatal("Telegram DM photo not delivered to Node")}
	again:=call()
	if again["worker"].(map[string]any)["worker_ref"]!=worker {t.Fatal("replay created another Worker")}
	for {
		entries,err=store.EntriesTail(ctx,conversation.ID,0,40)
		if err!=nil {t.Fatal(err)}
		found:=false
		for _,v:=range entries {if strings.Contains(v.Body,"T11 native Worker analyzed the Telegram photo"){found=true;break}}
		if found {break}
		select {case <-ctx.Done():t.Fatal("Worker Result not visible in canonical conversation")
		case <-time.After(20*time.Millisecond):}
	}
	if err:=adapter.HandleEvent(ctx,telegram.Event{Kind:"worker.created",WorkerRef:worker,Title:"photo-worker",TaskPrompt:"What is in the picture?"});err!=nil {t.Fatal(err)}
	if err:=adapter.SyncWorkerTopics(ctx);err!=nil {t.Fatal(err)}
	topic,err:=transport.GetUpdates(ctx,501,time.Second)
	if err!=nil || len(topic)!=1 {t.Fatalf("topic Bot API update: %v %+v",err,topic)}
	if err:=adapter.HandleUpdate(ctx,topic[0]);err!=nil {t.Fatal(err)}
	select {case received:=<-runtime.seen:
		if !strings.Contains(received.Task,"update 501, chat -200, message 52") {t.Fatalf("wrong direct Worker source: %s",received.Task)}
	case <-ctx.Done():t.Fatal("direct Worker Topic photo did not reach Node")}
	if err:=adapter.HandleUpdate(ctx,topic[0]);err!=nil {t.Fatal(err)}
	afterRestart,err:=telegram.New(telegram.Config{StatePath:adapterPath,OwnerChatID:100,GroupChatID:&group},transport,
		&telegram.HTTPServerClient{BaseURL:httpServer.URL,Credential:"t11-telegram-secret"})
	if err!=nil {t.Fatal(err)}
	if err:=afterRestart.HandleUpdate(ctx,topic[0]);err!=nil {t.Fatal(err)}
	select {case duplicate:=<-runtime.seen:t.Fatalf("replay made duplicate native execution: %+v",duplicate)
	case <-time.After(100*time.Millisecond):}
}
