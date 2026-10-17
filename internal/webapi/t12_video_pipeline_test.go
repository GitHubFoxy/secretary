package webapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

var videoTestWebP,_=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")

type t12Processor struct {
	started chan struct{}
	release chan struct{}
	once sync.Once
}
func (p *t12Processor) Process(ctx context.Context, original []byte, settings VideoOptions) ([]core.VideoFrameData,error) {
	if !strings.Contains(string(original),"good video") { return nil,errors.New("transcode failed intentionally") }
	p.once.Do(func(){close(p.started)})
	select {case <-p.release:case <-ctx.Done():return nil,ctx.Err()}
	return []core.VideoFrameData{{TimestampMS:0,Data:videoTestWebP},{TimestampMS:1000,Data:videoTestWebP}},nil
}

type t12Runtime struct {
	seen chan node.StartRequest
}
func (r *t12Runtime) Start(_ context.Context, req node.StartRequest) (node.Session,error) {
	r.seen<-req
	if len(req.Images)!=2 || !bytes.Equal(req.Images[0].Data,videoTestWebP) || !bytes.Equal(req.Images[1].Data,videoTestWebP) ||
		req.Images[0].MediaType!="image/webp" || req.Images[1].MediaType!="image/webp" ||
		!strings.Contains(req.Task,"0.000 s") || !strings.Contains(req.Task,"1.000 s") ||
		!strings.Contains(req.Task,"between frames were NOT observed") ||
		!strings.Contains(req.Task,"Describe the sampled video") {
		return nil,fmt.Errorf("video native image input/caption mismatch: %+v",req)
	}
	result:=make(chan node.Result,1)
	result<-node.Result{Status:"succeeded",Summary:"Observed sampled frames at 0.000 and 1.000 seconds; gaps unobserved"}
	return &artifactWorkerSession{result:result},nil
}

