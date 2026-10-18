package telegram

import(
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "path/filepath"
 "strings"
 "testing"
 "time"
 "sync"
)

type t15Store struct {
 fakeServer
 originals [][]byte
 uploads int
}
func(s *t15Store)StoreDocumentOriginal(_ context.Context,_ int64,_ Message,_ string,data []byte)(string,error){
 s.uploads++
 s.originals=append(s.originals,append([]byte(nil),data...))
 return "art_t15_fixture",nil
}

func TestT15TelegramDocumentIntakeReplayAndRestart(t *testing.T){
 original:=[]byte("document bytes and contents")
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:original}
 srv:=&t15Store{}
 path:=filepath.Join(t.TempDir(),"telegram.json")
 open:=func()*Adapter{
  t.Helper();a,e:=New(Config{StatePath:path,OwnerChatID:100},tr,srv);if e!=nil{t.Fatal(e)}
  return a
 }
 update:=Update{ID:900,Message:&Message{ChatID:100,FromID:100,MessageID:15,Caption:"Read the report",
  Document:&Document{FileID:"doc-id",FileUniqueID:"unique-report",FileName:"report.pdf",MimeType:"application/pdf",FileSize:int64(len(original))}}}
 a:=open()
 if err:=a.HandleUpdate(context.Background(),update);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if srv.uploads!=1||len(srv.messages)!=1||!bytes.Equal(srv.originals[0],original){t.Fatalf("document ingress lost: uploads=%d messages=%+v",srv.uploads,srv.messages)}
 body:=srv.messages[0].Body
 for _,value:=range []string{"Read the report","report.pdf","application/pdf","unique-report","artifact://art_t15_fixture"}{
  if !strings.Contains(body,value){t.Fatalf("missing %q: %s",value,body)}
 }
 if err:=a.HandleUpdate(context.Background(),update);err!=nil{t.Fatal(err)}
 if err:=open().HandleUpdate(context.Background(),update);err!=nil{t.Fatal(err)}
 if srv.uploads!=1||len(srv.messages)!=1||tr.downloads!=1{t.Fatal("Telegram duplicate replay dispatched twice")}
}

func TestT15TelegramDocumentRejectsUnauthorizedOversize(t *testing.T){
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:[]byte("data")}
 srv:=&t15Store{}
 a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"telegram.json"),OwnerChatID:100},tr,srv)
 if err!=nil{t.Fatal(err)}
 mk:=func(id int64)*Message{return &Message{ChatID:100,FromID:100,MessageID:id,Document:&Document{FileID:"document",FileName:"report.pdf",FileSize:4}}}
 denied:=mk(50);denied.FromID=99
 if err:=a.HandleUpdate(context.Background(),Update{ID:50,Message:denied});err!=nil{t.Fatal(err)}
 oversize:=mk(51);oversize.Document.FileSize=9<<20
 if err:=a.HandleUpdate(context.Background(),Update{ID:51,Message:oversize});err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if tr.downloads!=0||srv.uploads!=0||len(srv.messages)!=0||len(tr.sent)!=1{t.Fatalf("invalid document accessed: %+v",tr.sent)}
}

type t15DeliveryServer struct{
 fakeServer
 bytes []byte
}
func(s *t15DeliveryServer)DownloadDocument(context.Context,string)(string,string,[]byte,error){
 return "result.txt","text/plain",s.bytes,nil
}
type t15DeliveryTransport struct{
 *fakeTransport
 sentDocs []OutgoingMessage
 names []string
 originals [][]byte
}
func(t *t15DeliveryTransport)SendDocument(_ context.Context,chat,thread int64,name,mime string,data []byte,caption string)error{
 t.sentDocs=append(t.sentDocs,OutgoingMessage{ChatID:chat,ThreadID:thread,Text:caption})
 t.names=append(t.names,name)
 t.originals=append(t.originals,append([]byte(nil),data...))
 return nil
}

func TestT15WorkerPublishedResultFileOutboxReplay(t *testing.T){
 tr:=&t15DeliveryTransport{fakeTransport:&fakeTransport{}}
 srv:=&t15DeliveryServer{bytes:[]byte("worker output")}
 a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"telegram.json"),OwnerChatID:100},tr,srv)
 if err!=nil{t.Fatal(err)}
 payload,_:=json.Marshal(map[string]any{"id":"res_t15_fixture","artifact_refs":"artifact://art_t15fixture"})
 event:=Event{EventID:"event-1",Sequence:10,Kind:"worker.completed",WorkerRef:"worker-file",Title:"File Worker",
  Text:"Generated report",TerminalIdentity:"turn:t15",Payload:payload}
 if err:=a.HandleDurableEvent(context.Background(),event);err!=nil{t.Fatal(err)}
 if err:=a.HandleDurableEvent(context.Background(),event);err!=nil{t.Fatal(err)}
 if len(tr.sentDocs)!=1||tr.sentDocs[0].ChatID!=100||tr.names[0]!="result.txt"||!bytes.Equal(tr.originals[0],srv.bytes){
  t.Fatalf("Worker file delivery missing/duplicate: %+v",tr.sentDocs)
 }
}

