package node

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type t12QueuedSocketFixture struct {
	manager *ServerManager
	identity NodeIdentity
	local *LocalStore
	originalID string
	queuedRequests chan struct{}
	ready atomic.Bool
	runtime Runtime
}
func t12SocketWithQueuedVideo(t *testing.T,runtime Runtime) *t12QueuedSocketFixture {
	t.Helper()
	ctx,cancel:=context.WithTimeout(context.Background(),12*time.Second)
	store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"))
	if err!=nil{cancel();t.Fatal(err)}
	manager,err:=NewServerManager(ctx,store,"pair-token","admin-token")
	if err!=nil{cancel();t.Fatal(err)}
	fixture:=&t12QueuedSocketFixture{manager:manager,originalID:"art_"+strings.Repeat("6",32),queuedRequests:make(chan struct{},8),runtime:runtime}
	frameID:="art_"+strings.Repeat("7",32)
	original:=[]byte("queued video")
	webp,err:=base64.RawStdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	sum:=func(b []byte)string{h:=sha256.Sum256(b);return hex.EncodeToString(h[:])}
	mux:=http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect",manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes",manager)
	mux.Handle("/v1/nodes/",manager)
	mux.HandleFunc("/v1/artifacts/",func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("X-Artifact-Signature")==""||r.Header.Get("X-Secretary-Node")!="macbook" {
			http.Error(w,"unauthorized",403);return
		}
		if strings.HasSuffix(r.URL.Path,"/status") {
			select{case fixture.queuedRequests<-struct{}{}:default:}
			status:="queued"
			if fixture.ready.Load(){status="ready"}
			_ = json.NewEncoder(w).Encode(core.Artifact{ID:fixture.originalID,ProcessingStatus:status,
				VideoFrames:[]core.VideoFrame{{ArtifactID:frameID,TimestampMS:0,TimestampKind:"source-pts-relative"}}})
			return
		}
		id:=strings.TrimPrefix(r.URL.Path,"/v1/artifacts/")
		var data []byte
		var name string
		if id==fixture.originalID {
			data,name=original,"clip.mp4"
			w.Header().Set("X-Artifact-Video-Status","queued")
		}else if id==frameID{data,name=webp,"frame.webp"
		}else{http.NotFound(w,r);return}
		w.Header().Set("X-Artifact-SHA256",sum(data))
		w.Header().Set("X-Artifact-Name",base64.RawURLEncoding.EncodeToString([]byte(name)))
		_,_=w.Write(data)
	})
	server:=httptest.NewServer(mux)
	identity,err:=EnrollNode(ctx,server.Client(),server.URL,"pair-token","macbook")
	if err!=nil{cancel();server.Close();t.Fatal(err)}
	fixture.identity=identity
	local,err:=OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err!=nil{cancel();server.Close();t.Fatal(err)}
	fixture.local=local
	inventory:=daemonInventoryFixture(identity.Node)
	inventory.Instances[0].Capabilities.Execution=append(inventory.Instances[0].Capabilities.Execution,core.CapabilityImageInput)
	daemon:=&Daemon{Identity:identity,Store:local,Runtime:runtime,Inventory:daemonStaticInventory{snapshot:inventory},
		Capacity:2,HeartbeatInterval:20*time.Millisecond,OutboxPollInterval:10*time.Millisecond,
		ReconnectMin:10*time.Millisecond,ReconnectMax:30*time.Millisecond}
	done:=make(chan error,1)
	go func(){done<-daemon.Run(ctx)}()
	t.Cleanup(func(){
		cancel()
		select{case e:=<-done:if e!=nil{t.Errorf("daemon exit: %v",e)}
		case <-time.After(2*time.Second):t.Error("daemon did not stop")}
		server.Close()
		_ = store.Close()
		_ = local.Close()
	})
	waitFor(t,ctx,"Node active socket",func()bool{
		manager.mu.Lock();connected:=manager.connections[identity.Node]!=nil;manager.mu.Unlock()
		return connected
	})
	return fixture
}
func (f *t12QueuedSocketFixture) dispatchCommand(id string) Command {
	cmd:=daemonDispatchFixture(f.identity.Node,id)
	instance:=daemonInventoryFixture(f.identity.Node).Instances[0]
	instance.Capabilities.Execution=append(instance.Capabilities.Execution,core.CapabilityImageInput)
	cmd.Dispatch.Envelope.HarnessInstance=instance
	return cmd
}

