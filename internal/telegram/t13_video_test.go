package telegram

import (
 "bytes"
 "context"
 "errors"
 "path/filepath"
 "strings"
 "sync"
 "testing"
)

type t13FixtureServer struct {
 fakeServer
 mu sync.Mutex
 uploads int
 originals [][]byte
 status string
 reason string
}
func (s *t13FixtureServer) StoreVideoOriginal(_ context.Context,_ int64,_ Message,_ string,data []byte)(string,error){
 s.mu.Lock();defer s.mu.Unlock()
 s.uploads++
 s.originals=append(s.originals,append([]byte(nil),data...))
 return "art_t13_fixture",nil
}
func (s *t13FixtureServer) VideoStatus(context.Context,string)(string,string,error){return s.status,s.reason,nil}

func t13Update(id int64) Update {
 return Update{ID:id,Message:&Message{ChatID:100,FromID:100,MessageID:id,Caption:"Track the action",
 Video:&Video{FileID:"telegram-video",FileUniqueID:"video-unique",FileName:"clip.mp4",MimeType:"video/mp4",FileSize:5,Duration:2}}}
}

func TestT13VideoAndVideoNoteBotDecode(t *testing.T) {
 for _,tt:=range []struct{name string;note bool}{{"video",false},{"video_note",true}}{
  t.Run(tt.name,func(t *testing.T){
   dto:=botUpdateDTO{ID:12,Message:&botMessageDTO{MessageID:4,Caption:"Video caption"}}
   if tt.note {dto.Message.VideoNote=&botVoiceDTO{FileID:"note",FileUniqueID:"unique",Duration:2,FileSize:15}} else {dto.Message.Video=&botVoiceDTO{FileID:"ordinary",FileUniqueID:"unique",Duration:2,FileSize:15,FileName:"clip.mp4",MimeType:"video/mp4"}}
   u:=dto.update()
   if u.Message.Video==nil||u.Message.Video.FileUniqueID!="unique"||u.Message.Caption!="Video caption"||!telegramVideoName(u.Message.Video.FileName){t.Fatalf("video update: %+v",u)}
  })
 }
}

func TestT13VideoPreservesOriginalCaptionAndIdempotentRestart(t *testing.T) {
 original:=[]byte("video original bytes")
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:original}
 srv:=&t13FixtureServer{status:"ready"}
 state:=filepath.Join(t.TempDir(),"state.json")
 open:=func()*Adapter{t.Helper();a,err:=New(Config{StatePath:state,OwnerChatID:100},tr,srv);if err!=nil{t.Fatal(err)};return a}
 u:=t13Update(400)
 u.Message.Video.FileSize=int64(len(original))
 adapter:=open()
 if err:=adapter.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 adapter.imageWG.Wait()
 if srv.uploads!=1||len(srv.originals)!=1||!bytes.Equal(srv.originals[0],original)||len(srv.messages)!=1 {t.Fatalf("source lost/duplicated: uploads=%d messages=%+v",srv.uploads,srv.messages)}
 body:=srv.messages[0].Body
 if !strings.Contains(body,"Track the action")||!strings.Contains(body,"artifact://art_t13_fixture")||!strings.Contains(body,"chat 100, message 400")||!strings.Contains(body,"video-unique"){t.Fatalf("source/caption mismatch: %q",body)}
 if err:=adapter.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 if err:=open().HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 if srv.uploads!=1||len(srv.messages)!=1||tr.downloads!=1{t.Fatalf("replayed video: uploads=%d messages=%d downloads=%d",srv.uploads,len(srv.messages),tr.downloads)}
}

func TestT13VideoProcessingFailedNoDispatch(t *testing.T){
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:[]byte("video")}
 srv:=&t13FixtureServer{status:"failed",reason:"ffprobe invalid container"}
 a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},tr,srv)
 if err!=nil{t.Fatal(err)}
 u:=t13Update(401)
 if err:=a.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if len(srv.messages)!=0||len(tr.sent)!=1||!strings.Contains(tr.sent[0].Text,"ffprobe invalid container"){t.Fatalf("failure dispatched or silent: sent=%+v messages=%+v",tr.sent,srv.messages)}
 if err:=a.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if len(tr.sent)!=1||srv.uploads!=1{t.Fatalf("failure replay duplicated: sent=%d uploads=%d",len(tr.sent),srv.uploads)}
}

func TestT13OversizeAndUnauthorizedNeverDownload(t *testing.T){
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:[]byte("video")}
 srv:=&t13FixtureServer{status:"ready"}
 a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},tr,srv)
 if err!=nil{t.Fatal(err)}
 unauthorized:=t13Update(402);unauthorized.Message.FromID=999
 if err:=a.HandleUpdate(context.Background(),unauthorized);err!=nil{t.Fatal(err)}
 tooLarge:=t13Update(403);tooLarge.Message.Video.FileSize=9<<20
 if err:=a.HandleUpdate(context.Background(),tooLarge);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if tr.downloads!=0||srv.uploads!=0||len(srv.messages)!=0||len(tr.sent)!=1{t.Fatalf("unauthorized/oversize media accessed: downloads=%d uploads=%d notices=%+v",tr.downloads,srv.uploads,tr.sent)}
}

func TestT13VideoSourceRejectionIsTerminal(t *testing.T){
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:[]byte("video")}
 srv:=&t13RejectServer{}
 a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},tr,srv);if err!=nil{t.Fatal(err)}
 u:=t13Update(404)
 if err:=a.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if len(tr.sent)!=1||!strings.Contains(tr.sent[0].Text,"invalid video") {t.Fatalf("missing terminal warning: %+v",tr.sent)}
 if err:=a.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 if len(tr.sent)!=1{t.Fatal("duplicate terminal warning")}
}

type t13RejectServer struct{fakeServer}
func (*t13RejectServer) StoreVideoOriginal(context.Context,int64,Message,string,[]byte)(string,error){return "",&ImageSourceRejection{Reason:"invalid video"}}
func (*t13RejectServer) VideoStatus(context.Context,string)(string,string,error){return "","",errors.New("unexpected status request")}
