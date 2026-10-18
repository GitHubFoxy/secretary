package webapi

import (
 "context"
 "errors"
 "bytes"
 "fmt"
 "strings"

 "github.com/beruseruko/secretary/internal/node"
)

// A scripted native Secretary harness drives the same automatic prompt/MCP
// path as a real native Secretary; test code never submits spawn_worker.
type t13SecretaryHarness struct { onPrompt func(string)error }
type t13SecretarySession struct {
 onPrompt func(string)error
 results chan node.Result
 activity chan node.Activity
}
func(r *t13SecretaryHarness)Start(context.Context,node.StartRequest)(node.Session,error){
 return &t13SecretarySession{onPrompt:r.onPrompt,results:make(chan node.Result,4),activity:make(chan node.Activity,4)},nil
}
func(*t13SecretarySession)ID()string{return "t13-secretary-session"}
func(s *t13SecretarySession)Prompt(_ context.Context,prompt string)error{
 if s.onPrompt==nil{return errors.New("T13 Secretary harness not connected")}
 if err:=s.onPrompt(prompt);err!=nil{return err}
 s.results<-node.Result{Status:"failed",CompletionEvidence:&node.TerminalCompletionEvidence{
  Contract:node.TerminalCompletionOpenCodeV2,StopReason:node.TerminalStopReasonEndTurn,
  RPCSucceeded:true,DrainCompleted:true,
 }}
 return nil
}
func(*t13SecretarySession)Steer(context.Context,string)(bool,error){return false,nil}
func(*t13SecretarySession)Cancel(context.Context)error{return nil}
func(*t13SecretarySession)Respond(context.Context,string,string)error{return nil}
func(s *t13SecretarySession)Activity()<-chan node.Activity{return s.activity}
func(s *t13SecretarySession)Result()<-chan node.Result{return s.results}
func(*t13SecretarySession)Close()error{return nil}

func t13SecretaryPromptIdentity(prompt string)(string,string,string,error){
 const head="\n\nActive reply identity (server-issued; use these exact values):\n"
 i:=strings.Index(prompt,head)
 if i<0{return "","","",errors.New("missing Server-issued identity")}
 payload:=strings.Split(prompt[i+len(head):],"\n")
 if len(payload)<2||!strings.HasPrefix(payload[0],"secretary_turn_id=")||!strings.HasPrefix(payload[1],"input_id="){
  return "","","",errors.New("invalid Server-issued identity")
 }
 return prompt[:i],strings.TrimPrefix(payload[0],"secretary_turn_id="),strings.TrimPrefix(payload[1],"input_id="),nil
}

type t13VideoNativeRuntime struct { t12Runtime }
func(r *t13VideoNativeRuntime)Resume(_ context.Context,_ node.StartRequest,id string)(node.Session,error){
 if id!="artifact-worker-session"{return nil,fmt.Errorf("unexpected native session %s",id)}
 return &t13VideoFollowupSession{
  artifactWorkerSession:&artifactWorkerSession{result:make(chan node.Result,1)},
  seen:r.seen,
 },nil
}
type t13VideoFollowupSession struct{
 *artifactWorkerSession
 seen chan node.StartRequest
}
func(s *t13VideoFollowupSession)PromptImages(_ context.Context,prompt string,images []node.ImageInput)error{
 if len(images)!=2||images[0].MediaType!="image/webp"||images[1].MediaType!="image/webp"||
 !bytes.Equal(images[0].Data,videoTestWebP)||!bytes.Equal(images[1].Data,videoTestWebP)||
 !strings.Contains(prompt,"1.000 s")||!strings.Contains(prompt,"Describe the sampled video"){
  return fmt.Errorf("Telegram Topic native Worker lost video frames/timestamps: images=%d prompt=%q",len(images),prompt)
 }
 s.seen<-node.StartRequest{Task:prompt,Images:images}
 s.result<-node.Result{Status:"succeeded",Summary:"T13 topic native Worker analyzed sampled video"}
 return nil
}