func TestT12QueuedDispatchSocketDisconnectReconnectAutoReplaysOnce(t *testing.T) {
	runtime:=&daemonCountingRuntime{}
	f:=t12SocketWithQueuedVideo(t,runtime)
	ctx,cancel:=context.WithTimeout(context.Background(),9*time.Second)
	defer cancel()
	outcomes:=make(chan CommandOutcome,4)
	f.manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,o CommandOutcome)error{outcomes<-o;return nil})
	cmd:=f.dispatchCommand("t12-ws-queued-disconnect")
	cmd.Dispatch.Envelope.OriginalUserIntent="Inspect artifact://"+f.originalID
	if e:=f.manager.SendCommand(ctx,f.identity.Node,cmd);e!=nil{t.Fatal(e)}
	select{case <-f.queuedRequests:case <-ctx.Done():t.Fatal("no queued media polling")}
	f.manager.mu.Lock()
	first:=f.manager.connections[f.identity.Node]
	f.manager.mu.Unlock()
	if first==nil{t.Fatal("lost initial Node socket")}
	f.manager.closeConnection(f.identity.Node)
	waitFor(t,ctx,"replacement Node socket",func()bool{
		f.manager.mu.Lock()
		newSocket:=f.manager.connections[f.identity.Node]
		f.manager.mu.Unlock()
		return newSocket!=nil && newSocket!=first
	})
	// No server redelivery: Node must automatically replay the durable
	// pre-native command with its original ID after reconnect.
	if runtime.starts.Load()!=0{t.Fatal("native worker started while video queued")}
	f.ready.Store(true)
	got:=waitCommandOutcome(t,ctx,outcomes)
	if got.CommandID!=cmd.Metadata().CommandID || got.State!=CommandAccepted{
		t.Fatalf("replayed dispatch result: %+v",got)
	}
	if runtime.starts.Load()!=1{t.Fatalf("native Worker started %d times",runtime.starts.Load())}
	time.Sleep(250*time.Millisecond)
	if runtime.starts.Load()!=1{t.Fatal("duplicate native Start after reconnect")}
}

type t12HoldRuntime struct {
	starts atomic.Int32
	resumes atomic.Int32
	cancels atomic.Int32
	steers atomic.Int32
	session *t12HoldSession
}
type t12HoldSession struct {
	runtime *t12HoldRuntime
	activity chan Activity
	result chan Result
	once sync.Once
}
func (r *t12HoldRuntime) Start(context.Context,StartRequest)(Session,error) {
	r.starts.Add(1)
	if r.session==nil{r.session=&t12HoldSession{runtime:r,activity:make(chan Activity),result:make(chan Result)}}
	return r.session,nil
}
func (r *t12HoldRuntime) Resume(ctx context.Context,req StartRequest,sessionID string)(Session,error) {
	r.resumes.Add(1)
	return r.Start(ctx,req)
}
func (s *t12HoldSession) ID()string{return "held-native-session"}
func (s *t12HoldSession) Prompt(context.Context,string)error{return nil}
func (s *t12HoldSession) Steer(context.Context,string)(bool,error){s.runtime.steers.Add(1);return true,nil}
func (s *t12HoldSession) Cancel(context.Context)error{s.runtime.cancels.Add(1);return nil}
func (s *t12HoldSession) Activity()<-chan Activity{return s.activity}
func (s *t12HoldSession) Result()<-chan Result{return s.result}
func (s *t12HoldSession) Close()error{s.once.Do(func(){close(s.activity);close(s.result)});return nil}

