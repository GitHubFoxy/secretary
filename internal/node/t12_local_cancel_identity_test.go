package node

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type t12AttemptBoundRuntime struct {
	starts atomic.Int32
	resumes atomic.Int32
	cancels atomic.Int32
}
type t12AttemptBoundSession struct {
	runtime *t12AttemptBoundRuntime
	activities chan Activity
	results chan Result
}
func (r *t12AttemptBoundRuntime) Start(context.Context, StartRequest) (Session, error) {
	r.starts.Add(1)
	return &t12AttemptBoundSession{runtime:r,activities:make(chan Activity),results:make(chan Result)},nil
}
func (r *t12AttemptBoundRuntime) Resume(ctx context.Context,req StartRequest,nativeID string)(Session,error){
	r.resumes.Add(1)
	return &t12AttemptBoundSession{runtime:r,activities:make(chan Activity),results:make(chan Result)},nil
}
func (s *t12AttemptBoundSession) ID() string{return "same-native-identity-across-attempts"}
func (s *t12AttemptBoundSession) Prompt(context.Context,string) error{return nil}
func (s *t12AttemptBoundSession) Steer(context.Context,string)(bool,error){return true,nil}
func (s *t12AttemptBoundSession) Cancel(context.Context)error{s.runtime.cancels.Add(1);return nil}
func (s *t12AttemptBoundSession) Activity()<-chan Activity{return s.activities}
func (s *t12AttemptBoundSession) Result()<-chan Result{return s.results}
func (s *t12AttemptBoundSession) Close()error{return nil}

// Previous Attempt checkpoints intentionally survive a continuation, and
// the native session ID itself may stay identical on Resume. Neither can be
// used to authorize a Cancel of the newly active Attempt.
func TestT12LocalCancelChecksCurrentlyActiveAttemptAfterContinue(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	runtime:=&t12AttemptBoundRuntime{}
	local:=NewLocal(runtime)
	store,e:=OpenLocalStore(filepath.Join(t.TempDir(),"local-state.json"))
	if e!=nil{t.Fatal(e)}
	defer store.Close()
	local.SetPrestartStore(store)

	first:=StartRequest{WorkerRef:"worker-identity",AttemptID:"attempt-old",Workspace:t.TempDir()}
	oldSession,e:=local.Dispatch(ctx,first)
	if e!=nil{t.Fatalf("first local Dispatch: %v",e)}
	next:=first
	next.AttemptID="attempt-active"
	next.Task="new turn"
	currentSession,e:=local.Continue(ctx,next,first.AttemptID)
	if e!=nil{t.Fatalf("local Continue: %v",e)}
	if currentSession==oldSession||currentSession.ID()!=oldSession.ID(){
		t.Fatal("regression fixture must use different session objects with the SAME native session ID")
	}
	if runtime.resumes.Load()!=1{t.Fatalf("native Resume calls %d",runtime.resumes.Load())}
	if _,ok:=local.checkpoints[first.AttemptID];!ok{t.Fatal("historical checkpoint missing from fixture")}

	for _,id:=range []string{"attempt-old","attempt-unknown",""}{
		handled,err:=local.CancelLocalAttempt(ctx,next.WorkerRef,id)
		if err!=nil||handled{t.Fatalf("stale/unknown Attempt %q authorized Cancel: handled=%v err=%v",id,handled,err)}
		if runtime.cancels.Load()!=0{t.Fatalf("stale/unknown Attempt %q canceled the active session",id)}
	}
	handled,err:=local.CancelLocalAttempt(ctx,next.WorkerRef,next.AttemptID)
	if err!=nil||!handled||runtime.cancels.Load()!=1{
		t.Fatalf("active Attempt Cancel not delivered: handled=%v err=%v cancels=%d",handled,err,runtime.cancels.Load())
	}
	// Once the native session is removed even the former active Attempt
	// cannot cancel another session through historical checkpoints.
	if err:=local.Remove(next.WorkerRef);err!=nil{t.Fatal(err)}
	handled,err=local.CancelLocalAttempt(ctx,next.WorkerRef,next.AttemptID)
	if err!=nil||handled||runtime.cancels.Load()!=1{
		t.Fatalf("removed session was canceled via stale Attempt: handled=%v err=%v cancels=%d",handled,err,runtime.cancels.Load())
	}
}

func TestT12LocalCancelRequiresAttemptBoundOnResume(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	runtime:=&t12AttemptBoundRuntime{}
	local:=NewLocal(runtime)
	request:=StartRequest{WorkerRef:"worker-resumed",AttemptID:"attempt-resumed",Workspace:t.TempDir()}
	_,err:=local.Resume(ctx,request,"same-native-identity-across-attempts")
	if err!=nil{t.Fatal(err)}
	for _,id:=range []string{"old-attempt","unknown-attempt",""}{
		handled,e:=local.CancelLocalAttempt(ctx,request.WorkerRef,id)
		if e!=nil||handled||runtime.cancels.Load()!=0{
			t.Fatalf("foreign Attempt %q canceled resumed session: handled=%v err=%v count=%d",id,handled,e,runtime.cancels.Load())
		}
	}
	handled,e:=local.CancelLocalAttempt(ctx,request.WorkerRef,request.AttemptID)
	if e!=nil||!handled||runtime.cancels.Load()!=1{
		t.Fatalf("current resumed Attempt denied: handled=%v err=%v count=%d",handled,e,runtime.cancels.Load())
	}
}
