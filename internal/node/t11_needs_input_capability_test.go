package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type t11CapabilityRuntime struct{ session *t11CapabilitySession }
func (r *t11CapabilityRuntime) Start(context.Context, StartRequest) (Session,error) {
	return r.session,nil
}
type t11CapabilitySession struct {
	*fakeSession
	textReplies int
	imageReplies int
}
func (*t11CapabilitySession) SupportsImageResponse() bool { return false }
func (s *t11CapabilitySession) Respond(context.Context,string,string)error {
	s.textReplies++
	return nil
}
func (s *t11CapabilitySession) RespondImages(context.Context,string,string,[]ImageInput)error {
	s.imageReplies++
	return ErrImageResponseUnsupported
}

func TestT11UnsupportedACPStyleImageDoesNotClaimNeedsInputRequest(t *testing.T) {
	ctx:=context.Background()
	store,err:=OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err!=nil {t.Fatal(err)}
	session:=&t11CapabilitySession{fakeSession:&fakeSession{id:"native",activity:make(chan Activity,2),result:make(chan Result,2)}}
	n:=NewExecutionNode("local",&t11CapabilityRuntime{session:session},store)
	id:="art_"+strings.Repeat("a",32)
	original:=t10PNG(t)
	hash:=sha256.Sum256(original)
	n.SetArtifactFetcher(func(_ context.Context,artifactID string)(ArtifactPayload,error){
		if artifactID!=id {return ArtifactPayload{},core.ErrNotFound}
		return ArtifactPayload{ID:id,Name:"photo.png",SHA256:hex.EncodeToString(hash[:]),Data:original},nil
	})
	harness:=core.HarnessInstance{ID:"local/fx",Node:"local",Kind:core.HarnessFX,Version:"test",Status:core.HarnessReady,
		Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityImageInput}}}
	meta:=core.CommandMetadata{CommandID:"dispatch",Node:"local",WorkerRef:"worker",TurnID:"turn",AttemptID:"attempt",HarnessInstanceID:harness.ID,IssuedAt:time.Now().UTC()}
	envelope:=WorkerEnvelope{WorkerRef:meta.WorkerRef,TurnID:meta.TurnID,AttemptID:meta.AttemptID,OriginalUserIntent:"start",HarnessInstance:harness,LegacyWorkerTemplate:true}
	dispatch:=Command{Kind:CommandDispatch,Dispatch:&DispatchCommand{Metadata:meta,Envelope:envelope}}
	o,err:=n.HandleCommand(ctx,dispatch)
	if err!=nil || o.State!=CommandAccepted {t.Fatalf("dispatch: %#v %v",o,err)}
	respond:=func(commandID,response string)CommandOutcome{
		meta.CommandID=commandID
		oc,err:=n.HandleCommand(ctx,Command{Kind:CommandRespondWorker,RespondWorker:&RespondWorkerCommand{Metadata:meta,RequestID:"pending-A",Response:response}})
		if err!=nil {t.Fatal(err)}
		return oc
	}
	bad:=respond("respond-image","Inspect artifact://"+id)
	if bad.State==CommandAccepted || bad.ErrorCode!="image_input_unsupported" {t.Fatalf("preclaim image rejection not deterministic: %#v",bad)}
	if len(store.state.WorkerResponses)!=0 || session.imageReplies!=0 || session.textReplies!=0 {
		t.Fatalf("unsupported image mutated native request: state=%#v",store.state.WorkerResponses)
	}
	good:=respond("respond-text","Answer in text")
	if good.State!=CommandAccepted || session.textReplies!=1 {
		t.Fatalf("same native request cannot accept text after image rejection: %#v replies=%d",good,session.textReplies)
	}
}