func TestT12CancelSteeringWhileConversionStillQueued(t *testing.T) {
	runtime:=&t12HoldRuntime{}
	f:=t12SocketWithQueuedVideo(t,runtime)
	ctx,cancel:=context.WithTimeout(context.Background(),9*time.Second)
	defer cancel()
	outcomes:=make(chan CommandOutcome,8)
	f.manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,o CommandOutcome)error{outcomes<-o;return nil})
	dispatch:=f.dispatchCommand("t12-hold-native")
	dispatch.Dispatch.Envelope.OriginalUserIntent="work in native session"
	if e:=f.manager.SendCommand(ctx,f.identity.Node,dispatch);e!=nil{t.Fatal(e)}
	if result:=waitCommandOutcome(t,ctx,outcomes);result.State!=CommandAccepted{t.Fatalf("native dispatch failed: %+v",result)}
	meta:=dispatch.Metadata()
	steerMeta:=meta
	steerMeta.CommandID="t12-media-steering"
	steerMeta.IssuedAt=time.Now().UTC()
	steer:=Command{Kind:CommandSteering,Steering:&SteeringCommand{Metadata:steerMeta,Text:"Look at artifact://"+f.originalID}}
	if e:=f.manager.SendCommand(ctx,f.identity.Node,steer);e!=nil{t.Fatal(e)}
	select{case <-f.queuedRequests:case <-ctx.Done():t.Fatal("steering never reached queued conversion")}
	cancelMeta:=meta
	cancelMeta.CommandID="t12-cancel-steering"
	cancelMeta.IssuedAt=time.Now().UTC()
	cancelCmd:=Command{Kind:CommandCancel,Cancel:&CancelCommand{Metadata:cancelMeta,Reason:"owner"}}
	start:=time.Now()
	if e:=f.manager.SendCommand(ctx,f.identity.Node,cancelCmd);e!=nil{t.Fatal(e)}
	got:=map[string]CommandOutcome{}
	for len(got)<2 {
		o:=waitCommandOutcome(t,ctx,outcomes)
		got[o.CommandID]=o
	}
	if time.Since(start)>3*time.Second{t.Fatal("cancel was blocked by queued video steering")}
	if got[cancelMeta.CommandID].State!=CommandAccepted || runtime.cancels.Load()!=1{
		t.Fatalf("native cancel not accepted: %+v cancel calls %d",got,runtime.cancels.Load())
	}
	steerResult:=got[steerMeta.CommandID]
	if steerResult.State!=CommandFailed || steerResult.ErrorCode!="steering_not_delivered" || steerResult.AcceptanceUnknown{
		t.Fatalf("queued steering acceptance incorrectly reported: %+v",steerResult)
	}
	if runtime.steers.Load()!=0||f.ready.Load(){t.Fatal("queued media steering reached native runtime")}
}

func TestT12CancelNewResumeAttemptWithoutMapping(t *testing.T) {
	runtime:=&t12HoldRuntime{}
	f:=t12SocketWithQueuedVideo(t,runtime)
	ctx,cancel:=context.WithTimeout(context.Background(),9*time.Second)
	defer cancel()
	outcomes:=make(chan CommandOutcome,8)
	f.manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,o CommandOutcome)error{outcomes<-o;return nil})
	dispatch:=f.dispatchCommand("t12-previous-attempt")
	dispatch.Dispatch.Envelope.OriginalUserIntent="start session"
	if e:=f.manager.SendCommand(ctx,f.identity.Node,dispatch);e!=nil{t.Fatal(e)}
	if outcome:=waitCommandOutcome(t,ctx,outcomes);outcome.State!=CommandAccepted{t.Fatalf("initial session unavailable: %+v",outcome)}
	newMeta:=dispatch.Metadata()
	newMeta.CommandID="t12-resume-queued"
	newMeta.AttemptID="attempt-resume-2"
	newMeta.TurnID="turn-resume-2"
	newMeta.IssuedAt=time.Now().UTC()
	envelope:=dispatch.Dispatch.Envelope
	envelope.PreviousAttemptID=dispatch.Metadata().AttemptID
	envelope.AttemptID=newMeta.AttemptID
	envelope.TurnID=newMeta.TurnID
	envelope.OriginalUserIntent="Inspect artifact://"+f.originalID
	resume:=Command{Kind:CommandResume,Resume:&ResumeCommand{Metadata:newMeta,Envelope:envelope}}
	if e:=f.manager.SendCommand(ctx,f.identity.Node,resume);e!=nil{t.Fatal(e)}
	select{case <-f.queuedRequests:case <-ctx.Done():t.Fatal("continuation failed before media wait")}
	cancelMeta:=newMeta
	cancelMeta.CommandID="t12-cancel-resume"
	cancelMeta.IssuedAt=time.Now().UTC()
	canceled:=Command{Kind:CommandCancel,Cancel:&CancelCommand{Metadata:cancelMeta,Reason:"owner"}}
	if e:=f.manager.SendCommand(ctx,f.identity.Node,canceled);e!=nil{t.Fatal(e)}
	got:=map[string]CommandOutcome{}
	for len(got)<2{
		o:=waitCommandOutcome(t,ctx,outcomes)
		got[o.CommandID]=o
	}
	if got[newMeta.CommandID].ErrorCode!="canceled_before_handoff" || got[cancelMeta.CommandID].State!=CommandAccepted{
		t.Fatalf("resume prestart cancellation lost: %+v",got)
	}
	if _,mapped:=f.local.SessionMapping(newMeta.AttemptID);mapped{t.Fatal("prestart-canceled new Attempt acquired session mapping")}
	if runtime.resumes.Load()!=0{t.Fatal("late resume invoked native provider")}
	f.ready.Store(true)
	time.Sleep(350*time.Millisecond)
	if runtime.resumes.Load()!=0{t.Fatal("late conversion resumed canceled Attempt")}
	if !f.local.PrestartCanceled(newMeta.AttemptID){t.Fatal("durable cancellation record not retained")}
}


