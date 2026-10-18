package telegram

import(
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestT15BotAPISendDocumentMultipartPreservesBytesAndTopic(t *testing.T){
 original:=[]byte("published worker report contents")
 delivered:=0
 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/botTOKEN/sendDocument"||r.Method!=http.MethodPost{
   t.Errorf("unexpected Telegram outbound endpoint %s",r.URL.Path);http.NotFound(w,r);return
  }
  delivered++
  if err:=r.ParseMultipartForm(9<<20);err!=nil{t.Errorf("multipart parse: %v",err);http.Error(w,"invalid",400);return}
  if r.FormValue("chat_id")!="-200"||r.FormValue("message_thread_id")!="77"||r.FormValue("caption")!="Worker published report"{
   t.Errorf("lost topic or caption: %+v",r.Form)
  }
  f,head,err:=r.FormFile("document")
  if err!=nil{t.Errorf("document part missing: %v",err);http.Error(w,"missing",400);return}
  defer f.Close()
  body,err:=io.ReadAll(f)
  if err!=nil||!bytes.Equal(body,original)||head.Filename!="report.pdf"{
   t.Errorf("uploaded document changed: name=%q err=%v",head.Filename,err)
  }
  _,_=fmt.Fprint(w,`{"ok":true,"result":{"message_id":10}}`)
 }))
 defer bot.Close()
 sender:=&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 if err:=sender.SendDocument(context.Background(),-200,77,"report.pdf","application/pdf",original,"Worker published report");err!=nil{t.Fatal(err)}
 if delivered!=1{t.Fatalf("Telegram sendDocument calls=%d",delivered)}
}
func TestT15BotAPIRejectsFailedDocumentSend(t *testing.T){
 bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  w.WriteHeader(http.StatusBadRequest)
  _,_=fmt.Fprint(w,`{"ok":false,"description":"Bad Request"}`)
 }))
 defer bot.Close()
 sender:=&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
 if err:=sender.SendDocument(context.Background(),100,0,"report.pdf","application/pdf",[]byte("data"),"");err==nil||!strings.Contains(err.Error(),"400"){
  t.Fatalf("Telegram rejection silently accepted: %v",err)
 }
 _=json.Valid([]byte("{}"))
}
