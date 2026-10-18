package node

import (
	"context"
	"testing"
	"time"
)

// A native harness is allowed to retain its Resume context for the accepted
// Session's lifetime. Finishing pre-native cancellation bookkeeping must not
// stop the successfully accepted native Worker.
type t12ResumeContextCaptureRuntime struct {
	t12AttemptBoundRuntime
	contexts chan context.Context
}
func (r *t12ResumeContextCaptureRuntime) Resume(ctx context.Context, request StartRequest, previousID string) (Session,error) {
	r.contexts <- ctx
	return r.t12AttemptBoundRuntime.Resume(ctx,request,previousID)
}

func TestT12DirectResumeContextSurvivesSuccessfulHandoffUntilSessionEnds(t *testing.T){
	for _,ending:=range []string{"remove","close"}{
		t.Run(ending,func(t *testing.T){
			rootCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
			defer cancel()
			runtime:=&t12ResumeContextCaptureRuntime{contexts:make(chan context.Context,1)}
			local:=NewLocal(runtime)
			request:=StartRequest{WorkerRef:"worker-"+ending,AttemptID:"attempt-"+ending,Workspace:t.TempDir()}
			if _,err:=local.Resume(rootCtx,request,"previous-native");err!=nil{t.Fatal(err)}
			var nativeCtx context.Context
			select{case nativeCtx=<-runtime.contexts:default:t.Fatal("runtime did not see native Context")}
			select{
			case <-nativeCtx.Done():t.Fatal("successful native Resume was canceled by prestart cleanup")
			default:
			}
			// A stale Cancel cannot revoke the current native context.
			ok,err:=local.CancelLocalAttempt(rootCtx,request.WorkerRef,"previous-attempt")
			if ok||err!=nil{t.Fatalf("stale Cancel unexpectedly accepted: handled=%t err=%v",ok,err)}
			select{case <-nativeCtx.Done():t.Fatal("stale Attempt canceled active native Context");default:}
			if ending=="remove"{
				if err:=local.Remove(request.WorkerRef);err!=nil{t.Fatal(err)}
			}else{
				if err:=local.Close();err!=nil{t.Fatal(err)}
			}
			select{
			case <-nativeCtx.Done():
			case <-rootCtx.Done():t.Fatal("native Context survived Session removal or Node shutdown")
			}
		})
	}
}
