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
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT12CancelPreemptsQueuedVideoDispatchBeforeNativeStart(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"))
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	manager,err:=NewServerManager(ctx,store,"pair-token","admin-token")
	if err!=nil{t.Fatal(err)}
	id:="art_"+strings.Repeat("a",32)
	frameID:="art_"+strings.Repeat("b",32)
	original:=[]byte("queued clip")
	sha:=sha256.Sum256(original)
	entered:=make(chan struct{},1)
	var ready atomic.Bool
	mux:=http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect",manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes",manager)
	mux.Handle("/v1/nodes/",manager)
	mux.HandleFunc("/v1/artifacts/",func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("X-Artifact-Signature")=="" || r.Header.Get("X-Secretary-Node")!="macbook"{
			t.Errorf("Node authentication headers missing");http.Error(w,"not authorized",401);return
		}
		if strings.HasSuffix(r.URL.Path,"/status"){
			select{case entered<-struct{}{}:default:}
			state:="queued"
			if ready.Load(){state="ready"}
			_ = json.NewEncoder(w).Encode(core.Artifact{ID:id,ProcessingStatus:state,
				VideoFrames:[]core.VideoFrame{{ArtifactID:frameID,TimestampMS:0,TimestampKind:"source-pts-relative"}}})
			return
		}
		if !strings.HasSuffix(r.URL.Path,id){http.Error(w,"not found",404);return}
		w.Header().Set("X-Artifact-SHA256",hex.EncodeToString(sha[:]))
		w.Header().Set("X-Artifact-Name",base64.RawURLEncoding.EncodeToString([]byte("queued.mp4")))
		w.Header().Set("X-Artifact-Video-Status","queued")
		_,_=w.Write(original)
	})
	server:=httptest.NewServer(mux)
	defer server.Close()
	identity,err:=EnrollNode(ctx,server.Client(),server.URL,"pair-token","macbook")
	if err!=nil{t.Fatal(err)}
	local,err:=OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err!=nil{t.Fatal(err)}
	defer local.Close()
	runtime:=&daemonCountingRuntime{}
	inventory:=daemonInventoryFixture(identity.Node)
	daemon:=&Daemon{Identity:identity,Store:local,Runtime:runtime,Inventory:daemonStaticInventory{snapshot:inventory},
		Capacity:2,HeartbeatInterval:20*time.Millisecond,OutboxPollInterval:10*time.Millisecond,ReconnectMin:10*time.Millisecond,ReconnectMax:40*time.Millisecond}
	daemonCtx,stop:=context.WithCancel(ctx)
	done:=make(chan error,1)
	go func(){done<-daemon.Run(daemonCtx)}()
	defer func(){stop();select{case err:=<-done:if err!=nil{t.Errorf("Node exit: %v",err)};case <-time.After(time.Second):t.Error("Node did not exit")}}()
	waitFor(t,ctx,"Node heartbeat",func()bool{
		st,e:=manager.Status(ctx,identity.Node)
		return e==nil && st.Online && !st.LastHeartbeatAt.IsZero()
	})
	outcomes:=make(chan CommandOutcome,4)
	manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,outcome CommandOutcome)error{outcomes<-outcome;return nil})
	dispatch:=daemonDispatchFixture(identity.Node,"dispatch-queued-video")
	dispatch.Dispatch.Envelope.OriginalUserIntent="Describe artifact://"+id
	if err:=manager.SendCommand(ctx,identity.Node,dispatch);err!=nil{t.Fatal(err)}
	select{case <-entered:case <-ctx.Done():t.Fatal("dispatch never started awaiting queued conversion")}
	if runtime.starts.Load()!=0{t.Fatal("native session started before video conversion")}
	meta:=dispatch.Metadata()
	meta.CommandID="cancel-queued-video"
	meta.IssuedAt=time.Now().UTC()
	cancelCommand:=Command{Kind:CommandCancel,Cancel:&CancelCommand{Metadata:meta,Reason:"user requested cancellation"}}
	start:=time.Now()
	if err:=manager.SendCommand(ctx,identity.Node,cancelCommand);err!=nil{t.Fatal(err)}
	got:=map[string]CommandOutcome{}
	for len(got)<2 {
		outcome:=waitCommandOutcome(t,ctx,outcomes)
		got[outcome.CommandID]=outcome
	}
	if time.Since(start)>3*time.Second{t.Fatalf("cancel was blocked behind queued conversion: %s",time.Since(start))}
	if got[meta.CommandID].State!=CommandAccepted{
		t.Fatalf("pre-native Cancel did not complete: %+v",got[meta.CommandID])
	}
	originalOutcome:=got[dispatch.Metadata().CommandID]
	if originalOutcome.ErrorCode!="canceled_before_handoff" {
		t.Fatalf("dispatch was not interrupted by cancellation: %+v",originalOutcome)
	}
	ready.Store(true)
	time.Sleep(400*time.Millisecond)
	if runtime.starts.Load()!=0{t.Fatal("late conversion launched canceled native session")}
	if _,ok:=local.SessionMapping(meta.AttemptID);ok{t.Fatal("canceled dispatch retained a native session mapping")}
}
