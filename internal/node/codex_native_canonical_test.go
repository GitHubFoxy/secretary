package node

import (
 "context"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
)

func TestCodexNativeExitCanonicalResultPersistence(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
 defer cancel()
 db,err:=core.Open(ctx,filepath.Join(t.TempDir(),"codex-outcome.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 _,conversation,err:=db.CreatePersonWithConversation(ctx)
 if err!=nil{t.Fatal(err)}
 worker,turn,attempt,err:=db.CreateWorker(ctx,conversation.ID,core.WorkerSpec{
  WorkerRef:"codex-native-exit",Intent:"run command once",NodeID:"node-a",HarnessInstanceID:"node-a/codex",
 },core.TurnSpec{Input:"run exact exit 7"})
 if err!=nil{t.Fatal(err)}
 if _,err:=db.SetPhase4AttemptActive(ctx,attempt.ID);err!=nil{t.Fatal(err)}
 local,err:=OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
 if err!=nil{t.Fatal(err)}
 defer local.Close()
 runtime,_:=codexPhaseFixtureRuntime(t,"native-reordered")
 session,err:=runtime.Start(ctx,StartRequest{WorkerRef:worker.WorkerRef,Workspace:t.TempDir(),DeferInitialPrompt:true,Profile:ManagedProfile{Name:"secretary",Content:"MARKER",Runtime:"codex"}})
 if err!=nil{t.Fatal(err)}
 envelope:=WorkerEnvelope{WorkerRef:worker.WorkerRef,TurnID:turn.ID,AttemptID:attempt.ID,HarnessInstance:core.HarnessInstance{ID:"node-a/codex",Node:"node-a",Kind:core.HarnessCodex}}
 NewExecutionNode("node-a",nil,local).watchSession(session,envelope)
 if err:=session.Prompt(ctx,"run exact exit 7");err!=nil{t.Fatal(err)}
 var outcome core.AttemptOutcomeEnvelope
 for {
  events,err:=local.PendingEvents()
  if err!=nil{t.Fatal(err)}
  for _,pending:=range events{
   if pending.EventID!="attempt-outcome-"+attempt.ID{continue}
   var event NodeEvent
   if err:=json.Unmarshal(pending.Payload,&event);err!=nil{t.Fatal(err)}
   if event.Outcome!=nil{outcome=*event.Outcome}
  }
  if outcome.EventID!=""{break}
  if ctx.Err()!=nil{t.Fatalf("terminal outbox missing: %v",ctx.Err())}
  time.Sleep(10*time.Millisecond)
 }
 if outcome.Status!=core.OutcomeFailed||outcome.FailureCode!="tool_execution_failed"||!strings.Contains(outcome.Diagnostics,"exit code 7"){t.Fatalf("native terminal outcome=%+v",outcome)}
 sink:=NewStoreEventSink(db)
 for i:=0;i<2;i++{
  if err:=sink(ctx,outcomeEvent(outcome));err!=nil{t.Fatalf("persist %d: %v",i,err)}
 }
 details,err:=db.WorkerDetailsForConversation(ctx,conversation.ID,worker.WorkerRef)
 if err!=nil{t.Fatal(err)}
 if len(details.Results)!=1||len(details.Outcomes)!=1||details.CurrentAttempt().State!=core.AttemptFailed{t.Fatalf("canonical results=%d outcomes=%d attempt=%+v",len(details.Results),len(details.Outcomes),details.CurrentAttempt())}
 if details.Results[0].Status!=core.ResultFailed||details.Results[0].FailureCode!="tool_execution_failed"{t.Fatalf("canonical result=%+v",details.Results[0])}
 entries,err:=db.EntriesAfter(ctx,conversation.ID,0)
 if err!=nil{t.Fatal(err)}
 count:=0
 for _,entry:=range entries{if entry.Kind==core.EntryWorkerResult {count++;if entry.ResultID!=details.Results[0].ID{t.Fatalf("entry/result mismatch: %+v",entry)}}}
 if count!=1{t.Fatalf("canonical conversation entries=%d",count)}
}
