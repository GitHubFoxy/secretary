package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type t11IdempotentNativeSession struct {
	*fakeSession
	mu sync.Mutex
	injections int
	replies int
	ack chan struct{}
	injectionErr error
}
func (*t11IdempotentNativeSession) SupportsImageResponse() bool { return true }
func (s *t11IdempotentNativeSession) RespondImages(ctx context.Context, requestID, text string, images []ImageInput) error {
	if err:=s.InjectResponseImages(ctx,requestID,text,images);err!=nil {return err}
	return s.Respond(ctx,requestID,text)
}
func (s *t11IdempotentNativeSession) InjectResponseImages(_ context.Context, _ string, _ string, images []ImageInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(images)!=1 || images[0].MediaType!="image/png" {return ErrImageResponseUnsupported}
	s.injections++
	return s.injectionErr
}
func (s *t11IdempotentNativeSession) Respond(ctx context.Context,_,_ string) error {
	s.mu.Lock()
	s.replies++
	s.mu.Unlock()
	select {
	case <-s.ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
type t11IdempotentNativeRuntime struct {session *t11IdempotentNativeSession}
func (r *t11IdempotentNativeRuntime) Start(context.Context,StartRequest)(Session,error){return r.session,nil}

func t11NativeReplyNode(t *testing.T,storePath string,session *t11IdempotentNativeSession)(*ExecutionNode,string,core.CommandMetadata){
	t.Helper()
	store,err:=OpenLocalStore(storePath)
	if err!=nil {t.Fatal(err)}
	n:=NewExecutionNode("local",&t11IdempotentNativeRuntime{session:session},store)
	id:="art_"+strings.Repeat("d",32)
	original:=t10PNG(t)
	digest:=sha256.Sum256(original)
	n.SetArtifactFetcher(func(_ context.Context,artID string)(ArtifactPayload,error){
		if artID!=id {return ArtifactPayload{},core.ErrNotFound}
		return ArtifactPayload{ID:id,Name:"photo.png",SHA256:hex.EncodeToString(digest[:]),Data:original},nil
	})
	harness:=core.HarnessInstance{ID:"local/fx",Node:"local",Kind:core.HarnessFX,Version:"test",Status:core.HarnessReady,
		Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityImageInput}}}
	meta:=core.CommandMetadata{CommandID:"dispatch",Node:"local",WorkerRef:"worker",TurnID:"turn",AttemptID:"attempt",HarnessInstanceID:harness.ID,IssuedAt:time.Now().UTC()}
	envelope:=WorkerEnvelope{WorkerRef:meta.WorkerRef,TurnID:meta.TurnID,AttemptID:meta.AttemptID,OriginalUserIntent:"start",HarnessInstance:harness,LegacyWorkerTemplate:true}
	outcome,err:=n.HandleCommand(context.Background(),Command{Kind:CommandDispatch,Dispatch:&DispatchCommand{Metadata:meta,Envelope:envelope}})
	if err!=nil || outcome.State!=CommandAccepted {t.Fatalf("dispatch: %#v %v",outcome,err)}
	return n,id,meta
}

func TestT11ImageInjectionCheckpointPreventsDuplicateAfterNativeACKTimeout(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"node.json")
	session:=&t11IdempotentNativeSession{fakeSession:&fakeSession{id:"native",activity:make(chan Activity,3),result:make(chan Result,2)},ack:make(chan struct{})}
	n,id,meta:=t11NativeReplyNode(t,path,session)
	response:="Inspect the original artifact://"+id
	call:=func(ctx context.Context,commandID string)CommandOutcome{
		meta.CommandID=commandID
		outcome,err:=n.HandleCommand(ctx,Command{Kind:CommandRespondWorker,RespondWorker:&RespondWorkerCommand{
			Metadata:meta,RequestID:"same-request-A",Response:response,
		}})
		if err!=nil {t.Fatal(err)}
		return outcome
	}
	shortCtx,cancel:=context.WithTimeout(context.Background(),60*time.Millisecond)
	defer cancel()
	first:=call(shortCtx,"image-first")
	if first.State==CommandAccepted || first.ErrorCode!="execution_state_unknown" {
		t.Fatalf("delayed native ACK incorrectly acknowledged: %#v",first)
	}
	session.mu.Lock()
	injections,replies:=session.injections,session.replies
	session.mu.Unlock()
	if injections!=1 || replies!=1 {t.Fatalf("first attempt native operations: injected=%d replies=%d",injections,replies)}
	// Re-open durable Node state to prove the checkpoint survives a restart.
	restored,err:=OpenLocalStore(path)
	if err!=nil {t.Fatal(err)}
	if stage:=restored.state.ImageResponses["same-request-A"].Stage;stage!="injected"{
		t.Fatalf("confirmed image injection not durable: %q",stage)
	}
	n.store=restored
	close(session.ack)
	second:=call(context.Background(),"image-retry")
	if second.State!=CommandAccepted {t.Fatalf("retry should finish pending ACK: %#v",second)}
	session.mu.Lock()
	injections,replies=session.injections,session.replies
	session.mu.Unlock()
	if injections!=1 || replies!=2 {t.Fatalf("native image duplicated: injected=%d replies=%d",injections,replies)}
	again:=call(context.Background(),"image-repeat")
	if again.State!=CommandAccepted {t.Fatalf("duplicate request not idempotent: %#v",again)}
	session.mu.Lock()
	injections,replies=session.injections,session.replies
	session.mu.Unlock()
	if injections!=1 || replies!=2 {t.Fatalf("duplicate response replayed native side effects: injected=%d replies=%d",injections,replies)}
}

func TestT11UncertainImageInjectionNeverBlindlyRetriesNativeBytes(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"node.json")
	session:=&t11IdempotentNativeSession{fakeSession:&fakeSession{id:"native",activity:make(chan Activity,3),result:make(chan Result,2)},ack:make(chan struct{}),injectionErr:context.DeadlineExceeded}
	n,id,meta:=t11NativeReplyNode(t,path,session)
	call:=func(commandID string)CommandOutcome {
		meta.CommandID=commandID
		outcome,err:=n.HandleCommand(context.Background(),Command{Kind:CommandRespondWorker,RespondWorker:&RespondWorkerCommand{
			Metadata:meta,RequestID:"uncertain-request",Response:"Inspect artifact://"+id,
		}})
		if err!=nil{t.Fatal(err)}
		return outcome
	}
	first:=call("first-inject")
	if first.State==CommandAccepted {t.Fatal("uncertain native image must not ACK")}
	restored,err:=OpenLocalStore(path)
	if err!=nil{t.Fatal(err)}
	if stage:=restored.state.ImageResponses["uncertain-request"].Stage;stage!="started" {
		t.Fatalf("in-flight injection checkpoint missing: %q",stage)
	}
	n.store=restored
	second:=call("second-inject")
	if second.State==CommandAccepted || second.ErrorCode!="execution_state_unknown" {
		t.Fatalf("uncertain native injection should fail closed: %#v",second)
	}
	session.mu.Lock()
	injections:=session.injections
	session.mu.Unlock()
	if injections!=1 {t.Fatalf("ambiguous retry injected original twice: %d",injections)}
}
