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

func TestT11GetFilePermanentStatusClassification(t *testing.T) {
	for _, tc := range []struct{
		code int
		permanent bool
	}{
		{400,true}, {403,true}, {404,true}, {413,true},
		{408,false}, {429,false}, {500,false}, {502,false}, {503,false}, {504,false},
	} {
		err := botAPIError("getFile", tc.code, "test")
		if got := errors.Is(err, ErrTelegramFileRejected); got != tc.permanent {
			t.Errorf("getFile HTTP %d permanent=%v want %v: %v",tc.code,got,tc.permanent,err)
		}
	}
}

func TestT11GetFile403UnblocksImageAndTextAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(),"state.json")
	picture := t11PNG(t)
	firstEntered := make(chan struct{},1)
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	firstGet,secondGet,warnings := 0,0,0
	bot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path {
		case "/botTOKEN/getFile":
			_ = r.ParseForm()
			switch r.Form.Get("file_id") {
			case "forbidden":
				mu.Lock();firstGet++;mu.Unlock()
				select {case firstEntered<-struct{}{}:default:}
				<-releaseFirst
				w.WriteHeader(http.StatusForbidden)
				_,_ = fmt.Fprint(w,`{"ok":false,"error_code":403,"description":"Forbidden"}`)
			case "valid":
				mu.Lock();secondGet++;mu.Unlock()
				_,_ = fmt.Fprint(w,`{"ok":true,"result":{"file_path":"good.png","file_size":0}}`)
			default:
				t.Errorf("unexpected file_id: %s",r.Form.Get("file_id"))
			}
		case "/file/botTOKEN/good.png":
			_,_ = w.Write(picture)
		case "/botTOKEN/sendMessage":
			mu.Lock();warnings++;mu.Unlock()
			_,_ = fmt.Fprint(w,`{"ok":true,"result":true}`)
		case "/botTOKEN/sendChatAction":
			_,_ = fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:
			http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	server:=&t11SignalServer{delivered:make(chan string,5)}
	open:=func()*Adapter{
		t.Helper()
		a,err:=New(Config{StatePath:path,OwnerChatID:100},&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"},server)
		if err!=nil{t.Fatal(err)}
		return a
	}
	a:=open()
	photoA:=t11Update(4100,100,0)
	photoA.Message.Image.FileID="forbidden"
	photoA.Message.Image.FileSize=0
	photoB:=t11Update(4101,100,0)
	photoB.Message.Image.FileID="valid"
	photoB.Message.Image.FileSize=0
	textC:=Update{ID:4102,Message:&Message{ChatID:100,FromID:100,MessageID:4102,Text:"question C after image B"}}
	if err:=a.HandleUpdate(context.Background(),photoA);err!=nil{t.Fatal(err)}
	select{case <-firstEntered:case <-time.After(3*time.Second):t.Fatal("first getFile not entered")}
	if err:=a.HandleUpdate(context.Background(),photoB);err!=nil{t.Fatal(err)}
	if err:=a.HandleUpdate(context.Background(),textC);err!=nil{t.Fatal(err)}
	a.mu.Lock()
	inputs,images:=len(a.state.PendingInputs),len(a.state.PendingImages)
	a.mu.Unlock()
	if inputs!=2 || images!=1 {t.Fatalf("B/C were not durably waiting behind A: inputs=%d images=%d",inputs,images)}
	close(releaseFirst)
	a.imageWG.Wait()
	if err:=a.drainLane(context.Background(),inputLane(textC));err!=nil{t.Fatal(err)}
	a.imageWG.Wait()
	select{
	case first:=<-server.delivered:
		if !strings.Contains(first,"update 4101") || !strings.Contains(first,"artifact://"){
			t.Fatalf("expected image B before text C, got %q",first)
		}
	case <-time.After(3*time.Second):t.Fatal("image B was blocked by forbidden image")
	}
	select{
	case second:=<-server.delivered:
		if second!=textC.Message.Text {t.Fatalf("dependent text C lost or reordered: %q",second)}
	case <-time.After(3*time.Second):t.Fatal("dependent text C still blocked")
	}
	restored:=open()
	if err:=restored.HandleUpdate(context.Background(),photoA);err!=nil{t.Fatal(err)}
	restored.imageWG.Wait()
	mu.Lock()
	bad,good,notices:=firstGet,secondGet,warnings
	mu.Unlock()
	if bad!=1 || good!=1 || notices!=1 {t.Fatalf("403 retried or warning duplicated: bad=%d good=%d warnings=%d",bad,good,notices)}
	restored.mu.Lock()
	remainingImages,remainingInputs,remainingWarnings:=len(restored.state.PendingImages),len(restored.state.PendingInputs),len(restored.state.Outbox)
	restored.mu.Unlock()
	if remainingImages!=0 || remainingInputs!=0 || remainingWarnings!=0 {
		t.Fatalf("403 held durable queue after restart: images=%d inputs=%d warnings=%d",remainingImages,remainingInputs,remainingWarnings)
	}
}

func TestT11CrashAfterDurableTerminalWarningDoesNotRepeatGetFile(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"state.json")
	picture:=t11PNG(t)
	var mu sync.Mutex
	badGet,goodGet,warningAttempts,warningDelivered:=0,0,0,0
	bot:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		switch r.URL.Path{
		case "/botTOKEN/getFile":
			_ = r.ParseForm()
			if r.Form.Get("file_id")=="bad" {
				mu.Lock();badGet++;mu.Unlock()
				w.WriteHeader(http.StatusForbidden)
				_,_=fmt.Fprint(w,`{"ok":false,"error_code":403,"description":"Forbidden"}`)
			} else if r.Form.Get("file_id")=="good" {
				mu.Lock();goodGet++;mu.Unlock()
				_,_=fmt.Fprint(w,`{"ok":true,"result":{"file_path":"good.png","file_size":0}}`)
			} else {
				t.Errorf("unknown getFile id %q",r.Form.Get("file_id"))
			}
		case "/file/botTOKEN/good.png":
			_,_=w.Write(picture)
		case "/botTOKEN/sendMessage":
			mu.Lock()
			warningAttempts++
			attempt:=warningAttempts
			if attempt>1 {warningDelivered++}
			mu.Unlock()
			if attempt==1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_,_=fmt.Fprint(w,`{"ok":false,"error_code":503,"description":"temporarily unavailable"}`)
			} else {
				_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
			}
		case "/botTOKEN/sendChatAction":
			_,_=fmt.Fprint(w,`{"ok":true,"result":true}`)
		default:
			http.NotFound(w,r)
		}
	}))
	defer bot.Close()
	server:=&t11SignalServer{delivered:make(chan string,4)}
	open:=func()*Adapter {
		t.Helper()
		a,err:=New(Config{StatePath:path,OwnerChatID:100},&BotAPITransport{BaseURL:bot.URL,BotToken:"TOKEN"},server)
		if err!=nil {t.Fatal(err)}
		return a
	}
	beforeCrash:=open()
	bad:=t11Update(4200,100,0)
	bad.Message.Image.FileID="bad"
	bad.Message.Image.FileSize=0
	good:=t11Update(4201,100,0)
	good.Message.Image.FileID="good"
	good.Message.Image.FileSize=0
	textC:=Update{ID:4202,Message:&Message{ChatID:100,FromID:100,MessageID:4202,Text:"question after recovered image"}}
	// Persist A as an in-flight image; B and C are durable inputs blocked
	// behind A. Invoke only the delivery step: stop immediately after the
	// terminal warning checkpoint, before drainImageLane's cleanup.
	beforeCrash.mu.Lock()
	beforeCrash.state.PendingImages=append(beforeCrash.state.PendingImages,bad)
	beforeCrash.state.Processed[fmt.Sprint(bad.ID)]=beforeCrash.now()
	err:=beforeCrash.saveLocked()
	beforeCrash.mu.Unlock()
	if err!=nil {t.Fatal(err)}
	if err:=beforeCrash.enqueueInputs([]Update{good,textC},false);err!=nil{t.Fatal(err)}
	if err:=beforeCrash.deliverImageUpdate(context.Background(),bad,true);err!=nil{t.Fatal(err)}
	beforeCrash.mu.Lock()
	pending,warnings:=len(beforeCrash.state.PendingImages),len(beforeCrash.state.Outbox)
	beforeCrash.mu.Unlock()
	if pending!=0 || warnings!=1 {
		t.Fatalf("terminal warning not atomic with queue removal: images=%d warnings=%d",pending,warnings)
	}
	// Simulate the independent outbox loop attempting delivery while the
	// Bot API returns 503; the terminal image remains completed.
	if err:=beforeCrash.drainOutbox(context.Background());err==nil {
		t.Fatal("first Telegram warning delivery must fail with 503")
	}
	// Simulated hard crash without ever invoking drainImageLane cleanup.
	restored:=open()
	restored.mu.Lock()
	images,queuedWarnings:=len(restored.state.PendingImages),len(restored.state.Outbox)
	restored.mu.Unlock()
	if images!=0 || queuedWarnings!=1 {
		t.Fatalf("crash restored inconsistent image/warning state: images=%d warnings=%d",images,queuedWarnings)
	}
	if err:=restored.drainInputs(context.Background());err!=nil{t.Fatal(err)}
	restored.imageWG.Wait()
	if err:=restored.Flush(context.Background());err!=nil{t.Fatal(err)}
	if err:=restored.HandleUpdate(context.Background(),bad);err!=nil{t.Fatal(err)}
	restored.imageWG.Wait()
	select{
	case first:=<-server.delivered:
		if !strings.Contains(first,"update 4201"){t.Fatalf("B did not precede text C: %q",first)}
	case <-time.After(3*time.Second):t.Fatal("B not recovered")
	}
	select{
	case second:=<-server.delivered:
		if second!=textC.Message.Text {t.Fatalf("C reordered or missing: %q",second)}
	case <-time.After(3*time.Second):t.Fatal("C blocked on old A")
	}
	mu.Lock()
	badCount,goodCount,attempts,received:=badGet,goodGet,warningAttempts,warningDelivered
	mu.Unlock()
	if badCount!=1 || goodCount!=1 || attempts!=2 || received!=1 {
		t.Fatalf("terminal image was re-downloaded or warning duplicated: bad=%d good=%d attempts=%d received=%d",badCount,goodCount,attempts,received)
	}
	restored.mu.Lock()
	pending,warnings=len(restored.state.PendingImages),len(restored.state.Outbox)
	remainingInputs:=len(restored.state.PendingInputs)
	restored.mu.Unlock()
	if pending!=0 || warnings!=0 || remainingInputs!=0 {t.Fatalf("recovered queues nonempty: images=%d warnings=%d inputs=%d",pending,warnings,remainingInputs)}
}
