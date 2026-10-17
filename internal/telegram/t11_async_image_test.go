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
	group:=int64(-200)
	newAdapter:=func()*Adapter{
		t.Helper()
		a,err:=New(Config{StatePath:path,OwnerChatID:100,GroupChatID:&group},tr,srv)
		if err!=nil {t.Fatal(err)}
		return a
	}
	a:=newAdapter()
	a.mu.Lock()
	a.state.GroupChat=group
	a.state.OwnerUser=100
	err:=a.saveLocked()
	a.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	photo:=t11Update(800,100,0)
	if err:=a.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
	select {case <-tr.entered:case <-time.After(2*time.Second):t.Fatal("image download did not start")}
	text:=Update{ID:801,Message:&Message{ChatID:group,FromID:100,MessageID:801,Text:"independent text in another authorized chat"}}
	if err:=a.HandleUpdate(context.Background(),text);err!=nil {t.Fatal(err)}
	select {
	case body:=<-srv.delivered:
		if body!=text.Message.Text {t.Fatalf("text overtaken by blocked image: %q",body)}
	case <-time.After(time.Second):
		t.Fatal("unrelated chat was blocked while Bot API was slow")
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

func TestT11RestartDrainsPersistedImageWithoutReplayingUpdate(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"durable.json")
	tr:=&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}
	srv:=&t11SignalServer{delivered:make(chan string,2)}
	old,err:=New(Config{StatePath:path,OwnerChatID:100},tr,srv)
	if err!=nil {t.Fatal(err)}
	update:=t11Update(980,100,0)
	old.mu.Lock()
	old.state.PendingImages=append(old.state.PendingImages,update)
	old.state.Processed[fmt.Sprint(update.ID)]=old.now()
	err=old.saveLocked()
	old.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	restored,err:=New(Config{StatePath:path,OwnerChatID:100},tr,srv)
	if err!=nil {t.Fatal(err)}
	if err:=restored.PollOnce(context.Background());err!=nil {t.Fatal(err)}
	restored.imageWG.Wait()
	select{case body:=<-srv.delivered:
		if !strings.Contains(body,"artifact://art_t11_async") {t.Fatalf("restored image lost: %q",body)}
	default:t.Fatal("restored job not executed")}
	if err:=restored.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	restored.imageWG.Wait()
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.images!=1 || len(srv.texts)!=1 {t.Fatalf("recovery duplicated source or inbound: %+v",srv)}
}


func TestT11GetFilePermanent400WarnsOnceAndUnblocksNextImage(t *testing.T) {
	var mu sync.Mutex
	noticeCount, firstGetFile, secondGetFile := 0, 0, 0
	png := t11PNG(t)
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path {
		case "/botTOKEN/getFile":
			_ = r.ParseForm()
			switch r.Form.Get("file_id") {
			case "too-large":
				mu.Lock();firstGetFile++;mu.Unlock()
				w.WriteHeader(http.StatusBadRequest)
				_,_=fmt.Fprint(w,`{"ok":false,"error_code":400,"description":"Bad Request: file is too big"}`)
			case "valid":
				mu.Lock();secondGetFile++;mu.Unlock()
				_,_=fmt.Fprint(w,`{"ok":true,"result":{"file_path":"second.png","file_size":0}}`)
			default:
				t.Errorf("unexpected file_id: %s",r.Form.Get("file_id"))
			}
		case "/file/botTOKEN/second.png":
			_,_=w.Write(png)
		case "/botTOKEN/sendMessage":
			mu.Lock();noticeCount++;mu.Unlock()
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		case "/botTOKEN/sendChatAction":
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected Bot API method: %s",r.URL.Path)
			http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	client:=&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}
	server:=&t11SignalServer{delivered:make(chan string,4)}
	a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},client,server)
	if err!=nil {t.Fatal(err)}
	bad:=t11Update(1200,100,0)
	bad.Message.Image.FileSize=0
	bad.Message.Image.FileID="too-large"
	good:=t11Update(1201,100,0)
	good.Message.Image.FileSize=0
	good.Message.Image.FileID="valid"
	if err:=a.HandleUpdate(context.Background(),bad);err!=nil {t.Fatal(err)}
	if err:=a.HandleUpdate(context.Background(),good);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	if err:=a.HandleUpdate(context.Background(),bad);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	mu.Lock()
	badCount,goodCount,warnings:=firstGetFile,secondGetFile,noticeCount
	mu.Unlock()
	if badCount!=1 || goodCount!=1 || warnings!=1 {t.Fatalf("getFile 400 was retried or blocked next image: bad=%d good=%d notices=%d",badCount,goodCount,warnings)}
	select {
	case body:=<-server.delivered:
		if !strings.Contains(body,"update 1201") {t.Fatalf("wrong image delivered: %q",body)}
	default:
		t.Fatal("image after permanent getFile rejection never delivered")
	}
	a.mu.Lock()
	pending:=len(a.state.PendingImages)
	a.mu.Unlock()
	if pending!=0 {t.Fatalf("permanent rejection blocked image lane: %d",pending)}
}

