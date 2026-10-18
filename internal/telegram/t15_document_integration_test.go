package telegram

import(
 "bytes"
 "context"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"
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
 payload,_:=json.Marshal(map[string]any{"id":"res_t15_fixture","artifact_refs":[]string{"artifact://art_t15_fixture"}})
 event:=Event{EventID:"event-1",Sequence:10,Kind:"worker.completed",WorkerRef:"worker-file",Title:"File Worker",
  Text:"Generated report",TerminalIdentity:"turn:t15",Payload:payload}
 if err:=a.HandleDurableEvent(context.Background(),event);err!=nil{t.Fatal(err)}
 if err:=a.HandleDurableEvent(context.Background(),event);err!=nil{t.Fatal(err)}
 if len(tr.sentDocs)!=1||tr.sentDocs[0].ChatID!=100||tr.names[0]!="result.txt"||!bytes.Equal(tr.originals[0],srv.bytes){
  t.Fatalf("Worker file delivery missing/duplicate: %+v",tr.sentDocs)
 }
}