func TestT15WorkerDocumentResultAllTelegramProjectionsReconnect(t *testing.T){
 ctx:=context.Background()
 group:=int64(-200)
 tr:=&t15DeliveryTransport{fakeTransport:&fakeTransport{}}
 srv:=&t15DeliveryServer{bytes:[]byte("published document")}
 path:=filepath.Join(t.TempDir(),"telegram.json")
 open:=func()*Adapter{
  t.Helper()
  a,e:=New(Config{StatePath:path,OwnerChatID:100,GroupChatID:&group},tr,srv)
  if e!=nil{t.Fatal(e)}
  return a
 }
 a:=open()
 a.mu.Lock()
 a.state.OwnerChat=100
 a.state.GroupChat=group
 a.state.Topics["worker-export"]=TopicMapping{WorkerRef:"worker-export",ChatID:group,ThreadID:77,Title:"Worker export"}
 err:=a.saveLocked()
 a.mu.Unlock()
 if err!=nil{t.Fatal(err)}
 payload,_:=json.Marshal(map[string]any{
  "id":"res_t15_export","artifact_refs":"artifact://art_exported artifact://art_exported",
 })
 event:=Event{EventID:"result-event",Sequence:120,Kind:"worker.completed",WorkerRef:"worker-export",
  Title:"Worker export",Text:"Report finished",TerminalIdentity:"turn:t15-export",Payload:payload}
 if err:=a.HandleDurableEvent(ctx,event);err!=nil{t.Fatal(err)}
 if err:=a.SyncWorkerTopics(ctx);err!=nil{t.Fatal(err)}
 if err:=open().HandleDurableEvent(ctx,event);err!=nil{t.Fatal(err)}
 if len(tr.sentDocs)!=3{t.Fatalf("expected DM, General and Worker Topic once, got %+v",tr.sentDocs)}
 counts:=map[string]int{}
 for _,m:=range tr.sentDocs {
  counts[fmt.Sprintf("%d:%d",m.ChatID,m.ThreadID)]++
 }
 if counts["100:0"]!=1||counts["-200:0"]!=1||counts["-200:77"]!=1{t.Fatalf("missing or duplicate Worker Artifact projection: %+v",counts)}
}

type t15PendingDispatchStore struct {
 t15Store
 started chan struct{}
 unblock chan struct{}
 once sync.Once
}
func(s *t15PendingDispatchStore)SendWorkerMessage(ctx context.Context,m WorkerMessage)error{
 s.once.Do(func(){close(s.started)})
 select{
 case <-ctx.Done():return ctx.Err()
 case <-s.unblock:
 }
 return s.fakeServer.SendWorkerMessage(ctx,m)
}
func TestT15DocumentTopicPendingDispatchRestartsWithoutDuplicate(t *testing.T){
 group:=int64(-200)
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:[]byte("document payload")}
 srv:=&t15PendingDispatchStore{started:make(chan struct{}),unblock:make(chan struct{})}
 path:=filepath.Join(t.TempDir(),"pending.json")
 open:=func()*Adapter{
  t.Helper();a,e:=New(Config{StatePath:path,OwnerChatID:100,GroupChatID:&group},tr,srv)
  if e!=nil{t.Fatal(e)};return a
 }
 first:=open()
 first.mu.Lock()
 first.state.OwnerUser=100
 first.state.GroupChat=group
 first.state.Topics["worker-document"]=TopicMapping{
  WorkerRef:"worker-document",ChatID:group,ThreadID:97,PendingRequestID:"document-request",
 }
 err:=first.saveLocked()
 first.mu.Unlock()
 if err!=nil{t.Fatal(err)}
 u:=Update{ID:922,Message:&Message{
  ChatID:group,ChatType:"supergroup",FromID:100,MessageID:922,ThreadID:97,
  Caption:"Read document",Document:&Document{
   FileID:"doc",FileUniqueID:"doc-unique",FileName:"data.json",MimeType:"application/json",FileSize:16,
  },
 }}
 ctx,cancel:=context.WithCancel(context.Background())
 if err:=first.HandleUpdate(ctx,u);err!=nil{t.Fatal(err)}
 select{case <-srv.started:case <-time.After(3*time.Second):t.Fatal("document dispatch never reached Worker")}
 if err:=first.HandleUpdate(ctx,u);err!=nil{t.Fatal(err)}
 first.mu.Lock();pending:=len(first.state.PendingImages);first.mu.Unlock()
 if pending!=1{t.Fatalf("pending document not durable: %d",pending)}
 cancel()
 first.imageWG.Wait()
 restarted:=open()
 close(srv.unblock)
 restarted.startImageWorkers(context.Background())
 restarted.imageWG.Wait()
 if len(srv.workers)!=1||srv.workers[0].WorkerRef!="worker-document"||srv.workers[0].RequestID!="document-request"{
  t.Fatalf("document was not recovered to original Worker and request: %+v",srv.workers)
 }
 if err:=restarted.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 restarted.imageWG.Wait()
 if len(srv.workers)!=1{t.Fatal("duplicate Telegram update redelivered document")}
}
