package ctl

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/node"
)

type t12LocalCountingRuntime struct{ starts atomic.Int32 }
func (r *t12LocalCountingRuntime) Start(_ context.Context,req node.StartRequest)(node.Session,error){
 r.starts.Add(1)
 a:=make(chan node.Activity);close(a)
 b:=make(chan node.Result);close(b)
 return &t12LocalCountingSession{id:"native-"+req.WorkerRef,activity:a,results:b},nil
}
type t12LocalCountingSession struct{
 id string
 activity chan node.Activity
 results chan node.Result
}
func (s *t12LocalCountingSession) ID()string{return s.id}
func (s *t12LocalCountingSession) Prompt(context.Context,string)error{return nil}
func (s *t12LocalCountingSession) Steer(context.Context,string)(bool,error){return true,nil}
func (s *t12LocalCountingSession) Cancel(context.Context)error{return nil}
func (s *t12LocalCountingSession) Activity()<-chan node.Activity{return s.activity}
func (s *t12LocalCountingSession) Result()<-chan node.Result{return s.results}
func (s *t12LocalCountingSession) Close()error{return nil}

func TestT12LocalCommandCancelBeforeArtifactReadyIsDurableAndNoNativeStart(t *testing.T){
 ctx,cancel:=context.WithTimeout(context.Background(),9*time.Second)
 defer cancel()
 runtime:=&t12LocalCountingRuntime{}
 local:=node.NewLocal(runtime)
 statePath:=filepath.Join(t.TempDir(),"node-state.json")
 store,err:=node.OpenLocalStore(statePath)
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 local.SetPrestartStore(store)
 id:="art_"+strings.Repeat("f",32)
 data:=[]byte("video queued")
 sha:=sha256.Sum256(data)
 entered:=make(chan struct{},1)
 ready:=make(chan struct{})
 local.SetArtifactFetcher(func(_ context.Context,key string)(node.ArtifactPayload,error){
  if key!=id{return node.ArtifactPayload{},errors.New("wrong ID")}
  return node.ArtifactPayload{ID:id,Name:"clip.mp4",SHA256:hex.EncodeToString(sha[:]),Data:data,VideoStatus:"queued",
   CheckVideoStatus:func(c context.Context)(core.Artifact,error){
    select{case entered<-struct{}{}:default:}
    select{case <-ready:return core.Artifact{ID:id,ProcessingStatus:"ready"},nil
    case <-c.Done():return core.Artifact{},c.Err()}
   }},nil
 })
 harness:=core.HarnessInstance{ID:"local/fx",Node:"local",Kind:core.HarnessFX,Version:"test",Status:core.HarnessReady,
  Authentication:core.HarnessAuthentication{Authenticated:true},
  Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityCancel,core.CapabilityImageInput}}}
 meta:=core.CommandMetadata{CommandID:"dispatch-local-queued",Node:"local",WorkerRef:"local-queued-worker",
  TurnID:"turn-local",AttemptID:"attempt-local",HarnessInstanceID:harness.ID,IssuedAt:time.Now().UTC()}
 envelope:=node.WorkerEnvelope{WorkerRef:meta.WorkerRef,TurnID:meta.TurnID,AttemptID:meta.AttemptID,
  OriginalUserIntent:"Analyze artifact://"+id,HarnessInstance:harness,LegacyWorkerTemplate:true}
 dispatch:=node.Command{Kind:node.CommandDispatch,Dispatch:&node.DispatchCommand{Metadata:meta,Envelope:envelope}}
 rt:=NodeRuntime{Local:local}
 dispatched:=make(chan error,1)
 go func(){_,e:=rt.LocalCommand(ctx,dispatch);dispatched<-e}()
 select{case <-entered:case <-ctx.Done():t.Fatal("local Worker never waited for conversion")}
 cancelMeta:=meta;cancelMeta.CommandID="cancel-local-before-handoff";cancelMeta.IssuedAt=time.Now().UTC()
 cmd:=node.Command{Kind:node.CommandCancel,Cancel:&node.CancelCommand{Metadata:cancelMeta,Reason:"owner cancelled"}}
 started:=time.Now()
 canceled,err:=rt.LocalCommand(ctx,cmd)
 if err!=nil||canceled.State!=node.CommandAccepted{
  t.Fatalf("LocalCommand did not accept pre-native cancel: %+v err=%v",canceled,err)
 }
 if time.Since(started)>time.Second{t.Fatal("cancel blocked behind media polling")}
 select{case e:=<-dispatched:
  if !errors.Is(e,context.Canceled)&&!errors.Is(e,node.ErrLocalPrestartCanceled){t.Fatalf("dispatch failed incorrectly: %v",e)}
 case <-ctx.Done():t.Fatal("pending local dispatch not stopped")}
 close(ready)
 if runtime.starts.Load()!=0{t.Fatalf("canceled native Worker started late: %d",runtime.starts.Load())}
 persisted,err:=node.OpenLocalStore(statePath)
 if err!=nil{t.Fatal(err)}
 if persisted.LocalPrestartState(meta.AttemptID)!="canceled"{
  t.Fatal("cancellation checkpoint not durable across Node state reload")
 }
 err=persisted.BeginLocalPrestart(meta.AttemptID,meta.WorkerRef)
 if !errors.Is(err,node.ErrLocalPrestartCanceled){t.Fatalf("old canceled Attempt accepted after restart: %v",err)}
}
