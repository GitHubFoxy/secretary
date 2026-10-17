package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type t11Transport struct {
	*fakeTransport
	data []byte
	downloads int
}

func (t *t11Transport) GetFile(context.Context, string) (TelegramFile, error) {
	return TelegramFile{FilePath:"photo.jpg",FileSize:int64(len(t.data))},nil
}
func (t *t11Transport) DownloadFile(_ context.Context, _ string, limit int64) ([]byte,error) {
	t.downloads++
	if int64(len(t.data)) > limit { return nil,errors.New("oversized") }
	return t.data,nil
}

type t11Server struct {
	fakeServer
	uploads int
	reject error
}

func (s *t11Server) StoreImageOriginal(_ context.Context, updateID int64, m Message, name string, body []byte) (string,error) {
	s.uploads++
	if s.reject != nil { return "",s.reject }
	if updateID == 0 || m.MessageID == 0 || name == "" || len(body) == 0 { return "",errors.New("missing image metadata") }
	return "art_t11_fixture",nil
}

func t11PNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf,image.NewRGBA(image.Rect(0,0,2,2)));err!=nil { t.Fatal(err) }
	return buf.Bytes()
}

func t11Update(id,chat,thread int64) Update {
	return Update{ID:id,Message:&Message{
		ChatID:chat,FromID:100,MessageID:9,ThreadID:thread,Caption:"What is in the picture?",
		Image:&Image{FileID:"telegram-file",FileUniqueID:"unique-image",FileName:"photo.png",MimeType:"image/png",FileSize:25},
	}}
}

func TestT11PhotoPreservesCaptionAndReplayAcrossRestart(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"adapter.json")
	transport:=&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}
	server:=&t11Server{}
	open:=func() *Adapter {
		t.Helper()
		a,err:=New(Config{StatePath:path,OwnerChatID:100},transport,server)
		if err!=nil {t.Fatal(err)}
		return a
	}
	adapter:=open()
	update:=t11Update(50,100,0)
	if err:=adapter.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if server.uploads!=1 || transport.downloads!=1 || len(server.messages)!=1 {t.Fatalf("uploads=%d downloads=%d messages=%+v",server.uploads,transport.downloads,server.messages)}
	body:=server.messages[0].Body
	if !strings.Contains(body,"What is in the picture?") || !strings.Contains(body,"artifact://art_t11_fixture") || !strings.Contains(body,"update 50, chat 100, message 9, file_unique_id unique-image") {t.Fatalf("image/caption/source missing: %q",body)}
	if err:=adapter.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if err:=open().HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if server.uploads!=1 || transport.downloads!=1 || len(server.messages)!=1 {t.Fatal("replay duplicated image processing")}
}

func TestT11OwnerAuthorizationTopicRoutingAndWarnings(t *testing.T) {
	group:=int64(-200)
	transport:=&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}
	server:=&t11Server{}
	a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state"),OwnerChatID:100,GroupChatID:&group},transport,server)
	if err!=nil {t.Fatal(err)}
	update:=t11Update(51,group,99)
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if transport.downloads!=0 {t.Fatal("unmapped group topic downloaded image")}
	a.mu.Lock()
	a.state.Topics["worker-t11"]=TopicMapping{WorkerRef:"worker-t11",ChatID:group,ThreadID:77,PendingRequestID:"request-t11"}
	if err:=a.saveLocked();err!=nil {t.Fatal(err)}
	a.mu.Unlock()
	update=t11Update(52,group,77)
	update.Message.FromID=999
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if transport.downloads!=0 {t.Fatal("unauthorized sender downloaded image")}
	update=t11Update(53,group,77)
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if len(server.workers)!=1 || server.workers[0].WorkerRef!="worker-t11" || server.workers[0].RequestID!="request-t11" || !strings.Contains(server.workers[0].Text,"artifact://art_t11_fixture") {t.Fatalf("topic did not receive image: %+v",server.workers)}
	update=t11Update(54,group,77)
	update.Message.Image.FileSize=9<<20
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	update=t11Update(55,group,77)
	update.Message.Image.FileName="unhandled.svg"
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if len(transport.sent)!=2 || transport.downloads!=1 || server.uploads!=1 {t.Fatalf("warnings not isolated: sent=%+v downloads=%d uploads=%d",transport.sent,transport.downloads,server.uploads)}
	server.reject=&ImageSourceRejection{Reason:"image: corrupt JPEG"}
	update=t11Update(56,group,77)
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	if len(server.workers)!=1 || len(transport.sent)!=3 || !strings.Contains(transport.sent[2].Text,"corrupt JPEG") {t.Fatal("invalid image fell through into text-only Worker")}
}

func TestT11BotAPIPhotoAndImageDocumentDTO(t *testing.T) {
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path!="/botTOKEN/getUpdates" {t.Errorf("unexpected %s",r.URL.Path);return}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok":true,"result":[]any{
			map[string]any{"update_id":10,"message":map[string]any{"message_id":30,"from":map[string]any{"id":100},"chat":map[string]any{"id":100},"caption":"first caption","photo":[]any{map[string]any{"file_id":"small","file_unique_id":"small-id","file_size":10,"width":2,"height":2},map[string]any{"file_id":"large","file_unique_id":"large-id","file_size":100,"width":10,"height":10}}}},
			map[string]any{"update_id":11,"message":map[string]any{"message_id":31,"from":map[string]any{"id":100},"chat":map[string]any{"id":100},"document":map[string]any{"file_id":"document","file_unique_id":"doc-id","file_name":"picture.webp","mime_type":"image/webp","file_size":150}}},
			map[string]any{"update_id":12,"message":map[string]any{"message_id":32,"from":map[string]any{"id":100},"chat":map[string]any{"id":100},"document":map[string]any{"file_id":"pdf","file_name":"info.pdf","mime_type":"application/pdf"}}},
		}})
	}))
	defer bot.Close()
	updates,err:=(&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}).GetUpdates(context.Background(),1,time.Second)
	if err!=nil || len(updates)!=3 {t.Fatalf("updates=%+v err=%v",updates,err)}
	if image:=updates[0].Message.Image;image==nil || image.FileID!="large" || image.FileUniqueID!="large-id" || updates[0].Message.Caption!="first caption" || image.MimeType!="image/jpeg" {t.Fatalf("photo decode=%+v",updates[0])}
	if image:=updates[1].Message.Image;image==nil || image.FileName!="picture.webp" || image.MimeType!="image/webp" {t.Fatalf("document decode=%+v",updates[1])}
	if updates[2].Message.Image!=nil || updates[2].Message.Voice!=nil {t.Fatalf("non-image document ingested: %+v",updates[2])}
}
