package webapi

import (
 "context"
 "errors"
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
