package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type t11BlockedDownload struct {
	*fakeTransport
	entered chan struct{}
	unblock chan struct{}
	data []byte
	once sync.Once
}

func (tr *t11BlockedDownload) GetFile(context.Context,string)(TelegramFile,error) {
	return TelegramFile{FilePath:"photo.png",FileSize:int64(len(tr.data))},nil
}

func (tr *t11BlockedDownload) DownloadFile(ctx context.Context,_ string,_ int64)([]byte,error) {
	tr.once.Do(func(){close(tr.entered)})
	select{
	case <-tr.unblock:return tr.data,nil
	case <-ctx.Done():return nil,ctx.Err()
	}
}

type t11SignalServer struct {
	mu sync.Mutex
	images int
	texts []string
	delivered chan string
}
func (srv *t11SignalServer) SendMessage(_ context.Context,m InboundMessage) error {
	srv.mu.Lock()
	srv.texts=append(srv.texts,m.Body)
	srv.mu.Unlock()
	select{case srv.delivered<-m.Body:default:}
	return nil
}
func (srv *t11SignalServer) SendWorkerMessage(context.Context,WorkerMessage) error {return nil}
func (srv *t11SignalServer) ResumeWorker(context.Context,string)error{return nil}
func (srv *t11SignalServer) StoreImageOriginal(_ context.Context,_ int64,_ Message,_ string,_ []byte)(string,error){
	srv.mu.Lock()
	srv.images++
	srv.mu.Unlock()
	return "art_t11_async",nil
}

func TestT11DurableImageIngestionDoesNotBlockFollowingDMText(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"telegram-state.json")
	tr:=&t11BlockedDownload{fakeTransport:&fakeTransport{},entered:make(chan struct{}),unblock:make(chan struct{}),data:t11PNG(t)}
	srv:=&t11SignalServer{delivered:make(chan string,4)}
	newAdapter:=func()*Adapter{
		t.Helper()
		a,err:=New(Config{StatePath:path,OwnerChatID:100},tr,srv)
		if err!=nil {t.Fatal(err)}
		return a
	}
	a:=newAdapter()
	photo:=t11Update(800,100,0)
	if err:=a.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
	select {case <-tr.entered:case <-time.After(2*time.Second):t.Fatal("image download did not start")}
	text:=Update{ID:801,Message:&Message{ChatID:100,FromID:100,MessageID:801,Text:"independent text while image downloads"}}
	if err:=a.HandleUpdate(context.Background(),text);err!=nil {t.Fatal(err)}
	select {
	case body:=<-srv.delivered:
		if body!=text.Message.Text {t.Fatalf("text overtaken by blocked image: %q",body)}
	case <-time.After(time.Second):
		t.Fatal("dependent lane blocked unrelated text while Bot API was slow")
	}
	a.mu.Lock()
	queued:=len(a.state.PendingImages)
	a.mu.Unlock()
	if queued!=1 {t.Fatalf("image was not checkpointed: %d",queued)}
	close(tr.unblock)
	a.imageWG.Wait()
	select {
	case body:=<-srv.delivered:
		if !strings.Contains(body,"artifact://art_t11_async"){t.Fatalf("image not delivered after download: %q",body)}
	case <-time.After(time.Second):t.Fatal("image failed to finish")}
	a.mu.Lock()
	queued=len(a.state.PendingImages)
	a.mu.Unlock()
	if queued!=0 {t.Fatal("completed image still pending")}
	restarted:=newAdapter()
	if err:=restarted.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
	restarted.imageWG.Wait()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.images!=1 || len(srv.texts)!=2 {t.Fatalf("replay duplicate processing: images=%d texts=%v",srv.images,srv.texts)}
}

func TestT11UnknownFileSizeOversizeHasOneNoticeAndNoLaneRetry(t *testing.T) {
	const token="fixture"
	huge:=bytes.Repeat([]byte("x"),core.MaxArtifactBytes+1)
	var mu sync.Mutex
	notices:=[]string{}
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path{
		case "/botfixture/getFile":
			_,_=fmt.Fprint(w,`{"ok":true,"result":{"file_path":"unknown.jpg","file_size":0}}`)
		case "/file/botfixture/unknown.jpg":
			_,_=w.Write(huge)
		case "/botfixture/sendMessage":
			_ = r.ParseForm()
			mu.Lock()
			notices=append(notices,r.Form.Get("text"))
			mu.Unlock()
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		case "/botfixture/sendChatAction":
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:
			http.Error(w,"unsupported",http.StatusNotFound)
		}
	}))
	defer bot.Close()
	path:=filepath.Join(t.TempDir(),"adapter.json")
	client:=&BotAPITransport{BaseURL:bot.URL,BotToken:token}
	server:=&t11SignalServer{delivered:make(chan string,3)}
	a,err:=New(Config{StatePath:path,OwnerChatID:100},client,server)
	if err!=nil {t.Fatal(err)}
	photo:=t11Update(910,100,0)
	photo.Message.Image.FileSize=0
	photo.Message.Image.FileName="unknown.jpg"
	if err:=a.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	if err:=a.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	mu.Lock()
	if len(notices)!=1 || !strings.Contains(notices[0],"8 МиБ") {t.Fatalf("no idempotent over-limit notice: %+v",notices)}
	mu.Unlock()
	if err:=a.HandleUpdate(context.Background(),Update{ID:911,Message:&Message{ChatID:100,FromID:100,MessageID:911,Text:"following text"}});err!=nil {t.Fatal(err)}
	select{case body:=<-server.delivered:
		if body!="following text"{t.Fatalf("oversized image reached Worker: %q",body)}
	case <-time.After(time.Second):t.Fatal("lane stayed blocked after oversize")}
}

func TestT11MissingDocumentNameUsesVerifiedImageFormat(t *testing.T) {
	tr:=&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}
	srv:=&t11FileNameServer{}
	a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state"),OwnerChatID:100},tr,srv)
	if err!=nil {t.Fatal(err)}
	update:=t11Update(940,100,0)
	update.Message.Image.FileName=""
	update.Message.Image.MimeType="image/png"
	if err:=a.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	if srv.fileName!="telegram-image-940-9.png" {t.Fatalf("unsafe or missing generated name: %q",srv.fileName)}
}

type t11FileNameServer struct {fakeServer;fileName string}
func (s *t11FileNameServer) StoreImageOriginal(_ context.Context,_ int64,_ Message,name string,_ []byte)(string,error) {
	s.fileName=name
	return "art_t11_document",nil
}

func TestT11ImageDocumentWithoutFileNameDecodedFromImageMIME(t *testing.T){
	dto:=botUpdateDTO{ID:955,Message:&botMessageDTO{MessageID:8,Document:&botVoiceDTO{FileID:"doc",FileUniqueID:"unique-doc",MimeType:"image/png"}}}
	u:=dto.update()
	if u.Message==nil || u.Message.Image==nil || u.Message.Image.FileName!="" {t.Fatalf("image document without filename rejected: %+v",u)}
	_,_ = json.Marshal(u)
}
