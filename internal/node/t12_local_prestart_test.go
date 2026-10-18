package node

import (
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "errors"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
)

func TestT12LocalContinueQueuedMediaDoesNotBlockIndependentWorker(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),12*time.Second)
 defer cancel()
 runtime:=&t12HoldRuntime{}
 local:=NewLocal(runtime)
	store,err:=OpenLocalStore(filepath.Join(t.TempDir(),"local-state.json"))
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	local.SetPrestartStore(store)
	id:="art_"+strings.Repeat("8",32)
	original:=[]byte("queued video")
	frameID:="art_"+strings.Repeat("9",32)
	frame,_:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	sum:=func(b []byte)string{h:=sha256.Sum256(b);return hex.EncodeToString(h[:])}
	entered:=make(chan struct{},1)
	ready:=make(chan struct{})
	local.SetArtifactFetcher(func(_ context.Context,requested string)(ArtifactPayload,error){
		switch requested{
		case id:return ArtifactPayload{ID:id,Name:"queued.mp4",Data:original,SHA256:sum(original),VideoStatus:"queued",
			CheckVideoStatus:func(c context.Context)(core.Artifact,error){
				select{case entered<-struct{}{}:default:}
				select{case <-ready:return core.Artifact{ID:id,ProcessingStatus:"ready",VideoFrames:[]core.VideoFrame{
					{ArtifactID:frameID,TimestampMS:0,TimestampKind:"source-pts-relative"},
				}},nil;case <-c.Done():return core.Artifact{},c.Err()}
			}},nil
		case frameID:return ArtifactPayload{ID:frameID,Name:"frame.webp",Data:frame,SHA256:sum(frame)},nil
		}
		return ArtifactPayload{},errors.New("unexpected artifact")
	})
	profile:=workerTemplateFixture(daemonInventoryFixture("macbook").Instances[0],"","")
	instance:=daemonInventoryFixture("macbook").Instances[0]
	instance.Capabilities.Execution=append(instance.Capabilities.Execution,core.CapabilityImageInput)
	a:=StartRequest{WorkerRef:"worker-A",AttemptID:"attempt-A1",Workspace:t.TempDir(),Profile:profile,HarnessInstance:instance}
	_,err=local.Dispatch(ctx,a)
	if err!=nil{t.Fatalf("initial local A: %v",err)}
	next:=a;next.AttemptID="attempt-A2";next.Task="Inspect artifact://"+id
	result:=make(chan error,1)
	go func(){_,e:=local.Continue(ctx,next,a.AttemptID);result<-e}()
	select{case <-entered:case <-ctx.Done():t.Fatal("continue never waited for queued media")}
	fastCtx,stopFast:=context.WithTimeout(ctx,time.Second)
	defer stopFast()
	b:=StartRequest{WorkerRef:"worker-B",AttemptID:"attempt-B1",Workspace:t.TempDir(),Profile:profile,HarnessInstance:instance}
	done:=make(chan error,1)
	go func(){_,e:=local.Dispatch(fastCtx,b);done<-e}()
	select{case e:=<-done:if e!=nil{t.Fatalf("unrelated B Dispatch blocked/failed: %v",e)};case <-fastCtx.Done():t.Fatal("global mutex blocked unrelated B Dispatch")}
	sessionDone:=make(chan bool,1)
	go func(){_,ok:=local.Session("worker-B");sessionDone<-ok}()
	select{case ok:=<-sessionDone:if !ok{t.Fatal("unrelated Worker B Session unavailable")};case <-fastCtx.Done():t.Fatal("global mutex blocked Session lookup")}
	if runtime.resumes.Load()!=0{t.Fatal("A resumed native before media ready")}
	close(ready)
	select{case e:=<-result:if e!=nil{t.Fatalf("local continuation failed after ready: %v",e)};case <-ctx.Done():t.Fatal("A never resumed")}
	if runtime.resumes.Load()!=1{t.Fatalf("A native resumption count %d",runtime.resumes.Load())}
}

func TestT12LocalDispatchCanceledBeforeReadyNeverStartsAndStateDurable(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
	defer cancel()
	runtime:=&t12HoldRuntime{}
	local:=NewLocal(runtime)
	file:=filepath.Join(t.TempDir(),"local-state.json")
	store,err:=OpenLocalStore(file)
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	local.SetPrestartStore(store)
	id:="art_"+strings.Repeat("a",32)
	video:=[]byte("queued video")
	hash:=sha256.Sum256(video)
	entered:=make(chan struct{},1)
	ready:=make(chan struct{})
	local.SetArtifactFetcher(func(_ context.Context,key string)(ArtifactPayload,error){
		if key!=id{return ArtifactPayload{},errors.New("unexpected ID")}
		return ArtifactPayload{ID:id,Name:"clip.mp4",Data:video,SHA256:hex.EncodeToString(hash[:]),VideoStatus:"queued",
			CheckVideoStatus:func(ctx context.Context)(core.Artifact,error){
				select{case entered<-struct{}{}:default:}
				select{
				case <-ready:return core.Artifact{ID:id,ProcessingStatus:"ready"},nil
				case <-ctx.Done():return core.Artifact{},ctx.Err()
				}
			}},nil
	})
	req:=StartRequest{WorkerRef:"worker-A",AttemptID:"attempt-pre-native",Task:"Analyze artifact://"+id,Workspace:t.TempDir()}
	outcome:=make(chan error,1)
	go func(){_,e:=local.Dispatch(ctx,req);outcome<-e}()
	select{case <-entered:case <-ctx.Done():t.Fatal("dispatch did not await queued video")}
	start:=time.Now()
	accepted,err:=local.CancelLocalAttempt(ctx,req.WorkerRef,req.AttemptID)
	if err!=nil||!accepted{t.Fatalf("prestart Cancel failed: accepted=%t err=%v",accepted,err)}
	if time.Since(start)>time.Second{t.Fatal("Cancel waited behind video processing")}
	select{case e:=<-outcome:if !errors.Is(e,context.Canceled) && !errors.Is(e,ErrLocalPrestartCanceled){
		t.Fatalf("canceled local dispatch returned %v",e)
	};case <-ctx.Done():t.Fatal("local dispatch not interrupted")}
	close(ready)
	if runtime.starts.Load()!=0 {t.Fatalf("late native Start count %d",runtime.starts.Load())}
	reopened,e:=OpenLocalStore(file)
	if e!=nil{t.Fatal(e)}
	defer reopened.Close()
	if stage:=reopened.LocalPrestartState(req.AttemptID);stage!="canceled"{
		t.Fatalf("pre-native cancellation not durable after reopen: %q",stage)
	}
	if _,err:=reopened.BeginLocalPrestart(req.AttemptID,req.WorkerRef);!errors.Is(err,ErrLocalPrestartCanceled) {
		t.Fatalf("late re-dispatch bypassed canceled checkpoint: %v",err)
	}
}
