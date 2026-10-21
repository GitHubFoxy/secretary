package ctl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type t10LocalVisionRuntime struct {
	started chan node.StartRequest
	session node.Session
}
func (r *t10LocalVisionRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.started <- request
	return r.session,nil
}
type t10LocalVisionSession struct {
	steered chan []node.ImageInput
	injected bool
	texts chan string
}
func (*t10LocalVisionSession) ID() string { return "native-vision" }
func (*t10LocalVisionSession) Prompt(context.Context,string) error { return nil }
func (s *t10LocalVisionSession) Steer(_ context.Context,text string) (bool,error) { if s.texts != nil { s.texts <- text }; return true,nil }
func (s *t10LocalVisionSession) SteerImages(_ context.Context, _ string, images []node.ImageInput) (bool,error) {
	s.steered <- images
	return s.injected,nil
}
func (*t10LocalVisionSession) Cancel(context.Context) error { return nil }
func (*t10LocalVisionSession) Activity() <-chan node.Activity { return nil }
func (*t10LocalVisionSession) Result() <-chan node.Result { return nil }
func (*t10LocalVisionSession) Close() error { return nil }

func TestT10LocalDispatchAndSteeringUseOriginalImageBytes(t *testing.T) {
	source := image.NewRGBA(image.Rect(0,0,2,2))
	source.Set(1,1,color.RGBA{R:255,A:255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer,source); err != nil { t.Fatal(err) }
	original := buffer.Bytes()
	checksum := sha256.Sum256(original)
	id := "art_"+strings.Repeat("a",32)
	runtime := &t10LocalVisionRuntime{
		started:make(chan node.StartRequest,1),
		session:&t10LocalVisionSession{steered:make(chan []node.ImageInput,1), injected:true},
	}
	local := node.NewLocal(runtime)
	defer local.Close()
	local.SetArtifactFetcher(func(_ context.Context, ref string) (node.ArtifactPayload,error) {
		if ref != id { t.Fatalf("wrong Artifact: %s",ref) }
		return node.ArtifactPayload{ID:id,Name:"original.png",SHA256:hex.EncodeToString(checksum[:]),Data:original},nil
	})
	nruntime := legacyLocalTestRuntime{Local:local}
	instance := core.HarnessInstance{
		ID:"local/fx",Node:"local",Kind:core.HarnessFX,Version:"test",Status:core.HarnessReady,
		Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityImageInput}},
	}
	envelope := node.WorkerEnvelope{WorkerRef:"visual-local",TurnID:"turn",AttemptID:"attempt",
		OriginalUserIntent:"What is shown? artifact://"+id,HarnessInstance:instance,LegacyWorkerTemplate:true}
	meta := core.CommandMetadata{CommandID:"dispatch",Node:"local",WorkerRef:envelope.WorkerRef,TurnID:envelope.TurnID,AttemptID:envelope.AttemptID,HarnessInstanceID:instance.ID,IssuedAt:time.Now().UTC()}
	cmd := node.Command{Kind:node.CommandDispatch,Dispatch:&node.DispatchCommand{Metadata:meta,Envelope:envelope}}
	outcome,err := nruntime.LocalCommand(context.Background(),cmd)
	if err != nil || outcome.State != node.CommandAccepted { t.Fatalf("local dispatch = %#v %v",outcome,err) }
	select {
	case request := <-runtime.started:
		if len(request.Images)!=1 || request.Images[0].ArtifactID!=id || !bytes.Equal(request.Images[0].Data,original) || !strings.Contains(request.Task,"What is shown?") {
			t.Fatalf("local native image not delivered: %#v",request)
		}
	default: t.Fatal("native Worker never launched")
	}
	meta.CommandID="steer"
	steer:=node.Command{Kind:node.CommandSteering,Steering:&node.SteeringCommand{Metadata:meta,Text:"Check color artifact://"+id}}
	outcome,err=nruntime.LocalCommand(context.Background(),steer)
	if err!=nil || outcome.State!=node.CommandAccepted { t.Fatalf("local steer = %#v %v",outcome,err) }
	select {
	case images:=<-runtime.session.(*t10LocalVisionSession).steered:
		if len(images)!=1 || images[0].ArtifactID!=id || !bytes.Equal(images[0].Data,original) { t.Fatalf("local steer lost image bytes: %#v",images) }
	default: t.Fatal("local image was not steered")
	}
}
