package node

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// A successful provider return can race the old connection's buffered
// completions with commandCtx.Done(). Its accepted receipt must already be
// persisted and delivered by the replacement socket's durable outbox.
func TestT12RemoteNativeResumeSuccessAfterDisconnectDeliversOneDurableReceipt(t *testing.T) {
	runtime := &t12RemoteStalledResumeRuntime{
		entered: make(chan struct{},1),
		release: make(chan struct{}),
	}
	defer func(){select{case <-runtime.release:default:close(runtime.release)}}()
	f := t12SocketWithQueuedVideo(t,runtime)
	ctx, cancel := context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	receipts := make(chan CommandOutcome,16)
	var delivered atomic.Int32
	f.manager.SetCommandOutcomeSink(func(_ context.Context,_ core.NodeReference,o CommandOutcome) error {
		if o.CommandID=="t12-resume-success-after-disconnect" {delivered.Add(1)}
		select{case receipts<-o:default:}
		return nil
	})

	previous:=f.dispatchCommand("t12-prior-worker-success-reconnect")
	previous.Dispatch.Envelope.OriginalUserIntent="Start an existing native session"
	if err:=f.manager.SendCommand(ctx,f.identity.Node,previous);err!=nil{t.Fatal(err)}
	got:=waitCommandOutcome(t,ctx,receipts)
	if got.CommandID!=previous.Metadata().CommandID || got.State!=CommandAccepted {
		t.Fatalf("prior Worker not started: %+v",got)
	}

	meta:=previous.Metadata()
	meta.CommandID="t12-resume-success-after-disconnect"
	meta.AttemptID="attempt-success-after-disconnect"
	meta.TurnID="turn-success-after-disconnect"
	meta.IssuedAt=time.Now().UTC()
	envelope:=previous.Dispatch.Envelope
	envelope.PreviousAttemptID=previous.Metadata().AttemptID
	envelope.AttemptID=meta.AttemptID
	envelope.TurnID=meta.TurnID
	envelope.OriginalUserIntent="Native provider will finish after socket reconnect"
	command:=Command{Kind:CommandResume,Resume:&ResumeCommand{Metadata:meta,Envelope:envelope}}
	if err:=f.manager.SendCommand(ctx,f.identity.Node,command);err!=nil{t.Fatal(err)}
	select{case <-runtime.entered:case <-ctx.Done():t.Fatal("native Resume never began")}
	if runtime.resumeCount.Load()!=1{t.Fatalf("native Resume invoked %d times",runtime.resumeCount.Load())}
	record,err:=f.local.Command(meta.CommandID)
	if err!=nil||record.State!=CommandProcessing||record.SafePrestart {
		t.Fatalf("native Resume not durably handed off: %+v err=%v",record,err)
	}

	f.manager.mu.Lock()
	initial:=f.manager.connections[f.identity.Node]
	f.manager.mu.Unlock()
	if initial==nil{t.Fatal("initial WebSocket missing")}
	f.manager.closeConnection(f.identity.Node)
	waitFor(t,ctx,"replacement WebSocket before native Resume completion",func()bool{
		f.manager.mu.Lock()
		next:=f.manager.connections[f.identity.Node]
		f.manager.mu.Unlock()
		return next!=nil && next!=initial
	})
	if runtime.resumeCount.Load()!=1 {t.Fatal("native Resume restarted during reconnect")}

	// Connection 1 is closed, yet its completion goroutine is still active.
	// Release only after Connection 2 is ready, reproducing the old select race.
	close(runtime.release)
	got=waitCommandOutcome(t,ctx,receipts)
	if got.CommandID!=meta.CommandID||got.Kind!=CommandResume||got.State!=CommandAccepted {
		t.Fatalf("durable accepted Resume receipt missing after reconnect: %+v",got)
	}
	waitFor(t,ctx,"successful resume receipt ACK",func()bool{
		r,e:=f.local.Command(meta.CommandID)
		return e==nil && r.Acknowledged && r.Outcome.State==CommandAccepted
	})
	waitFor(t,ctx,"successful resume outbox drained",func()bool{
		remaining,e:=f.local.PendingEvents()
		return e==nil&&len(remaining)==0
	})
	if _,mapped:=f.local.SessionMapping(meta.AttemptID);!mapped{
		t.Fatal("successful native Resume did not persist Session mapping")
	}
	if runtime.resumeCount.Load()!=1{t.Fatalf("duplicate native Resume: %d",runtime.resumeCount.Load())}
	// There must be no second receipt caused by a direct SendCommandOutcome
	// racing the new connection's outbox flush.
	time.Sleep(200*time.Millisecond)
	if delivered.Load()!=1 {
		t.Fatalf("successful native Resume delivered %d receipts; want exactly one",delivered.Load())
	}
}

func TestT12LifecycleCommandReceiptDeduplicatesBeforeAndAfterAck(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	store,err:=OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	command:=daemonDispatchFixture("macbook","t12-receipt-once")
	if _,duplicate,err:=store.ClaimCommand(command);err!=nil||duplicate {
		t.Fatalf("durable claim: dup=%v err=%v",duplicate,err)
	}
	outcome:=acceptedOutcome(command)
	if _,err:=store.CompleteCommand(command.Metadata().CommandID,outcome);err!=nil{t.Fatal(err)}
	for i:=0;i<3;i++{
		if _,err:=store.QueueLifecycleCommandOutcome(outcome);err!=nil{t.Fatalf("queue receipt %d: %v",i,err)}
	}
	pending,err:=store.PendingEvents()
	if err!=nil||len(pending)!=1{t.Fatalf("same CommandID produced %d events: %v",len(pending),err)}
	if err:=store.AckThrough(pending[0].Sequence);err!=nil{t.Fatal(err)}
	if _,err:=store.QueueLifecycleCommandOutcome(outcome);err!=nil{t.Fatal(err)}
	pending,err=store.PendingEvents()
	if err!=nil||len(pending)!=0{t.Fatalf("ACKed lifecycle receipt was queued again: events=%d err=%v",len(pending),err)}
	_ = ctx
}
