package secretary

import (
 "context"
 "errors"
 "path/filepath"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/node"
)

func newPromptBarrierFixture(t *testing.T) (context.Context, *core.Store, core.SecretaryIdentity, *Runtime, *fakeSession) {
 t.Helper()
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
 t.Cleanup(cancel)
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"secretary.db"))
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){store.Close()})
 person,conversation,err:=store.CreatePersonWithConversation(ctx)
 if err!=nil{t.Fatal(err)}
 identity,err:=store.EnsureSecretaryIdentity(ctx,person.ID,conversation.ID)
 if err!=nil{t.Fatal(err)}
 if _,err=store.SaveUserDocument(ctx,filepath.Join(t.TempDir(),"user.md"),"barrier owner");err!=nil{t.Fatal(err)}
 setRuntimeTestPolicy(t,store)
 fake:=&fakeRuntime{}
 runtime:=NewRuntime(node.NewLocal(fake),"test-capability")
 runtime.AttachIdentity(identity)
 runtime.AttachConversation(store,conversation.ID)
 if err=runtime.Start(ctx);err!=nil{t.Fatal(err)}
 t.Cleanup(func(){_ = runtime.Stop(context.Background())})
 return ctx,store,identity,runtime,fake.session
}

func waitBarrierTurn(t *testing.T,ctx context.Context,store *core.Store,identity core.SecretaryIdentity) core.SecretaryTurn {
 t.Helper()
 for {
  turns,err:=store.SecretaryTurns(ctx,identity.ID)
  if err!=nil{t.Fatal(err)}
  if len(turns)==1&&turns[0].State==core.SecretaryTurnActive&&turns[0].PromptState=="started"{return turns[0]}
  select{case <-ctx.Done():t.Fatalf("prompt never reached started: %+v",turns)
  case <-time.After(time.Millisecond):}
 }
}

func TestPromptResultBarrierControlledInterleavings(t *testing.T) {
 for _,mode:=range []string{"success","prompt_error","cancel","stop"} {
  t.Run(mode,func(t *testing.T){
   ctx,store,identity,runtime,session:=newPromptBarrierFixture(t)
   emitted:=make(chan struct{})
   release:=make(chan struct{})
   defer func(){select{case <-release:default:close(release)}}()
   promptFailure:=errors.New("injected prompt rejection")
   session.onPrompt=func(context.Context,string)error{
    status:="succeeded"
    if mode=="cancel"{status="canceled"}
    session.results<-node.Result{Status:status,Summary:"synthetic terminal"}
    close(emitted)
    <-release
    if mode=="prompt_error"{return promptFailure}
    return nil
   }
   session.onCancel=func(context.Context)error{return nil}
   if err:=runtime.HandleMessage(ctx,"controlled terminal interleaving");err!=nil{t.Fatal(err)}
   select{case <-emitted:case <-ctx.Done():t.Fatal("Prompt did not publish terminal Result")}
   turn:=waitBarrierTurn(t,ctx,store,identity)
   runtime.mu.Lock()
   barrier:=runtime.promptReturn
   runtime.mu.Unlock()
   if barrier==nil{t.Fatal("terminal Result has no prompt settlement barrier")}
   select{case <-barrier:t.Fatal("prompt barrier closed before Prompt returned")
   default:}
   active,err:=store.SecretaryTurn(ctx,turn.ID)
   if err!=nil||active.State!=core.SecretaryTurnActive||active.PromptState!="started"{
    t.Fatalf("premature terminal completion: %+v %v",active,err)
   }
   if mode=="stop"{
    if err:=runtime.Stop(ctx);err!=nil{t.Fatal(err)}
    select{case <-barrier:case <-ctx.Done():t.Fatal("Stop did not release result consumer")}
    close(release)
    if err:=runtime.Start(ctx);err!=nil{t.Fatal(err)}
    final:=waitForPromptBarrierTerminal(t,ctx,store,turn.ID)
    if final.State!=core.SecretaryTurnInterrupted{
     t.Fatalf("restarted turn not interrupted: %+v",final)
    }
    return
   }
   if mode=="cancel"{
    runtime.mu.Lock()
    promptID:=runtime.activePromptID
    runtime.mu.Unlock()
    runtime.cancelActivePrompt(ctx,session,promptID)
   }
   close(release)
   final:=waitForPromptBarrierTerminal(t,ctx,store,turn.ID)
   want:=core.SecretaryTurnSucceeded
   if mode=="prompt_error"{want=core.SecretaryTurnFailed}
   if mode=="cancel"{want=core.SecretaryTurnCanceled}
   if final.State!=want{t.Fatalf("terminal state %s, want %s: %+v",final.State,want,final)}
   if mode!="prompt_error"&&final.PromptState!="accepted"{t.Fatalf("Prompt was not accepted before terminal completion: %+v",final)}
   if mode=="prompt_error"&&final.PromptState=="accepted"{t.Fatalf("failed Prompt was accepted: %+v",final)}
   if mode=="prompt_error"{
    if final.Error!=promptFailure.Error(){t.Fatalf("failed Prompt terminal error=%q, want %q",final.Error,promptFailure.Error())}
    select{
    case reported:=<-runtime.Errors():
     if reported==nil||reported.Error()!=promptFailure.Error()||errors.Is(reported,core.ErrInvalidTransition){
      t.Fatalf("unexpected runtime error instead of injected Prompt rejection: %v",reported)
     }
    case <-ctx.Done():t.Fatal("expected injected Prompt rejection was not reported")
    }
   }
   select{case reported:=<-runtime.Errors():t.Fatalf("unexpected additional runtime transition error: %v",reported)
   default:}
   events,err:=store.SecretaryEvents(ctx,turn.ID,0,30)
   if err!=nil{t.Fatal(err)}
   finished:=0
   for _,event:=range events{if event.Kind==core.SecretaryTurnFinishedEvent{finished++}}
   if finished!=1{t.Fatalf("expected exactly one terminal event, got %d",finished)}
  })
 }
}

func waitForPromptBarrierTerminal(t *testing.T,ctx context.Context,store *core.Store,turnID string) core.SecretaryTurn {
 t.Helper()
 for{
  turn,err:=store.SecretaryTurn(ctx,turnID)
  if err!=nil{t.Fatal(err)}
  if turn.State.Terminal(){return turn}
  select{case <-ctx.Done():t.Fatalf("Secretary turn remains nonterminal: %+v",turn)
  case <-time.After(time.Millisecond):}
 }
}
