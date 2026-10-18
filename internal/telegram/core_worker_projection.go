package telegram

import (
 "encoding/json"
 "strings"
 "github.com/beruseruko/secretary/internal/core"
)

// ProjectCoreWorkerEvent maps Server-issued Worker lifecycle records to
// canonical Telegram messages. Both production bridge and E2E use it.
func ProjectCoreWorkerEvent(source core.Event)(Event,bool){
 if source.Kind!="worker.spawned"&&source.Kind!="attempt.outcome_recorded"&&source.Kind!="result.accepted"{return Event{},false}
 var p map[string]any
 _=json.Unmarshal(source.Payload,&p)
 ref:=source.WorkerRef
 if ref==""&&source.AggregateType=="worker"{ref=source.AggregateID}
 get:=func(k string)string{v,_:=p[k].(string);return strings.TrimSpace(v)}
 out:=Event{EventID:source.ID,Sequence:source.Seq,WorkerRef:ref,Payload:source.Payload,
  AttemptID:source.AttemptID,Title:get("title")}
 if out.AttemptID=="" {
  out.AttemptID=get("attempt_id")
  if out.AttemptID=="" && source.Kind=="attempt.outcome_recorded" && source.AggregateType=="attempt"{
   out.AttemptID=source.AggregateID
  }
  if out.AttemptID=="" {
   if meta,ok:=p["metadata"].(map[string]any);ok {
    out.AttemptID,_=meta["attempt_id"].(string)
   }
  }
 }
 if source.Kind=="worker.spawned"{
  out.Kind="worker.created";out.TaskPrompt=get("intent");out.NodeID=get("node_id")
  return out,true
 }
 out.WorkerAttemptTerminal=true
 message:=get("text")
 if message==""{message=get("summary")}
 if message==""{message=get("result")}
 if message==""{message=get("error")}
 if message==""{message=get("response")}
 if source.Kind=="attempt.outcome_recorded"&&message==""{return out,true}
 if source.Kind=="attempt.outcome_recorded"&&get("classification")=="retryable"{return out,true}
 out.Kind="worker.completed"
 out.Text=message
 status:=strings.ToLower(get("status"))
 if strings.Contains(status,"cancel"){out.Kind="worker.canceled"}else if strings.Contains(status,"offline")||strings.Contains(status,"interrupt"){out.Kind="worker.offline"}else if strings.Contains(status,"fail")||strings.Contains(status,"error"){out.Kind="worker.failed"}
 id:=strings.TrimSpace(source.CorrelationID)
 if id!=""{out.TerminalIdentity="turn:"+id}else if turnID:=get("turn_id");turnID!=""{out.TerminalIdentity="turn:"+turnID}else if resultID:=get("id");resultID!=""{out.TerminalIdentity="result:"+resultID}else if source.AggregateID!=""{out.TerminalIdentity=source.AggregateType+":"+source.AggregateID}
 out.WorkerAttemptTerminal=true
 return out,true
}
