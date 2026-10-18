package ctl

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/node"
)

// A local ACP-like session reports successful multimodal steering, but the
// native needs_input ACK is intentionally delayed beyond the first command.
type t11LocalDelayedACPImageSession struct {
	*t10LocalVisionSession
	mu sync.Mutex
	injected int
	responded int
	legacyCalls int
	received []byte
	ack chan struct{}
	injectErr error
}
func (s *t11LocalDelayedACPImageSession) SupportsImageResponse() bool { return true }
func (s *t11LocalDelayedACPImageSession) InjectResponseImages(_ context.Context, _ string, _ string, images []node.ImageInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.injected++
	if len(images)==1 {s.received=append([]byte(nil),images[0].Data...)}
	return s.injectErr
}
func (s *t11LocalDelayedACPImageSession) Respond(ctx context.Context,_,_ string) error {
	s.mu.Lock()
	s.responded++
	s.mu.Unlock()
	select {
	case <-s.ack:return nil
	case <-ctx.Done():return ctx.Err()
	}
}
func (s *t11LocalDelayedACPImageSession) RespondImages(context.Context,string,string,[]node.ImageInput)error {
	s.mu.Lock();s.legacyCalls++;s.mu.Unlock()
	return errors.New("legacy RespondImages bypassed durable injection checkpoint")
}

func TestT11LocalNeedsInputDelayedACKDoesNotReinjectOnRetry(t *testing.T) {
	original:=t10PNGFixture(t)
	artifact:="art_"+strings.Repeat("a",32)
	session:=&t11LocalDelayedACPImageSession{t10LocalVisionSession:&t10LocalVisionSession{},ack:make(chan struct{})}
	rt,meta,_:=t10LocalSteeringSetup(t,session,true,t10LocalOriginal(artifact,"telegram.png",original))
	path:=filepath.Join(t.TempDir(),"local-response-checkpoints.json")
	firstStore,err:=node.OpenLocalStore(path)
	if err!=nil{t.Fatal(err)}
	rt.Local.SetImageResponseStore(firstStore)
	send:=func(ctx context.Context,commandID string)(node.CommandOutcome,error){
		meta.CommandID=commandID
		return rt.LocalCommand(ctx,node.Command{Kind:node.CommandRespondWorker,
			RespondWorker:&node.RespondWorkerCommand{Metadata:meta,RequestID:"image-question-A",
			Response:"Describe original artifact://"+artifact}})
	}
	ctx,cancel:=context.WithTimeout(context.Background(),70*time.Millisecond)
	defer cancel()
	first,err:=send(ctx,"answer-first")
	if err==nil || !errors.Is(err,node.ErrCommandOutcomeUnknown) || first.State==node.CommandAccepted {
		t.Fatalf("first native ACK timeout not uncertain: %#v err=%v",first,err)
	}
	session.mu.Lock()
	injected,responded,legacy:=session.injected,session.responded,session.legacyCalls
	got:=append([]byte(nil),session.received...)
	session.mu.Unlock()
	if injected!=1 || responded!=1 || legacy!=0 || !bytes.Equal(got,original) {
		t.Fatalf("initial native delivery wrong: injections=%d responses=%d legacy=%d bytes=%v",injected,responded,legacy,bytes.Equal(got,original))
	}
	// Simulate a service restart: a fresh LocalStore must retain the
	// native injection confirmation and allow ONLY the ACK stage to retry.
	reopened,err:=node.OpenLocalStore(path)
	if err!=nil {t.Fatal(err)}
	rt.Local.SetImageResponseStore(reopened)
	close(session.ack)
	second,err:=send(context.Background(),"answer-retry")
	if err!=nil || second.State!=node.CommandAccepted {
		t.Fatalf("retry after ACK timeout not accepted: %#v err=%v",second,err)
	}
	session.mu.Lock()
	injected,responded,legacy=session.injected,session.responded,session.legacyCalls
	session.mu.Unlock()
	if injected!=1 || responded!=2 || legacy!=0 {
		t.Fatalf("native bytes injected more than once: injections=%d responses=%d legacy=%d",injected,responded,legacy)
	}
}

func TestT11LocalAmbiguousImageInjectionFailsClosedAcrossRestart(t *testing.T) {
	original:=t10PNGFixture(t)
	artifact:="art_"+strings.Repeat("b",32)
	session:=&t11LocalDelayedACPImageSession{
		t10LocalVisionSession:&t10LocalVisionSession{},ack:make(chan struct{}),
		injectErr:context.DeadlineExceeded,
	}
	rt,meta,_:=t10LocalSteeringSetup(t,session,true,t10LocalOriginal(artifact,"telegram.png",original))
	path:=filepath.Join(t.TempDir(),"local-response-checkpoints.json")
	store,err:=node.OpenLocalStore(path)
	if err!=nil{t.Fatal(err)}
	rt.Local.SetImageResponseStore(store)
	call:=func(id string)(node.CommandOutcome,error){
		meta.CommandID=id
		return rt.LocalCommand(context.Background(),node.Command{Kind:node.CommandRespondWorker,
			RespondWorker:&node.RespondWorkerCommand{Metadata:meta,RequestID:"ambiguous-question",
			Response:"Inspect original artifact://"+artifact}})
	}
	first,err:=call("inject-first")
	if err==nil || first.State==node.CommandAccepted {t.Fatalf("ambiguous first image incorrectly accepted: %#v %v",first,err)}
	reopened,err:=node.OpenLocalStore(path)
	if err!=nil{t.Fatal(err)}
	rt.Local.SetImageResponseStore(reopened)
	second,err:=call("inject-retry")
	if err==nil || second.State==node.CommandAccepted {t.Fatalf("ambiguous second image incorrectly accepted: %#v %v",second,err)}
	session.mu.Lock()
	injected,responded:=session.injected,session.responded
	session.mu.Unlock()
	if injected!=1 || responded!=0 {t.Fatalf("uncertain local injection retried: injections=%d responses=%d",injected,responded)}
}

func TestT11LocalImageRejectsMissingDurableCheckpointBeforeNativeCall(t *testing.T) {
	original:=t10PNGFixture(t)
	artifact:="art_"+strings.Repeat("c",32)
	session:=&t11LocalDelayedACPImageSession{t10LocalVisionSession:&t10LocalVisionSession{},ack:make(chan struct{})}
	rt,meta,_:=t10LocalSteeringSetup(t,session,true,t10LocalOriginal(artifact,"telegram.png",original))
	rt.Local.SetImageResponseStore(nil)
	meta.CommandID="no-store"
	outcome,err:=rt.LocalCommand(context.Background(),node.Command{Kind:node.CommandRespondWorker,
		RespondWorker:&node.RespondWorkerCommand{Metadata:meta,RequestID:"missing-store",
		Response:"Describe artifact://"+artifact}})
	if err==nil || outcome.State==node.CommandAccepted {
		t.Fatalf("local image accepted without durable store: %#v err=%v",outcome,err)
	}
	session.mu.Lock();defer session.mu.Unlock()
	if session.injected!=0 || session.responded!=0 {t.Fatal("native side effect occurred without durable checkpoint")}
}