// A native runtime may continue doing work long after the Dispatch receipt.
// Its process CommandContext must be bound to Daemon.Run, not to the transient
// socket used to receive the command.
type t12SurvivingRuntime struct {
	starts atomic.Int32
	cancels atomic.Int32
	mu sync.Mutex
	startCtx context.Context
	session *t12SurvivingSession
}
type t12SurvivingSession struct {
	runtime *t12SurvivingRuntime
	activity chan Activity
	results chan Result
}
func (r *t12SurvivingRuntime) Start(ctx context.Context, _ StartRequest) (Session,error) {
	r.starts.Add(1)
	s:=&t12SurvivingSession{runtime:r,activity:make(chan Activity),results:make(chan Result,1)}
	r.mu.Lock()
	r.startCtx=ctx
	r.session=s
	r.mu.Unlock()
	return s,nil
}
func (r *t12SurvivingRuntime) snapshot()(context.Context,*t12SurvivingSession){
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startCtx,r.session
}
func (s *t12SurvivingSession) ID()string{return "long-running-native-session"}
func (s *t12SurvivingSession) Prompt(context.Context,string)error{return nil}
func (s *t12SurvivingSession) Steer(context.Context,string)(bool,error){return true,nil}
func (s *t12SurvivingSession) Cancel(context.Context)error{
	s.runtime.cancels.Add(1)
	s.results<-Result{Status:"canceled",Summary:"canceled after reconnect"}
	return nil
}
func (s *t12SurvivingSession) Activity()<-chan Activity{return s.activity}
func (s *t12SurvivingSession) Result()<-chan Result{return s.results}
func (s *t12SurvivingSession) Close()error{return nil}

