package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// Interruption before a native Start must leave a durable, replayable claim
// rather than persist artifact_unavailable. RecoverRunning must not turn it
// into an unknown-execution terminal after Node process restart.
func TestT12PreNativeDisconnectAndNodeRestartRecoveryStartsOnce(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),7*time.Second)
	defer cancel()
	file:=filepath.Join(t.TempDir(),"node-state.json")
	store,err:=OpenLocalStore(file)
	if err!=nil{t.Fatal(err)}
	runtime:=&daemonCountingRuntime{}
	origin:="art_"+strings.Repeat("1",32)
	child:="art_"+strings.Repeat("2",32)
	video:=[]byte("queued original")
	webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	digest:=func(b []byte)string{h:=sha256.Sum256(b);return hex.EncodeToString(h[:])}
	entered:=make(chan struct{},1)
	worker:=NewExecutionNode("macbook",runtime,store)
	worker.SetArtifactFetcher(func(_ context.Context,id string)(ArtifactPayload,error){
		if id!=origin{return ArtifactPayload{},errors.New("unexpected frame fetch before ready")}
		return ArtifactPayload{ID:origin,Name:"queued.mp4",Data:video,SHA256:digest(video),VideoStatus:"queued",
			CheckVideoStatus:func(ctx context.Context)(core.Artifact,error){
				select{case entered<-struct{}{}:default:}
				<-ctx.Done()
				return core.Artifact{},ctx.Err()
			}},nil
	})
	cmd:=daemonDispatchFixture("macbook","t12-queued-restart")
	cmd.Dispatch.Envelope.OriginalUserIntent="Inspect artifact://"+origin
	cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution=append(cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution,core.CapabilityImageInput)
	startCtx,interrupt:=context.WithCancel(ctx)
	done:=make(chan error,1)
	go func(){
		_,e:=worker.HandleCommand(startCtx,cmd)
		done<-e
	}()
	select{case <-entered:case <-ctx.Done():t.Fatal("queued native dispatch never started polling")}
	interrupt()
	select{case e:=<-done:if !errors.Is(e,context.Canceled){t.Fatalf("expected deferred prestart claim: %v",e)}
	case <-ctx.Done():t.Fatal("pre-native disconnect did not interrupt") }
	rec,e:=store.Command(cmd.Metadata().CommandID)
	if e!=nil||rec.State!=CommandProcessing||!rec.SafePrestart||!rec.Replayable {
		t.Fatalf("pre-native disconnect persisted a terminal result: %+v %v",rec,e)
	}
	if runtime.starts.Load()!=0{t.Fatal("native runtime started before disconnect")}
	_=store.Close()
	reopened,e:=OpenLocalStore(file)
	if e!=nil{t.Fatal(e)}
	defer reopened.Close()
	if e:=reopened.RecoverRunning(ctx,nil);e!=nil{t.Fatal(e)}
	recovered,e:=reopened.PendingPrestartCommands()
	if e!=nil||len(recovered)!=1||recovered[0].Metadata().CommandID!=cmd.Metadata().CommandID {
		t.Fatalf("restart lost durable pending command: %+v %v",recovered,e)
	}
	restored:=NewExecutionNode("macbook",runtime,reopened)
	var originalDownloads,frameDownloads int
	restored.SetArtifactFetcher(func(_ context.Context,id string)(ArtifactPayload,error){
		if id==origin {
			originalDownloads++
			return ArtifactPayload{ID:origin,Name:"queued.mp4",Data:video,SHA256:digest(video),VideoStatus:"ready",
				VideoFrames:[]core.VideoFrame{{ArtifactID:child,TimestampMS:0,TimestampKind:"source-pts-relative"}}},nil
		}
		if id!=child{return ArtifactPayload{},errors.New("unknown child")}
		frameDownloads++
		return ArtifactPayload{ID:child,Name:"frame.webp",Data:webp,SHA256:digest(webp)},nil
	})
	outcome,e:=restored.HandleCommand(ctx,recovered[0])
	if e!=nil||outcome.State!=CommandAccepted{t.Fatalf("recovered native dispatch failed: %+v %v",outcome,e)}
	if runtime.starts.Load()!=1||originalDownloads!=1||frameDownloads!=1 {
		t.Fatalf("recovery duplicated native handoff: starts=%d original=%d child=%d",runtime.starts.Load(),originalDownloads,frameDownloads)
	}
	rec,e=reopened.Command(cmd.Metadata().CommandID)
	if e!=nil||rec.SafePrestart||rec.State!=CommandAccepted {
		t.Fatalf("native handoff marker not persisted: %+v %v",rec,e)
	}
}