func TestT12WebVideoAsyncToRemoteNodeWorkerResultAndFailure(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
	defer cancel()
	store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"video.db"))
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	api,err:=New(ctx,store,"bootstrap")
	if err!=nil{t.Fatal(err)}
	if err=api.AttachVideoOptions(VideoOptions{FPS:2,MaxFrames:12,MaxDurationSeconds:12,TimeoutSeconds:10});err!=nil{t.Fatal(err)}
	processor:=&t12Processor{started:make(chan struct{}),release:make(chan struct{})}
	api.AttachVideoProcessor(processor)
	credential,err:=store.RotateSecretaryCapability(ctx,api.OwnerID())
	if err!=nil{t.Fatal(err)}
	manager,err:=node.NewServerManager(ctx,store,"t12-pair","t12-admin")
	if err!=nil{t.Fatal(err)}
	manager.SetEventSink(node.NewStoreEventSink(store))
	manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	api.AttachNodeService(manager)
	api.AttachSecretaryWorkerTools(ctl.WorkerService{
		Store:store,PersonID:api.OwnerID(),Capability:credential,
		WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
		WorkerProfileSource:func()(node.ManagedProfile,error){
			return node.ManagedProfile{Version:"t12",Name:"worker",Content:"Analyze WebP video frame samples",Hash:"t12",Runtime:"fx",AllowTools:[]string{"read"}},nil
		},Runtime:ctl.NodeRuntime{Manager:manager},
	})
	server:=httptest.NewServer(api.Handler())
	defer server.Close()
	identity,err:=node.EnrollNode(ctx,server.Client(),server.URL,"t12-pair","t12-remote")
	if err!=nil{t.Fatal(err)}
	instance:=core.HarnessInstance{
		ID:core.HarnessInstanceID(string(identity.Node)+"/fx"),Node:identity.Node,Kind:core.HarnessFX,
		Version:"test",Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityImageInput}},
	}
	inventory:=core.HarnessInventorySnapshot{Node:identity.Node,ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{instance}}
	runtime:=&t12Runtime{seen:make(chan node.StartRequest,2)}
	local,err:=node.OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err!=nil{t.Fatal(err)}
	defer local.Close()
	daemon:=&node.Daemon{Identity:identity,Store:local,Runtime:runtime,Inventory:t10InventorySource{snapshot:inventory},
		Capacity:2,HeartbeatInterval:20*time.Millisecond,InventoryInterval:time.Hour,OutboxPollInterval:10*time.Millisecond,
		ReconnectMin:10*time.Millisecond,ReconnectMax:40*time.Millisecond}
	done:=make(chan error,1)
	go func(){done<-daemon.Run(ctx)}()
	defer func(){cancel();select{case <-done:case <-time.After(time.Second):t.Error("Node stuck")}}()
	for {
		state,e:=manager.Status(ctx,identity.Node)
		if e==nil&&state.Online&&!state.LastHeartbeatAt.IsZero(){break}
		select {case <-ctx.Done():t.Fatal("remote Node not ready");case <-time.After(10*time.Millisecond):}
	}
	jar,err:=cookiejar.New(nil)
	if err!=nil{t.Fatal(err)}
	client:=&http.Client{Jar:jar}
	login(t,client,server.URL)
	post:=func(path,key string,body []byte)(int,[]byte){
		t.Helper()
		req,e:=http.NewRequestWithContext(ctx,http.MethodPost,server.URL+path,bytes.NewReader(body))
		if e!=nil{t.Fatal(e)}
		req.Header.Set("Idempotency-Key",key)
		res,e:=client.Do(req)
		if e!=nil{t.Fatal(e)}
		defer res.Body.Close()
		var buf bytes.Buffer
		if _,e=buf.ReadFrom(res.Body);e!=nil{t.Fatal(e)}
		return res.StatusCode,buf.Bytes()
	}
	status,raw:=post("/v1/artifacts?name=clip.mp4","t12-up-1",[]byte("good video"),)
	if status!=http.StatusCreated{t.Fatalf("upload %d %s",status,raw)}
	var uploaded struct{Reference string `json:"reference"`;Artifact core.Artifact `json:"artifact"`}
	if e:=json.Unmarshal(raw,&uploaded);e!=nil{t.Fatal(e)}
	if uploaded.Artifact.ProcessingStatus!="processing" {t.Fatalf("upload state %s",uploaded.Artifact.ProcessingStatus)}
	<-processor.started
	status,replay:=post("/v1/artifacts?name=clip.mp4","t12-up-1",[]byte("good video"))
	if status!=http.StatusOK || !bytes.Contains(replay,[]byte(uploaded.Reference)){t.Fatalf("video retry %d %s",status,replay)}
	caption:="Describe the sampled video "+uploaded.Reference
	message,_:=json.Marshal(map[string]string{"external_message_id":"t12-inbound","body":caption})
	status,raw=post("/v1/messages","t12-message",message)
	if status!=http.StatusAccepted || !bytes.Contains(raw,[]byte(uploaded.Reference)){t.Fatalf("caption %d %s",status,raw)}
	var ack struct { Entry core.ConversationEntry `json:"entry"` }
	if e:=json.Unmarshal(raw,&ack);e!=nil{t.Fatal(e)}
	unrelatedStart:=time.Now()
	res,e:=client.Get(server.URL+"/v1/conversation")
	if e!=nil{t.Fatal(e)}
	res.Body.Close()
	if res.StatusCode!=200 || time.Since(unrelatedStart)>time.Second {t.Fatal("unrelated chat blocked by media conversion")}
	conversation,e:=store.ConversationForPerson(ctx,api.OwnerID())
	if e!=nil{t.Fatal(e)}
	sec,e:=store.EnsureSecretaryIdentity(ctx,api.OwnerID(),conversation.ID)
	if e!=nil{t.Fatal(e)}
	if e=store.SetSecretaryPolicySnapshot(ctx,core.SecretaryPolicySnapshot{Version:"t12",Harness:"fx",Model:"fixture",Reasoning:"high",ProfileVersion:"t12",ProfileName:"secretary",ProfileHash:"t12",ProfileContent:"test"});e!=nil{t.Fatal(e)}
	turn,duplicate,e:=store.EnqueueSecretaryTurnForEntry(ctx,sec.ID,caption,ack.Entry,nil)
	if e!=nil||duplicate{t.Fatalf("Secretary turn %v duplicate %v",e,duplicate)}
	if _,e=store.StartSecretaryTurn(ctx,turn.ID);e!=nil{t.Fatal(e)}
	action,_:=json.Marshal(map[string]any{"name":"spawn_worker","arguments":map[string]any{
		"intent":"Describe the sampled video","idempotency_key":"t12-action",
		"preferences":map[string]any{"node_id":identity.Node,"harness_kind":"fx"},
	}})
	actionDone:=make(chan error,1)
	go func(){
		actionReq,e:=http.NewRequestWithContext(ctx,http.MethodPost,server.URL+"/v1/internal/secretary/tools/call",bytes.NewReader(action))
		if e!=nil {actionDone<-e;return}
		actionReq.Header.Set("Authorization","Bearer "+credential)
		actionReq.Header.Set("Content-Type","application/json")
		actionRes,e:=client.Do(actionReq)
		if e!=nil {actionDone<-e;return}
		defer actionRes.Body.Close()
		var toolResponse secretaryToolCallResponse
		e=json.NewDecoder(actionRes.Body).Decode(&toolResponse)
		if e!=nil||actionRes.StatusCode!=http.StatusOK||toolResponse.Error!=""{
			actionDone<-fmt.Errorf("tool %d %v %#v",actionRes.StatusCode,e,toolResponse);return
		}
		actionDone<-nil
	}()
	actionReturned:=false
	select {
	case e:=<-actionDone:
		if e!=nil {t.Fatalf("dependent dispatch request rejected: %v",e)}
		actionReturned=true
	case <-time.After(100*time.Millisecond):
	}
	select {case early:=<-runtime.seen:t.Fatalf("Worker started before video conversion succeeded: Task=%q Images=%d",early.Task,len(early.Images));default:}
	close(processor.release)
	if !actionReturned {
		select{case e:=<-actionDone:if e!=nil{t.Fatalf("dependent action failed after conversion: %v",e)};case <-ctx.Done():t.Fatal("dependent action remained blocked")}
	}
	select{case received:=<-runtime.seen:
		if len(received.Images)!=2 || !strings.Contains(received.Task,uploaded.Reference){t.Fatalf("lost native video input: %+v",received)}
	case <-time.After(8*time.Second):
		job,_:=store.VideoJob(context.Background(),api.OwnerID(),uploaded.Artifact.ID)
		workers,_:=store.WorkersForConversation(context.Background(),conversation.ID)
		var detail any
		if len(workers)>0 { detail,_=store.WorkerDetailsForConversation(context.Background(),conversation.ID,workers[0].WorkerRef) }
		t.Fatalf("remote Worker did not start: job=%+v details=%+v",job,detail)
	case <-ctx.Done():t.Fatal("remote Worker never received frame images")}
	original,body,e:=store.ArtifactForPerson(ctx,api.OwnerID(),uploaded.Artifact.ID)
	if e!=nil || !bytes.Equal(body,[]byte("good video")) || original.ProcessingStatus!="ready" ||
		len(original.VideoFrames)!=2 || original.VideoFrames[1].TimestampMS!=1000 {t.Fatalf("original/frames %+v %v",original,e)}
	fetched,e:=identity.FetchArtifact(ctx,original.ID)
	if e!=nil||len(fetched.VideoFrames)!=2||fetched.VideoStatus!="ready"{t.Fatalf("remote video metadata %+v %v",fetched,e)}
	for {
		response,e:=client.Get(server.URL+"/v1/conversation")
		if e!=nil{t.Fatal(e)}
		var result bytes.Buffer
		_,_=result.ReadFrom(response.Body);response.Body.Close()
		if bytes.Contains(result.Bytes(),[]byte("Observed sampled frames at 0.000")){break}
		select{case <-ctx.Done():t.Fatalf("no canonical Worker result: %s",result.String());case <-time.After(20*time.Millisecond):}
	}
	status,raw=post("/v1/artifacts?name=corrupt.webm","t12-bad",[]byte("bad video"))
	if status!=http.StatusCreated{t.Fatalf("failed source was not retained: %d %s",status,raw)}
	var failed struct{Artifact core.Artifact `json:"artifact"`}
	if e:=json.Unmarshal(raw,&failed);e!=nil{t.Fatal(e)}
	for {
		a,bytes,e:=store.ArtifactForPerson(ctx,api.OwnerID(),failed.Artifact.ID)
		if e!=nil||string(bytes)!="bad video"{t.Fatalf("failed clip original lost %v",e)}
		if a.ProcessingStatus=="failed" {
			if !strings.Contains(a.ProcessingError,"transcode failed"){t.Fatalf("missing conversion error %+v",a)}
			break
		}
		select{case <-ctx.Done():t.Fatal("failed video stuck");case <-time.After(10*time.Millisecond):}
	}
	_,_,e=node.PrepareArtifactInput(ctx,t.TempDir(),"inspect artifact://"+failed.Artifact.ID,identity.FetchArtifact)
	if e==nil||!strings.Contains(e.Error(),"could not be processed"){t.Fatalf("failed video accepted: %v",e)}
	if e=api.AttachVideoOptions(VideoOptions{FPS:5});e==nil{t.Fatal("unsupported FPS accepted")}
	status,_=post("/v1/artifacts?name=too-big.mp4","t12-oversize",bytes.Repeat([]byte{'x'},core.MaxArtifactBytes+1))
	if status!=http.StatusRequestEntityTooLarge{t.Fatalf("oversize video = %d",status)}
}
