package ctl

import (
	"context"

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