func TestT12ReadyVideoFrameGET503Then200StartsOnce(t *testing.T) {
	source:="art_"+strings.Repeat("3",32)
	frame:="art_"+strings.Repeat("4",32)
	video:=[]byte("ready video original")
	webp,e:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if e!=nil{t.Fatal(e)}
	sum:=func(data []byte)string{h:=sha256.Sum256(data);return hex.EncodeToString(h[:])}
	var frameGets,originalGets atomic.Int32
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("X-Artifact-Signature")=="" {http.Error(w,"unauthorized",403);return}
		id:=strings.TrimPrefix(r.URL.Path,"/v1/artifacts/")
		var data []byte
		var name string
		if id==source{
			originalGets.Add(1)
			data,name=video,"ready.mp4"
			w.Header().Set("X-Artifact-Video-Status","ready")
			refs,_:=json.Marshal([]core.VideoFrame{{ArtifactID:frame,TimestampMS:0,TimestampKind:"source-pts-relative"}})
			w.Header().Set("X-Artifact-Video-Frames",base64.RawURLEncoding.EncodeToString(refs))
		}else if id==frame{
			if frameGets.Add(1)==1 {http.Error(w,"temporarily unavailable",http.StatusServiceUnavailable);return}
			data,name=webp,"frame.webp"
		}else{http.NotFound(w,r);return}
		w.Header().Set("X-Artifact-SHA256",sum(data))
		w.Header().Set("X-Artifact-Name",base64.RawURLEncoding.EncodeToString([]byte(name)))
		_,_=w.Write(data)
	}))
	defer server.Close()
	auth:=base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x67},32))
	identity:=NodeIdentity{Node:"macbook",Credential:auth,ConnectURL:"ws"+strings.TrimPrefix(server.URL,"http")+"/v1/nodes/connect"}
	local,e:=OpenLocalStore(filepath.Join(t.TempDir(),"state.json"))
	if e!=nil{t.Fatal(e)}
	defer local.Close()
	runtime:=&daemonCountingRuntime{}
	execution:=NewExecutionNode("macbook",runtime,local)
	execution.SetArtifactFetcher(identity.FetchArtifact)
	cmd:=daemonDispatchFixture("macbook","t12-503-frame")
	cmd.Dispatch.Envelope.OriginalUserIntent="Describe artifact://"+source
	cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution=append(cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution,core.CapabilityImageInput)
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	result,e:=execution.HandleCommand(ctx,cmd)
	if e!=nil||result.State!=CommandAccepted{t.Fatalf("frame 503 permanently failed native dispatch: %+v %v",result,e)}
	if frameGets.Load()!=2||originalGets.Load()!=1||runtime.starts.Load()!=1{
		t.Fatalf("not exactly one native start: original=%d frame=%d native=%d",originalGets.Load(),frameGets.Load(),runtime.starts.Load())
	}
}

func TestT12TransientOriginalGET503AndPermanent404(t *testing.T) {
	id:="art_"+strings.Repeat("5",32)
	data:=[]byte("original")
	sha:=sha256.Sum256(data)
	var reqCount atomic.Int32
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		n:=reqCount.Add(1)
		if n==1{http.Error(w,"retry later",503);return}
		if n==3{http.Error(w,"not found",404);return}
		w.Header().Set("X-Artifact-SHA256",hex.EncodeToString(sha[:]))
		w.Header().Set("X-Artifact-Name",base64.RawURLEncoding.EncodeToString([]byte("clip.mp4")))
		_,_=w.Write(data)
	}))
	defer server.Close()
	auth:=base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x67},32))
	identity:=NodeIdentity{Node:"macbook",Credential:auth,ConnectURL:"ws"+strings.TrimPrefix(server.URL,"http")+"/v1/nodes/connect"}
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	payload,e:=identity.FetchArtifact(ctx,id)
	if e!=nil || !bytes.Equal(payload.Data,data)||reqCount.Load()!=2 {t.Fatalf("original GET retry: %+v %v requests=%d",payload,e,reqCount.Load())}
	_,e=identity.FetchArtifact(ctx,id)
	if e==nil || errors.Is(e,ErrTransientArtifactStatus) || reqCount.Load()!=3 {
		t.Fatalf("permanent 404 retried instead of failed: %v requests=%d",e,reqCount.Load())
	}
}

var _ = fmt.Sprintf
