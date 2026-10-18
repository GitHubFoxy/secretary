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

type t11HangingNoticesTransport struct{
	*fakeTransport
	mu sync.Mutex
	picture []byte
	entered chan struct{}
	release chan struct{}
	warningCalls int
	downloads int
}
func (t *t11HangingNoticesTransport) GetFile(_ context.Context,fileID string)(TelegramFile,error){
	if strings.HasPrefix(fileID,"bad-") {
		return TelegramFile{},fmt.Errorf("%w: forbidden",ErrTelegramFileRejected)
	}
	return TelegramFile{FilePath:"valid.png",FileSize:int64(len(t.picture))},nil
}
func (t *t11HangingNoticesTransport) DownloadFile(_ context.Context,_ string,_ int64)([]byte,error){
	t.mu.Lock()
	t.downloads++
	t.mu.Unlock()
	select{case t.entered<-struct{}{}:default:}
	return t.picture,nil
}
func (t *t11HangingNoticesTransport) SendMessage(ctx context.Context,m OutgoingMessage)error {
	t.mu.Lock()
	t.warningCalls++
	t.mu.Unlock()
	select{
	case <-t.release:
		return t.fakeTransport.SendMessage(ctx,m)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestT11HangingTerminalWarningsReleaseSlotsForIndependentThirdTopic(t *testing.T){
	const group int64=-200
	tr:=&t11HangingNoticesTransport{
		fakeTransport:&fakeTransport{},picture:t11PNG(t),
		entered:make(chan struct{},4),release:make(chan struct{}),
	}
	srv:=&t11SignalServer{delivered:make(chan string,4)}
	a:=t11BoundTopicAdapter(t,filepath.Join(t.TempDir(),"state.json"),tr,srv,group,3)
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	for i:=0;i<2;i++ {
		photo:=t11Update(int64(5000+i),group,int64(5+i))
		photo.Message.Image.FileID=fmt.Sprintf("bad-%d",i)
		if err:=a.HandleUpdate(ctx,photo);err!=nil {t.Fatal(err)}
	}
	// Both terminal image jobs must reach the atomic warning checkpoint.
	deadline:=time.After(3*time.Second)
	for {
		a.mu.Lock()
		warnings,remaining:=len(a.state.Outbox),len(a.state.PendingImages)
		a.mu.Unlock()
		if warnings==2 && remaining==0 {break}
		select{case <-time.After(10*time.Millisecond):
		case <-deadline:t.Fatalf("two terminal checkpoints not persisted: warnings=%d pending=%d",warnings,remaining)}
	}
	// At least one warning is actively blocked inside Telegram sendMessage.
	deadline=time.After(2*time.Second)
	for {
		tr.mu.Lock()
		sends:=tr.warningCalls
		tr.mu.Unlock()
		if sends>0 {break}
		select{case <-time.After(10*time.Millisecond):
		case <-deadline:t.Fatal("warning delivery never started")}
	}
	good:=t11Update(5002,group,7)
	good.Message.Image.FileID="good"
	if err:=a.HandleUpdate(ctx,good);err!=nil {t.Fatal(err)}
	select {
	case <-tr.entered: // third independent Topic started DownloadFile
	case <-time.After(time.Second):t.Fatal("two blocked warning sends held the image semaphore")
	}
	close(tr.release)
	a.imageWG.Wait()
	tr.mu.Lock()
	sends,downloads:=tr.warningCalls,tr.downloads
	tr.mu.Unlock()
	if downloads!=1 || sends!=2 {
		t.Fatalf("warning/download counts after recovery: warnings=%d downloads=%d",sends,downloads)
	}
	a.mu.Lock()
	pending,queued,slots:=len(a.state.PendingImages),len(a.state.Outbox),len(a.imageSlots)
	a.mu.Unlock()
	if pending!=0 || queued!=0 || slots!=0 {
		t.Fatalf("terminal outbox/released slot inconsistent: images=%d outbox=%d slots=%d",pending,queued,slots)
	}
}

func TestT11Download401TerminalUnblocksFollowingImageAndText(t *testing.T){
	path:=filepath.Join(t.TempDir(),"state.json")
	png:=t11PNG(t)
	var mu sync.Mutex
	badDownloads,goodDownloads,warnings:=0,0,0
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path {
		case "/botTOKEN/getFile":
			_ = r.ParseForm()
			file:="good.png"
			if r.Form.Get("file_id")=="bad" {file="unauthorized.png"}
			_,_=fmt.Fprintf(w,`{"ok":true,"result":{"file_path":%q,"file_size":0}}`,file)
		case "/file/botTOKEN/unauthorized.png":
			mu.Lock();badDownloads++;mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
		case "/file/botTOKEN/good.png":
			mu.Lock();goodDownloads++;mu.Unlock()
			_,_=w.Write(png)
		case "/botTOKEN/sendMessage":
			mu.Lock();warnings++;mu.Unlock()
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		case "/botTOKEN/sendChatAction":
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	srv:=&t11SignalServer{delivered:make(chan string,4)}
	open:=func()*Adapter {
		t.Helper()
		a,err:=New(Config{StatePath:path,OwnerChatID:100},&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"},srv)
		if err!=nil {t.Fatal(err)}
		return a
	}
	a:=open()
	bad:=t11Update(5100,100,0)
	bad.Message.Image.FileID="bad"
	bad.Message.Image.FileSize=0
	good:=t11Update(5101,100,0)
	good.Message.Image.FileID="good"
	good.Message.Image.FileSize=0
	text:=Update{ID:5102,Message:&Message{ChatID:100,FromID:100,MessageID:5102,Text:"text C after B"}}
	for _,u:=range []Update{bad,good,text}{
		if err:=a.HandleUpdate(context.Background(),u);err!=nil{t.Fatal(err)}
	}
	a.imageWG.Wait()
	select{
	case first:=<-srv.delivered:
		if !strings.Contains(first,"update 5101") {t.Fatalf("valid photo B not delivered first: %q",first)}
	case <-time.After(time.Second):t.Fatal("HTTP 401 blocked photo B")
	}
	select{
	case second:=<-srv.delivered:
		if second!=text.Message.Text {t.Fatalf("text C overtook image B: %q",second)}
	case <-time.After(time.Second):t.Fatal("HTTP 401 blocked following text C")
	}
	reopened:=open()
	if err:=reopened.PollOnce(context.Background());err!=nil{t.Fatal(err)}
	reopened.imageWG.Wait()
	mu.Lock()
	b,g,n:=badDownloads,goodDownloads,warnings
	mu.Unlock()
	if b!=1 || g!=1 || n!=1 {t.Fatalf("download 401 retry/warning duplicate: bad=%d good=%d warnings=%d",b,g,n)}
	reopened.mu.Lock()
	queued:=len(reopened.state.PendingImages)+len(reopened.state.PendingInputs)+len(reopened.state.Outbox)
	reopened.mu.Unlock()
	if queued!=0 {t.Fatalf("download 401 left blocked queue: %d",queued)}
}

func TestT11DownloadErrorCodeClassification(t *testing.T){
	for _,test:=range []struct{code int; permanent bool}{
		{400,true},{401,true},{403,true},{404,true},{409,true},{410,true},{413,true},{422,true},
		{408,false},{429,false},{500,false},{502,false},{503,false},{504,false},
	}{
		bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(test.code)}))
		_,err:=(&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"}).DownloadFile(context.Background(),"fixture.png",100)
		bot.Close()
		if errors.Is(err,ErrTelegramDownloadRejected)!=test.permanent{
			t.Errorf("DownloadFile HTTP %d classified incorrectly: %v",test.code,err)
		}
	}
}
