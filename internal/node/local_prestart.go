package node

import (
	"context"
	"errors"
)

// localPending owns the preparation cancellation path of exactly one local
// Attempt. The per-worker semaphore serializes all native handoff side effects
// without ever monopolizing LocalNode.mu across network and provider calls.
type localPending struct {
	workerRef string
	stage string // preparing, canceled, handoff
	cancel context.CancelFunc
	// A successful direct native Resume uses this Context for the entire
	// native Session lifetime. Ownership transfers from pending prestart to
	// LocalNode on handoff, and Remove/Close performs the final cancellation.
	keepContext bool
}

func (n *LocalNode) lockWorker(ctx context.Context,workerRef string)(func(),error){
	if workerRef==""{return nil,errors.New("node: WorkerRef required")}
	n.mu.Lock()
	gate:=n.workerGates[workerRef]
	if gate==nil{gate=make(chan struct{},1);gate<-struct{}{};n.workerGates[workerRef]=gate}
	n.mu.Unlock()
	select {
	case <-ctx.Done():return nil,ctx.Err()
	case <-gate:return func(){gate<-struct{}{}},nil
	}
}

func (n *LocalNode) beginLocalPrestart(ctx context.Context,attemptID,workerRef string)(context.Context,func(),error){
	if err:=ctx.Err();err!=nil{return nil,nil,err}
	key:=attemptID
	if key==""{key="worker:"+workerRef}
	prep,cancel:=context.WithCancel(ctx)
	n.mu.Lock()
	if n.prestarts[key]!=nil {
		n.mu.Unlock();cancel()
		return nil,nil,errors.New("node: local Attempt already preparing")
	}
	if n.prestartStore!=nil && attemptID!="" {
		if err:=n.prestartStore.BeginLocalPrestart(attemptID,workerRef);err!=nil{
			n.mu.Unlock();cancel();return nil,nil,err
		}
	}
	pending:=&localPending{workerRef:workerRef,stage:"preparing",cancel:cancel}
	n.prestarts[key]=pending
	n.mu.Unlock()
	finish:=func(){
		n.mu.Lock()
		if n.prestarts[key]==pending {
			if pending.stage=="preparing" && n.prestartStore!=nil && attemptID!=""{
				_ = n.prestartStore.ReleaseLocalPrestart(attemptID,workerRef)
			}
			delete(n.prestarts,key)
		}
		keep := pending.keepContext
		n.mu.Unlock()
		if !keep { cancel() }
	}
	return prep,finish,nil
}

func (n *LocalNode) commitLocalHandoff(ctx context.Context,attemptID,workerRef string) error{
	if err:=ctx.Err();err!=nil{return err}
	key:=attemptID
	if key==""{key="worker:"+workerRef}
	n.mu.Lock()
	defer n.mu.Unlock()
	pending:=n.prestarts[key]
	if pending==nil || pending.workerRef!=workerRef{return errors.New("node: local prestart lost")}
	if pending.stage=="canceled"{return ErrLocalPrestartCanceled}
	if err:=ctx.Err();err!=nil{return err}
	if n.prestartStore!=nil && attemptID!=""{
		if err:=n.prestartStore.MarkLocalHandoff(attemptID,workerRef);err!=nil{return err}
	}
	pending.stage="handoff"
	return nil
}

// markLocalResuming records the uncertain native entry boundary before
// calling a provider. The Attempt stays in stage "preparing" in memory so
// Cancel can interrupt a blocked native Resume without waiting for its gate.
func (n *LocalNode) markLocalResuming(ctx context.Context, attemptID, workerRef string) error {
	if err := ctx.Err(); err != nil {return err}
	key := attemptID
	if key == "" {key="worker:"+workerRef}
	n.mu.Lock()
	defer n.mu.Unlock()
	pending := n.prestarts[key]
	if pending == nil || pending.workerRef != workerRef {return errors.New("node: local Resume prestart lost")}
	if pending.stage == "canceled" {return ErrLocalPrestartCanceled}
	if pending.stage != "preparing" {return errors.New("node: local Resume already handed off")}
	if err := ctx.Err(); err != nil {return err}
	if n.prestartStore != nil && attemptID != "" {
		if err := n.prestartStore.MarkLocalResumeInFlight(attemptID,workerRef); err != nil {return err}
	}
	return nil
}

// CancelLocalAttempt is also valid BEFORE a Session exists. Cancellation is
// persisted under Node control before interrupting an in-flight media GET.
// If handoff already occurred, it waits only for the same Worker's semaphore
// and cancels the actual session, not unrelated Workers.
func (n *LocalNode) CancelLocalAttempt(ctx context.Context,workerRef,attemptID string)(bool,error){
	key:=attemptID
	if key==""{key="worker:"+workerRef}
	n.mu.Lock()
	pending:=n.prestarts[key]
	if pending!=nil && pending.workerRef==workerRef && pending.stage=="canceled" {n.mu.Unlock();return true,nil}
	if pending!=nil && pending.workerRef==workerRef && pending.stage=="preparing" {
		if n.prestartStore!=nil && attemptID!=""{
			ok,err:=n.prestartStore.CancelLocalPrestart(attemptID,workerRef)
			if err!=nil{n.mu.Unlock();return true,err}
			if !ok{n.mu.Unlock();return true,errors.New("node: local prestart cancellation conflict")}
		}
		pending.stage="canceled"
		pending.cancel()
		n.mu.Unlock()
		return true,nil
	}
	if pending==nil && n.prestartStore!=nil && attemptID!="" {
		ok,err:=n.prestartStore.CancelLocalPrestart(attemptID,workerRef)
		if err!=nil{n.mu.Unlock();return true,err}
		if ok{n.mu.Unlock();return true,nil}
	}
	n.mu.Unlock()
	unlock,err:=n.lockWorker(ctx,workerRef)
	if err!=nil{return true,err}
	defer unlock()
	n.mu.Lock()
	session,ok:=n.workers[workerRef]
	// A historical checkpoint may refer to a previous Attempt on this
	// same Worker. Only the Attempt bound to the current native session
	// may be canceled; an empty/unknown/stale ID is never authorized.
	ok=ok && attemptID!="" && n.activeAttempts[workerRef]==attemptID
	n.mu.Unlock()
	if !ok{return false,nil}
	return true,session.Cancel(ctx)
}
