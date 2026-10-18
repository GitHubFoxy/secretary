package telegram

import (
 "context"
 "github.com/beruseruko/secretary/internal/core"
)

// RelayCoreEventsOnce is the production durable Server-to-Telegram replay
// operation; callers provide their event projection but not the replay loop.
func RelayCoreEventsOnce(ctx context.Context, store *core.Store, adapter *Adapter, project func(core.Event)Event)(int64,error){
 cursor:=adapter.LastEventSeq()
 for{
  events,err:=store.EventsAfterSeq(ctx,cursor,500)
  if err!=nil{return 0,err}
  if len(events)==0{return 0,nil}
  for _,event:=range events{
   if err:=adapter.HandleDurableEvent(ctx,project(event));err!=nil{return event.Seq,err}
   if event.Kind==core.SecretaryTurnFinishedEvent{
    if err:=adapter.Flush(ctx);err!=nil{return event.Seq,err}
   }
   cursor=event.Seq
  }
  if len(events)<500{return 0,nil}
 }
}
