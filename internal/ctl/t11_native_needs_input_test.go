package ctl

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/node"
)

type t11NativeImageAnswer struct {
	*t10LocalVisionSession
	called int
	legacy int
	images []node.ImageInput
	text string
	requestID string
}
func (s *t11NativeImageAnswer) Respond(_ context.Context, _ string, _ string) error {
	s.legacy++
	return nil
}
func (s *t11NativeImageAnswer) RespondImages(_ context.Context, requestID, text string, images []node.ImageInput) error {
	s.called++
	s.requestID=requestID
	s.text=text
	s.images=append([]node.ImageInput(nil), images...)
	return nil
}

type t11NativeTextAnswer struct {
	*t10LocalVisionSession
	called int
}
func (s *t11NativeTextAnswer) Respond(context.Context,string,string)error {
	s.called++
	return nil
}

func TestT11NeedsInputLocalNodeDeliversVerifiedNativeImageBytes(t *testing.T) {
	original:=t10PNGFixture(t)
	id:="art_"+strings.Repeat("a",32)
	session:=&t11NativeImageAnswer{t10LocalVisionSession:&t10LocalVisionSession{}}
	runtime,meta,_:=t10LocalSteeringSetup(t,session,true,t10LocalOriginal(id,"telegram.png",original))
	meta.CommandID="respond-image"
	command:=node.Command{Kind:node.CommandRespondWorker,RespondWorker:&node.RespondWorkerCommand{
		Metadata:meta,RequestID:"owner-photo-question",Response:"Describe photo artifact://"+id,
	}}
	outcome,err:=runtime.LocalCommand(context.Background(),command)
	if err!=nil || outcome.State!=node.CommandAccepted {
		t.Fatalf("native image response was not accepted: outcome=%#v err=%v",outcome,err)
	}
	if session.called!=1 || session.legacy!=0 || session.requestID!="owner-photo-question" ||
		!strings.Contains(session.text,"artifact://"+id) || len(session.images)!=1 ||
		session.images[0].ArtifactID!=id || session.images[0].MediaType!="image/png" ||
		!bytes.Equal(session.images[0].Data,original) {
		t.Fatalf("native needs_input did not receive original image bytes: %#v",session)
	}
}

func TestT11NeedsInputLocalNodeNeverFallsBackToTextOnlyResponse(t *testing.T) {
	original:=t10PNGFixture(t)
	id:="art_"+strings.Repeat("b",32)
	session:=&t11NativeTextAnswer{t10LocalVisionSession:&t10LocalVisionSession{}}
	runtime,meta,_:=t10LocalSteeringSetup(t,session,true,t10LocalOriginal(id,"telegram.png",original))
	meta.CommandID="respond-unsupported"
	command:=node.Command{Kind:node.CommandRespondWorker,RespondWorker:&node.RespondWorkerCommand{
		Metadata:meta,RequestID:"owner-photo-question",Response:"Describe photo artifact://"+id,
	}}
	outcome,err:=runtime.LocalCommand(context.Background(),command)
	if err==nil || outcome.State==node.CommandAccepted || session.called!=0 {
		t.Fatalf("text-only native Respond falsely accepted image: outcome=%#v err=%v",outcome,err)
	}
}