func TestT11RebindDiscardsQueuedImagesAcrossRestart(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"state.json")
	transport:=&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}
	server:=&t11SignalServer{delivered:make(chan string,2)}
	old,err:=New(Config{StatePath:path,OwnerChatID:100},transport,server)
	if err!=nil {t.Fatal(err)}
	oldPhoto:=t11Update(1300,100,0)
	old.mu.Lock()
	old.state.PendingImages=append(old.state.PendingImages,oldPhoto)
	old.state.Processed[fmt.Sprint(oldPhoto.ID)]=old.now()
	err=old.saveLocked()
	old.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	if err:=Rebind(path,200,"private-to-forum",0);err!=nil {t.Fatal(err)}
	next,err:=New(Config{StatePath:path,OwnerChatID:200},transport,server)
	if err!=nil {t.Fatal(err)}
	next.mu.Lock()
	if n:=len(next.state.PendingImages);n!=0 {t.Fatalf("Rebind left previous owner's images queued: %d",n)}
	// A stale/corrupt state file must also fail closed after a restart.
	next.state.PendingImages=append(next.state.PendingImages,oldPhoto)
	err=next.saveLocked()
	next.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	restarted,err:=New(Config{StatePath:path,OwnerChatID:200},transport,server)
	if err!=nil {t.Fatal(err)}
	if err:=restarted.PollOnce(context.Background());err!=nil {t.Fatal(err)}
	restarted.imageWG.Wait()
	server.mu.Lock()
	calls:=server.images
	server.mu.Unlock()
	if transport.downloads!=0 || calls!=0 {t.Fatalf("previous owner's photo leaked: downloads=%d uploads=%d",transport.downloads,calls)}
	restarted.mu.Lock()
	pending:=len(restarted.state.PendingImages)
	restarted.mu.Unlock()
	if pending!=0 {t.Fatalf("unauthorized queued image not removed: %d",pending)}
}

func TestT11PhotoBeforeQuestionKeepsCausalOrderInSameConversation(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	download:=&t11BlockedDownload{fakeTransport:&fakeTransport{},entered:make(chan struct{}),unblock:make(chan struct{}),data:t11PNG(t)}
	server:=&t11SignalServer{delivered:make(chan string,3)}
	a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},download,server)
	if err!=nil {t.Fatal(err)}
	photo:=t11Update(1400,100,0)
	question:=Update{ID:1401,Message:&Message{ChatID:100,FromID:100,MessageID:1401,Text:"Что изображено?"}}
	if err:=a.HandleUpdate(ctx,photo);err!=nil {t.Fatal(err)}
	select {case <-download.entered:case <-ctx.Done():t.Fatal("slow download not reached")}
	// Processing a dependent message must checkpoint it without blocking
	// the handler, but must NOT dispatch it before the image.
	done:=make(chan error,1)
	go func(){done<-a.HandleUpdate(ctx,question)}()
	select{case err:=<-done:if err!=nil {t.Fatal(err)}
	case <-time.After(time.Second):t.Fatal("dependent input was not durably parked")}
	select{case body:=<-server.delivered:t.Fatalf("question overtook its image: %q",body)
	default:}
	a.mu.Lock()
	pendingInputs,pendingImages:=len(a.state.PendingInputs),len(a.state.PendingImages)
	a.mu.Unlock()
	if pendingInputs!=1 || pendingImages!=1 {t.Fatalf("dependency was not persisted: inputs=%d images=%d",pendingInputs,pendingImages)}
	close(download.unblock)
	a.imageWG.Wait()
	select{case body:=<-server.delivered:
		if !strings.Contains(body,"artifact://art_t11_async") {t.Fatalf("image wasn't first: %q",body)}
	case <-ctx.Done():t.Fatal("image never delivered")}
	select{case body:=<-server.delivered:
		if body!="Что изображено?" {t.Fatalf("question was lost/reordered: %q",body)}
	case <-ctx.Done():t.Fatal("dependent question never released")}
}

func TestT11TransientGetFileErrorRetainsDurableImageUntilRetry(t *testing.T) {
	picture:=t11PNG(t)
	var mu sync.Mutex
	calls,warnings:=0,0
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path {
		case "/botTOKEN/getFile":
			mu.Lock()
			calls++
			attempt:=calls
			mu.Unlock()
			if attempt==1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_,_=fmt.Fprint(w,`{"ok":false,"error_code":503,"description":"Temporary upstream problem"}`)
				return
			}
			_,_=fmt.Fprint(w,`{"ok":true,"result":{"file_path":"recovered.png","file_size":0}}`)
		case "/file/botTOKEN/recovered.png":
			_,_=w.Write(picture)
		case "/botTOKEN/sendMessage":
			mu.Lock();warnings++;mu.Unlock()
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		case "/botTOKEN/sendChatAction":
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected API call %s",r.URL.Path)
			http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	server:=&t11SignalServer{delivered:make(chan string,2)}
	adapter,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"},server)
	if err!=nil {t.Fatal(err)}
	update:=t11Update(1500,100,0)
	update.Message.Image.FileSize=0
	if err:=adapter.HandleUpdate(context.Background(),update);err!=nil {t.Fatal(err)}
	adapter.imageWG.Wait()
	adapter.mu.Lock()
	waiting:=len(adapter.state.PendingImages)
	adapter.mu.Unlock()
	if waiting!=1 {t.Fatalf("transient getFile error lost durable work: %d",waiting)}
	adapter.startImageWorkers(context.Background())
	adapter.imageWG.Wait()
	select{case body:=<-server.delivered:
		if !strings.Contains(body,"artifact://art_t11_async") {t.Fatalf("bad recovered image delivery %q",body)}
	default:t.Fatal("transient image never retried")}
	mu.Lock()
	defer mu.Unlock()
	if calls!=2 || warnings!=0 {t.Fatalf("transient getFile was treated as permanent: calls=%d warnings=%d",calls,warnings)}
}