func TestT12AcceptedNativeWorkerSurvivesWebSocketReconnectAndTerminalResult(t *testing.T) {
	for _,scenario:=range []string{"native_result","cancel_after_reconnect"} {
		t.Run(scenario,func(t *testing.T){
			runtime:=&t12SurvivingRuntime{}
			f:=t12SocketWithQueuedVideo(t,runtime)
			ctx,cancel:=context.WithTimeout(context.Background(),9*time.Second)
			defer cancel()
			f.ready.Store(true)
			receipts:=make(chan CommandOutcome,5)
			terminals:=make(chan core.AttemptOutcomeEnvelope,5)
			var terminalDeliveries atomic.Int32
			f.manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,o CommandOutcome)error {
				receipts<-o
				return nil
			})
			f.manager.SetEventSink(func(_ context.Context,event NodeEvent)error {
				if event.Outcome!=nil && event.Outcome.AttemptID=="attempt-1" {
					terminalDeliveries.Add(1)
					terminals<-*event.Outcome
				}
				return nil
			})
			dispatch:=f.dispatchCommand("t12-dispatch-survives-ws-"+scenario)
			dispatch.Dispatch.Envelope.OriginalUserIntent="Inspect artifact://"+f.originalID
			if err:=f.manager.SendCommand(ctx,f.identity.Node,dispatch);err!=nil{t.Fatal(err)}
			accepted:=waitCommandOutcome(t,ctx,receipts)
			if accepted.CommandID!=dispatch.Metadata().CommandID || accepted.State!=CommandAccepted {
				t.Fatalf("native Start was not accepted: %+v",accepted)
			}
			nativeCtx,nativeSession:=runtime.snapshot()
			if nativeCtx==nil || nativeSession==nil || runtime.starts.Load()!=1 {
				t.Fatalf("missing native process after handoff: ctx=%v starts=%d",nativeCtx,runtime.starts.Load())
			}
			if err:=nativeCtx.Err();err!=nil{t.Fatalf("native process canceled before disconnect: %v",err)}
			f.manager.mu.Lock()
			oldSocket:=f.manager.connections[f.identity.Node]
			f.manager.mu.Unlock()
			if oldSocket==nil{t.Fatal("original socket missing")}
			f.manager.closeConnection(f.identity.Node)
			waitFor(t,ctx,"new authenticated socket",func()bool{
				f.manager.mu.Lock()
				sock:=f.manager.connections[f.identity.Node]
				f.manager.mu.Unlock()
				return sock!=nil && sock!=oldSocket
			})
			if err:=nativeCtx.Err();err!=nil {
				t.Fatalf("transport disconnect killed accepted native CommandContext: %v",err)
			}
			if runtime.starts.Load()!=1{t.Fatalf("reconnect launched duplicate native worker: %d",runtime.starts.Load())}
			// The same durable CommandID must not restart a native process.
			if err:=f.manager.SendCommand(ctx,f.identity.Node,dispatch);err!=nil{t.Fatal(err)}
			duplicate:=waitCommandOutcome(t,ctx,receipts)
			if duplicate.CommandID!=accepted.CommandID || duplicate.State!=CommandAccepted {
				t.Fatalf("duplicate native dispatch changed outcome: %+v",duplicate)
			}
			if runtime.starts.Load()!=1{t.Fatalf("duplicate native Start: %d",runtime.starts.Load())}

			if scenario=="native_result"{
				nativeSession.results<-Result{Status:"succeeded",Summary:"video frames processed after reconnect"}
			}else{
				meta:=dispatch.Metadata()
				meta.CommandID="t12-owner-cancel-after-reconnect"
				meta.IssuedAt=time.Now().UTC()
				cancelCommand:=Command{Kind:CommandCancel,Cancel:&CancelCommand{Metadata:meta,Reason:"owner cancellation after reconnect"}}
				if err:=f.manager.SendCommand(ctx,f.identity.Node,cancelCommand);err!=nil{t.Fatal(err)}
				cancelReceipt:=waitCommandOutcome(t,ctx,receipts)
				if cancelReceipt.CommandID!=meta.CommandID || cancelReceipt.State!=CommandAccepted {
					t.Fatalf("native Cancel after reconnect not accepted: %+v",cancelReceipt)
				}
				if runtime.cancels.Load()!=1{t.Fatalf("Cancel delivered %d times",runtime.cancels.Load())}
			}
			select {
			case result:=<-terminals:
				expected:=core.OutcomeSucceeded
				if scenario=="cancel_after_reconnect"{expected=core.OutcomeCanceled}
				if result.Status!=expected || result.AttemptID!="attempt-1" {
					t.Fatalf("wrong terminal outcome after reconnect: %+v",result)
				}
			case <-ctx.Done():t.Fatal("accepted native session lost terminal Result")
			}
			waitFor(t,ctx,"durable terminal receipt",func()bool{
				record,err:=f.local.Command(dispatch.Metadata().CommandID)
				return err==nil && record.TerminalOutcomeQueued
			})
			waitFor(t,ctx,"terminal outbox acknowledged",func()bool{
				remaining,err:=f.local.PendingEvents()
				return err==nil && len(remaining)==0
			})
			if terminalDeliveries.Load()!=1 || runtime.starts.Load()!=1 {
				t.Fatalf("terminal duplicated or Worker relaunched: terminal=%d starts=%d",terminalDeliveries.Load(),runtime.starts.Load())
			}
		})
	}
}
