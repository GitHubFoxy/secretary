package node

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// This provider deliberately ignores context cancellation until explicitly
// released, as a native Resume can complete even after Cancel was accepted.
// LocalNode must close that late Session instead of publishing it.
type t12LateResumeRuntime struct {
	entered chan struct{}
	release chan struct{}
	resumes atomic.Int32
	starts atomic.Int32
	closes atomic.Int32
	cancels atomic.Int32
}
type t12LateResumeSession struct {
	runtime *t12LateResumeRuntime
	activities chan Activity
	results chan Result
}
func (r *t12LateResumeRuntime) session() Session {
	return &t12LateResumeSession{runtime:r,activities:make(chan Activity),results:make(chan Result)}
}
func (r *t12LateResumeRuntime) Start(context.Context,StartRequest)(Session,error){
	r.starts.Add(1)
	return r.session(),nil
}
func (r *t12LateResumeRuntime) Resume(_ context.Context,_ StartRequest,_ string)(Session,error){
	r.resumes.Add(1)
	select{case r.entered<-struct{}{}:default:}
	<-r.release // intentionally ignores ctx.Done()
	return r.session(),nil
}
func (s *t12LateResumeSession) ID()string{return "native-late-resume"}
func (s *t12LateResumeSession) Prompt(context.Context,string)error{return nil}
func (s *t12LateResumeSession) Steer(context.Context,string)(bool,error){return true,nil}
func (s *t12LateResumeSession) Cancel(context.Context)error{s.runtime.cancels.Add(1);return nil}
func (s *t12LateResumeSession) Activity()<-chan Activity{return s.activities}
func (s *t12LateResumeSession) Result()<-chan Result{return s.results}
func (s *t12LateResumeSession) Close()error{s.runtime.closes.Add(1);return nil}

func TestT12DirectLocalResumeCancelInterruptsInFlightNativeWithoutLateSession(t *testing.T){
	ctx,stop:=context.WithTimeout(context.Background(),8*time.Second)
	defer stop()
	runtime:=&t12LateResumeRuntime{entered:make(chan struct{},1),release:make(chan struct{})}
	defer func(){select{case <-runtime.release:default:close(runtime.release)}}()
	local:=NewLocal(runtime)
	path:=filepath.Join(t.TempDir(),"local-prestarts.json")
	store,err:=OpenLocalStore(path)
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	local.SetPrestartStore(store)

	request:=StartRequest{WorkerRef:"worker-resume",AttemptID:"attempt-blocked-resume",Workspace:t.TempDir()}
	type response struct{session Session;err error}
	finished:=make(chan response,1)
	go func(){s,e:=local.Resume(ctx,request,"prior-native");finished<-response{s,e}}()
	select{case <-runtime.entered:case <-ctx.Done():t.Fatal("direct Resume never entered provider")}

	if stage:=store.LocalPrestartState(request.AttemptID);stage!="resuming"{
		t.Fatalf("in-flight native Resume was not durably marked: %q",stage)
	}
	// An ambiguous, possibly already-started native Resume must NOT replay
	// blindly after a Node process restart.
	reopened,e:=OpenLocalStore(path)
	if e!=nil{t.Fatal(e)}
	defer reopened.Close()
	if e:=reopened.BeginLocalPrestart(request.AttemptID,request.WorkerRef);e==nil{
		t.Fatal("in-flight native Resume could be duplicated after restart")
	}

	cancelCtx,doneCancel:=context.WithTimeout(ctx,600*time.Millisecond)
	defer doneCancel()
	started:=time.Now()
	handled,err:=local.CancelLocalAttempt(cancelCtx,request.WorkerRef,request.AttemptID)
	if err!=nil||!handled{t.Fatalf("Cancel blocked behind native Resume: handled=%t err=%v",handled,err)}
	if time.Since(started)>500*time.Millisecond{t.Fatal("Cancel did not return promptly")}
	if stage:=store.LocalPrestartState(request.AttemptID);stage!="canceled"{
		t.Fatalf("native Resume Cancel was not durably recorded: %q",stage)
	}
	select{case got:=<-finished:t.Fatalf("provider returned before explicit release: %+v",got);default:}

	// Provider ignores cancellation and returns a Session anyway. The
	// completed native side effect cannot become an active local Worker.
	close(runtime.release)
	select{
	case got:=<-finished:
		if got.session!=nil || (!errors.Is(got.err,context.Canceled)&&!errors.Is(got.err,ErrLocalPrestartCanceled)){
			t.Fatalf("canceled native Resume was registered: %+v",got)
		}
	case <-ctx.Done():t.Fatal("canceled Resume never returned after provider release")
	}
	if _,ok:=local.Session(request.WorkerRef);ok{t.Fatal("late native Resume registered a Session after Cancel")}
	if runtime.closes.Load()!=1||runtime.cancels.Load()!=0{
		t.Fatalf("late provider Session not closed exactly once: closes=%d native-cancels=%d",runtime.closes.Load(),runtime.cancels.Load())
	}
	if err:=store.BeginLocalPrestart(request.AttemptID,request.WorkerRef);!errors.Is(err,ErrLocalPrestartCanceled){
		t.Fatalf("durable cancel accepted stale Resume replay: %v",err)
	}

	// A subsequent attempt on the SAME Worker may start normally. The
	// canceled/unknown previous Attempt must not cancel this new Session.
	next:=StartRequest{WorkerRef:request.WorkerRef,AttemptID:"attempt-new",Workspace:t.TempDir()}
	if _,err:=local.Dispatch(ctx,next);err!=nil{t.Fatalf("new local Dispatch after canceled Resume: %v",err)}
	handled,err=local.CancelLocalAttempt(ctx,next.WorkerRef,request.AttemptID)
	if err!=nil||!handled||runtime.cancels.Load()!=0{
		t.Fatalf("stale canceled Attempt affected active Session: handled=%t err=%v cancels=%d",handled,err,runtime.cancels.Load())
	}
	handled,err=local.CancelLocalAttempt(ctx,next.WorkerRef,"attempt-unknown")
	if err!=nil||handled||runtime.cancels.Load()!=0{
		t.Fatalf("unknown Attempt affected active Session: handled=%t err=%v cancels=%d",handled,err,runtime.cancels.Load())
	}
	handled,err=local.CancelLocalAttempt(ctx,next.WorkerRef,next.AttemptID)
	if err!=nil||!handled||runtime.cancels.Load()!=1{
		t.Fatalf("active Attempt Cancel failed: handled=%t err=%v cancels=%d",handled,err,runtime.cancels.Load())
	}
}
