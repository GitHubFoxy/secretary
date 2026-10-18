package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type t11RejectOnceServer struct {
	t11SignalServer
	workerMu sync.Mutex
	calls int
}
func (s *t11RejectOnceServer) SendWorkerMessage(_ context.Context, message WorkerMessage) error {
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	s.calls++
	if s.calls==1 {
		return errors.New(`telegram: server returned 400 Bad Request: {"error":"image_input_unsupported"}`)
	}
	return nil
}

type t11FlakyWarningTransport struct {
	*t11Transport
	mu sync.Mutex
	sends int
}
func (tr *t11FlakyWarningTransport) SendMessage(ctx context.Context,m OutgoingMessage)error {
	tr.mu.Lock()
	tr.sends++
	current:=tr.sends
	tr.mu.Unlock()
	if current==1 {return errors.New("telegram: Bot API sendMessage failed (503)")}
	return tr.fakeTransport.SendMessage(ctx,m)
}

func TestT11NativeWorkerRejection503ThenRestartDoesNotReDownloadImage(t *testing.T) {
	group:=int64(-200)
	path:=filepath.Join(t.TempDir(),"telegram.json")
	tr:=&t11FlakyWarningTransport{t11Transport:&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}}
	srv:=&t11RejectOnceServer{t11SignalServer:t11SignalServer{delivered:make(chan string,4)}}
	newAdapter:=func()*Adapter{
		a,err:=New(Config{StatePath:path,OwnerChatID:100,GroupChatID:&group},tr,srv)
		if err!=nil {t.Fatal(err)}
		return a
	}
	a:=newAdapter()
	a.mu.Lock()
	a.state.GroupChat=group
	a.state.OwnerUser=100
	a.state.Topics["worker"]=TopicMapping{WorkerRef:"worker",ChatID:group,ThreadID:5,PendingRequestID:"question-A"}
	err:=a.saveLocked()
	a.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	first:=t11Update(2000,group,5)
	second:=t11Update(2001,group,5)
	if err:=a.HandleUpdate(context.Background(),first);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	a.mu.Lock()
	remaining,notice:=len(a.state.PendingImages),len(a.state.Outbox)
	a.mu.Unlock()
	if remaining!=0 || notice!=1 || tr.downloads!=1 {t.Fatalf("rejected Worker image retried because of notice 503: queue=%d notice=%d downloads=%d",remaining,notice,tr.downloads)}
	restored:=newAdapter()
	// Worker Topic warnings are flushed by the independent topic recovery
	// loop, not by the Secretary DM Flush path.
	if err:=restored.drainTopicOutbox(context.Background());err!=nil {t.Fatal(err)}
	if err:=restored.HandleUpdate(context.Background(),second);err!=nil {t.Fatal(err)}
	restored.imageWG.Wait()
	if err:=restored.HandleUpdate(context.Background(),first);err!=nil {t.Fatal(err)}
	restored.imageWG.Wait()
	srv.workerMu.Lock()
	count:=srv.calls
	srv.workerMu.Unlock()
	tr.mu.Lock()
	sends:=tr.sends
	tr.mu.Unlock()
	if count!=2 || tr.downloads!=2 || sends!=2 {t.Fatalf("native rejection replayed or warning missing: worker=%d downloads=%d sends=%d",count,tr.downloads,sends)}
	restored.mu.Lock()
	pending,warning:=len(restored.state.PendingImages),len(restored.state.Outbox)
	restored.mu.Unlock()
	if pending!=0 || warning!=0 {t.Fatalf("queue not recovered: images=%d notices=%d",pending,warning)}
}

