package node

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// The original native Resume intentionally ignores canceled contexts until
// explicitly released, reproducing a provider blocked in Resume RPC.
type t12RemoteStalledResumeRuntime struct{
	t12HoldRuntime
	entered chan struct{}
	release chan struct{}
	mu sync.Mutex
	resumeCtx context.Context
	resumeCount atomic.Int32
	lateSession *t12HoldSession
}
func (r *t12RemoteStalledResumeRuntime) Resume(ctx context.Context,_ StartRequest,_ string)(Session,error){
	r.resumeCount.Add(1)
	r.mu.Lock()
	r.resumeCtx=ctx
	r.mu.Unlock()
	select{case r.entered<-struct{}{}:default:}
	<-r.release
	s:=&t12HoldSession{runtime:&r.t12HoldRuntime,activity:make(chan Activity),result:make(chan Result)}
	r.mu.Lock();r.lateSession=s;r.mu.Unlock()
	return s,nil
}
func (r *t12RemoteStalledResumeRuntime) Context()context.Context{
	r.mu.Lock();defer r.mu.Unlock()
	return r.resumeCtx
}

func TestT12RemoteNativeResumeDisconnectReconnectCancelBeforeProviderReturns(t *testing.T){
	runtime:=&t12RemoteStalledResumeRuntime{entered:make(chan struct{},1),release:make(chan struct{})}
	defer func(){select{case <-runtime.release:default:close(runtime.release)}}()
	f:=t12SocketWithQueuedVideo(t,runtime)
	ctx,stop:=context.WithTimeout(context.Background(),10*time.Second)
	defer stop()
	outcomes:=make(chan CommandOutcome,16)
	f.manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,o CommandOutcome)error{
		select{case outcomes<-o:default:}
		return nil
	})
	previous:=f.dispatchCommand("t12-remote-resume-original")
	previous.Dispatch.Envelope.OriginalUserIntent="native first turn"
	if e:=f.manager.SendCommand(ctx,f.identity.Node,previous);e!=nil{t.Fatal(e)}
	if o:=waitCommandOutcome(t,ctx,outcomes);o.CommandID!=previous.Metadata().CommandID||o.State!=CommandAccepted{
		t.Fatalf("prior native Worker not accepted: %+v",o)
	}
	meta:=previous.Metadata()
	meta.CommandID="t12-remote-resume-hangs"
	meta.AttemptID="attempt-native-resume-hangs"
	meta.TurnID="turn-native-resume-hangs"
	meta.IssuedAt=time.Now().UTC()
	env:=previous.Dispatch.Envelope
	env.PreviousAttemptID=previous.Metadata().AttemptID
	env.AttemptID=meta.AttemptID
	env.TurnID=meta.TurnID
	env.OriginalUserIntent="second turn via native Resume"
	command:=Command{Kind:CommandResume,Resume:&ResumeCommand{Metadata:meta,Envelope:env}}
	if e:=f.manager.SendCommand(ctx,f.identity.Node,command);e!=nil{t.Fatal(e)}
	select{case <-runtime.entered:case <-ctx.Done():t.Fatal("native Resume did not enter provider")}
	if runtime.resumeCount.Load()!=1{t.Fatal("native Resume duplicated before reconnect")}
	oldCtx:=runtime.Context()
	if oldCtx==nil||oldCtx.Err()!=nil{t.Fatal("native Resume does not have live daemon-owned context")}
	record,e:=f.local.Command(meta.CommandID)
	if e!=nil || record.State!=CommandProcessing || record.SafePrestart{
		t.Fatalf("native Resume was not durably handed off: %+v err=%v",record,e)
	}
	f.manager.mu.Lock()
	firstSocket:=f.manager.connections[f.identity.Node]
	f.manager.mu.Unlock()
	if firstSocket==nil{t.Fatal("initial authenticated WebSocket missing")}
	f.manager.closeConnection(f.identity.Node)
	// Regression: the blocked native Resume must NOT hold a WaitGroup that
	// prevents the Node from establishing a replacement authenticated socket.
	waitFor(t,ctx,"reconnect while native Resume is blocked",func()bool{
		f.manager.mu.Lock()
		socket:=f.manager.connections[f.identity.Node]
		f.manager.mu.Unlock()
		return socket!=nil && socket!=firstSocket
	})
	if oldCtx.Err()!=nil{t.Fatalf("WebSocket disconnect canceled native Resume: %v",oldCtx.Err())}
	if runtime.resumeCount.Load()!=1{t.Fatalf("reconnect relaunched native Resume: %d",runtime.resumeCount.Load())}
	// Replaying the original CommandID while provider Resume is blocked
	// must NOT send an early terminal ACCEPTED or reserve its outbox ID.
	if e:=f.manager.SendCommand(ctx,f.identity.Node,command);e!=nil{t.Fatal(e)}
	if runtime.resumeCount.Load()!=1{t.Fatal("duplicate native Resume invoked after reconnect")}
	select {
	case early:=<-outcomes:
		t.Fatalf("in-flight replay generated premature terminal receipt: %+v",early)
	case <-time.After(200*time.Millisecond):
	}
	pending,e:=f.local.PendingEvents()
	if e!=nil{t.Fatal(e)}
	for _,event:=range pending {
		if event.EventID=="command-outcome-"+meta.CommandID{
			t.Fatal("in-flight Resume reserved terminal outbox event before provider returned")
		}
	}

	cancelMeta:=meta
	cancelMeta.CommandID="t12-remote-resume-cancel-after-reconnect"
	cancelMeta.IssuedAt=time.Now().UTC()
	cancelCommand:=Command{Kind:CommandCancel,Cancel:&CancelCommand{Metadata:cancelMeta,Reason:"owner cancels stalled Resume"}}
	begin:=time.Now()
	if e:=f.manager.SendCommand(ctx,f.identity.Node,cancelCommand);e!=nil{t.Fatal(e)}
	cancelOutcome:=waitCommandOutcome(t,ctx,outcomes)
	if cancelOutcome.CommandID!=cancelMeta.CommandID||cancelOutcome.State!=CommandAccepted{
		t.Fatalf("Cancel was blocked behind native Resume: %+v",cancelOutcome)
	}
	if time.Since(begin)>3*time.Second{t.Fatal("Cancel did not complete before releasing native provider")}
	waitFor(t,ctx,"native Resume cancellation",func()bool{return oldCtx.Err()!=nil})
	record,e=f.local.Command(meta.CommandID)
	if e!=nil||!record.NativeResumeCancelRequested{
		t.Fatalf("native Resume cancel was not persisted: %+v err=%v",record,e)
	}
	if _,ok:=f.local.SessionMapping(meta.AttemptID);ok{t.Fatal("native Resume got late mapping before release")}
	close(runtime.release)
	waitFor(t,ctx,"native Resume cancellation committed",func()bool{
		rec,e:=f.local.Command(meta.CommandID)
		return e==nil && rec.State==CommandFailed
	})
	if _,ok:=f.local.SessionMapping(meta.AttemptID);ok{t.Fatal("canceled Resume registered a new session mapping")}
	final:=waitCommandOutcome(t,ctx,outcomes)
	if final.CommandID!=meta.CommandID||final.State!=CommandFailed {
		t.Fatalf("expected final FAILED after provider returned: %+v",final)
	}
	waitFor(t,ctx,"final FAILED receipt ACK",func()bool{
		rec,err:=f.local.Command(meta.CommandID)
		return err==nil && rec.Acknowledged && rec.Outcome.State==CommandFailed
	})
	waitFor(t,ctx,"terminal outbox ACK",func()bool{
		events,err:=f.local.PendingEvents()
		if err!=nil{return false}
		for _,event:=range events {if event.EventID=="command-outcome-"+meta.CommandID{return false}}
		return true
	})
	// A further reconnect cannot deliver a second terminal receipt.
	f.manager.closeConnection(f.identity.Node)
	waitFor(t,ctx,"reconnected socket after final ACK",func()bool{
		f.manager.mu.Lock();socket:=f.manager.connections[f.identity.Node];f.manager.mu.Unlock()
		return socket!=nil
	})
	select {
	case extra:=<-outcomes:
		if extra.CommandID==meta.CommandID{t.Fatalf("duplicate final Resume receipt: %+v",extra)}
	case <-time.After(200*time.Millisecond):
	}
	if runtime.resumeCount.Load()!=1{t.Fatalf("native Resume count=%d",runtime.resumeCount.Load())}
	runtime.mu.Lock()
	late:=runtime.lateSession
	runtime.mu.Unlock()
	if late==nil{t.Fatal("test did not exercise provider returning a late native session")}
	select{case <-late.activity:default:t.Fatal("late native Session not closed after cancellation")}
}
