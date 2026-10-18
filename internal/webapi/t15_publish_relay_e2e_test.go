package webapi

import (
 "bytes"
 "context"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "testing"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/telegram"
)

func t15VerifyPublishedDocumentRelay(t *testing.T,ctx context.Context,api *Server,store *core.Store,serverURL,workerRef,artifactID string,original []byte){
 t.Helper()
 api.AttachInternalCredential("t15-published-secret")
 type delivered struct{chat,thread,name string;body []byte}
 results:=make([]delivered,0,3)
 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  switch r.URL.Path{
  case "/botTOKEN/createForumTopic":
   _,_=io.WriteString(w,`{"ok":true,"result":{"message_thread_id":77,"name":"Worker files"}}`)
  case "/botTOKEN/sendChatAction","/botTOKEN/sendMessage":
   _,_=io.WriteString(w,`{"ok":true,"result":true}`)
  case "/botTOKEN/sendDocument":
   if err:=r.ParseMultipartForm(9<<20);err!=nil{t.Errorf("multipart: %v",err);http.Error(w,"bad",400);return}
   f,header,err:=r.FormFile("document")
   if err!=nil{t.Errorf("Telegram file missing: %v",err);http.Error(w,"no file",400);return}
   body,err:=io.ReadAll(f);_ = f.Close()
   if err!=nil{t.Errorf("Telegram document: %v",err)}
   if !bytes.Equal(body,original){t.Errorf("published Worker bytes changed")}
   results=append(results,delivered{chat:r.FormValue("chat_id"),thread:r.FormValue("message_thread_id"),name:header.Filename,body:body})
   _,_=io.WriteString(w,`{"ok":true,"result":{"message_id":900}}`)
  default:
   t.Errorf("unexpected Bot API call %s",r.URL.Path);http.NotFound(w,r)
  }
 }))
 defer bot.Close()
 group:=int64(-200)
 state:=filepath.Join(t.TempDir(),"telegram.json")
 tr:=&telegram.BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 open:=func()*telegram.Adapter{
  t.Helper()
  a,err:=telegram.New(telegram.Config{StatePath:state,OwnerChatID:100,GroupChatID:&group},tr,
   &telegram.HTTPServerClient{BaseURL:serverURL,Credential:"t15-published-secret"})
  if err!=nil{t.Fatal(err)}
  return a
 }
 adapter:=open()
 // Pair the optional General forum through the real Telegram adapter path.
 // Configuring GroupChatID alone does not authorize or link that forum.
 if err:=adapter.HandleUpdate(ctx,telegram.Update{ID:710,Message:&telegram.Message{
  MessageID:710,ChatID:-200,ChatType:"supergroup",IsForum:true,FromID:100,
  Text:"/linkgroup",
 }});err!=nil{t.Fatal(err)}
 project:=func(source core.Event)telegram.Event{
  if event,ok:=telegram.ProjectCoreWorkerEvent(source);ok{return event}
  return telegram.Event{EventID:source.ID,Sequence:source.Seq}
 }
 // The real T09 worker has already published bytes using its native
 // authenticated Node broker and completed a persisted Result.
 if _,err:=telegram.RelayCoreEventsOnce(ctx,store,adapter,project);err!=nil{t.Fatal(err)}
 if err:=adapter.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 if _,err:=telegram.RelayCoreEventsOnce(ctx,store,adapter,project);err!=nil{t.Fatal(err)}
 if err:=adapter.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 if len(results)!=3{t.Fatalf("T09 -> T15 production relay delivered %d files instead of DM+General+Worker Topic: %+v",len(results),results)}
 seen:=map[string]int{}
 for _,del:=range results{
  seen[del.chat+":"+del.thread]++
  if del.name!="result.zip" || !bytes.Equal(del.body,original){t.Fatalf("wrong filename/original bytes: %+v",del)}
 }
 if seen["100:"]!=1||seen["-200:"]!=1||seen["-200:77"]!=1{t.Fatalf("Worker %s artifact %s projections incorrect: %+v",workerRef,artifactID,seen)}
 // A new adapter process replays the same source event cursor; no duplicate
 // Bot API upload is allowed.
 reconnected:=open()
 if _,err:=telegram.RelayCoreEventsOnce(ctx,store,reconnected,project);err!=nil{t.Fatal(err)}
 if err:=reconnected.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 if len(results)!=3{t.Fatalf("reconnect resent %d files",len(results)-3)}
 _=fmt.Sprint(workerRef,artifactID)
}