func TestT11Download404IsTerminalAndNextImageProceeds(t *testing.T) {
	var mu sync.Mutex
	notices,badDownloads,goodDownloads:=0,0,0
	image:=t11PNG(t)
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path{
		case "/botfixture/getFile":
			_ = r.ParseForm()
			filePath:="valid.png"
			if r.Form.Get("file_id")=="bad" {filePath="missing.png"}
			_,_=fmt.Fprintf(w,`{"ok":true,"result":{"file_path":%q,"file_size":0}}`,filePath)
		case "/file/botfixture/missing.png":
			mu.Lock();badDownloads++;mu.Unlock()
			http.NotFound(w,r)
		case "/file/botfixture/valid.png":
			mu.Lock();goodDownloads++;mu.Unlock()
			_,_=w.Write(image)
		case "/botfixture/sendMessage":
			mu.Lock();notices++;mu.Unlock()
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		case "/botfixture/sendChatAction":
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:
			http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	srv:=&t11SignalServer{delivered:make(chan string,3)}
	a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100},
		&BotAPITransport{BaseURL:bot.URL,BotToken:"fixture"},srv)
	if err!=nil {t.Fatal(err)}
	bad:=t11Update(2100,100,0)
	bad.Message.Image.FileID="bad"
	bad.Message.Image.FileSize=0
	good:=t11Update(2101,100,0)
	good.Message.Image.FileID="good"
	good.Message.Image.FileSize=0
	if err:=a.HandleUpdate(context.Background(),bad);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	if err:=a.HandleUpdate(context.Background(),good);err!=nil {t.Fatal(err)}
	a.imageWG.Wait()
	mu.Lock()
	n,b,g:=notices,badDownloads,goodDownloads
	mu.Unlock()
	if n!=1 || b!=1 || g!=1 {t.Fatalf("permanent file download retried or queued follower blocked: notices=%d bad=%d good=%d",n,b,g)}
	select {
	case m:=<-srv.delivered:
		if !strings.Contains(m,"update 2101") {t.Fatalf("wrong image delivered: %q",m)}
	default:t.Fatal("following image was never delivered")
	}
	a.mu.Lock()
	pending:=len(a.state.PendingImages)
	a.mu.Unlock()
	if pending!=0 {t.Fatalf("download 404 left image pending: %d",pending)}
}

func TestT11SlowImageForQuestionAMustNeverAnswerQuestionB(t *testing.T) {
	group:=int64(-200)
	tr:=&t11BlockedDownload{fakeTransport:&fakeTransport{},entered:make(chan struct{}),unblock:make(chan struct{}),data:t11PNG(t)}
	srv:=&t11RejectOnceServer{t11SignalServer:t11SignalServer{delivered:make(chan string,2)}}
	a,err:=New(Config{StatePath:filepath.Join(t.TempDir(),"state.json"),OwnerChatID:100,GroupChatID:&group},tr,srv)
	if err!=nil {t.Fatal(err)}
	a.mu.Lock()
	a.state.GroupChat=group
	a.state.OwnerUser=100
	a.state.Topics["worker"]=TopicMapping{WorkerRef:"worker",ChatID:group,ThreadID:5,PendingRequestID:"question-A"}
	err=a.saveLocked()
	a.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	photo:=t11Update(2200,group,5)
	if err:=a.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
	select{case <-tr.entered:case <-time.After(2*time.Second):t.Fatal("slow Telegram download never started")}
	a.mu.Lock()
	mapping:=a.state.Topics["worker"]
	mapping.PendingRequestID="question-B"
	a.state.Topics["worker"]=mapping
	err=a.saveLocked()
	a.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	close(tr.unblock)
	a.imageWG.Wait()
	srv.workerMu.Lock()
	calls:=srv.calls
	srv.workerMu.Unlock()
	if calls!=0 || srv.images!=0 {t.Fatalf("photo for question A leaked into B: dispatches=%d stored=%d",calls,srv.images)}
	if tr.fakeTransport==nil {t.Fatal("no fake transport")}
	a.mu.Lock()
	pending,notice:=len(a.state.PendingImages),len(a.state.Outbox)
	a.mu.Unlock()
	if pending!=0 || notice!=0 {t.Fatalf("stale request blocked queue: pending=%d notice=%d",pending,notice)}
}
