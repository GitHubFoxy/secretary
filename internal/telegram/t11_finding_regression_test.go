package telegram

import (
 "context"
 "errors"
 "fmt"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "sync"
 "testing"
 "time"
)

type t11HTTPWorkerBridge struct {
 *t11SignalServer
 remote *HTTPServerClient
}
func (b *t11HTTPWorkerBridge) SendWorkerMessage(ctx context.Context,m WorkerMessage)error {
 return b.remote.SendWorkerMessage(ctx,m)
}

func t11BoundTopicAdapter(t *testing.T,path string,tr Transport,server ServerClient,group int64,topics int)*Adapter{
 t.Helper()
 a,err:=New(Config{StatePath:path,OwnerChatID:100,GroupChatID:&group},tr,server)
 if err!=nil {t.Fatal(err)}
 a.mu.Lock()
 a.state.GroupChat=group
 a.state.OwnerUser=100
 for i:=0;i<topics;i++ {
   ref:=fmt.Sprintf("worker-%d",i)
   if _,ok:=a.state.Topics[ref];!ok {
    a.state.Topics[ref]=TopicMapping{WorkerRef:ref,ChatID:group,ThreadID:int64(i+5),PendingRequestID:"request-A"}
   }
 }
 err=a.saveLocked()
 a.mu.Unlock()
 if err!=nil {t.Fatal(err)}
 return a
}

func TestT11WorkerDispatchHTTP503RetriesAfterServerRestart(t *testing.T){
 var mu sync.Mutex
 attempts,accepted:=0,0
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/v1/workers/worker-0/message" {http.NotFound(w,r);return}
  mu.Lock()
  attempts++
  n:=attempts
  if n>1 {accepted++}
  mu.Unlock()
  if n==1 {
   w.WriteHeader(http.StatusServiceUnavailable)
   _,_=fmt.Fprint(w,`{"error":"temporary failure"}`)
  } else {
   w.Header().Set("Content-Type","application/json")
   _,_=fmt.Fprint(w,`{}`)
  }
 }))
 defer server.Close()
 tr:=&t11Transport{fakeTransport:&fakeTransport{},data:t11PNG(t)}
 bridge:=&t11HTTPWorkerBridge{t11SignalServer:&t11SignalServer{delivered:make(chan string,4)},
  remote:&HTTPServerClient{BaseURL:server.URL,Credential:"fixture"}}
 path:=filepath.Join(t.TempDir(),"state.json")
 const group int64=-200
 a:=t11BoundTopicAdapter(t,path,tr,bridge,group,1)
 photo:=t11Update(3000,group,5)
 if err:=a.HandleUpdate(context.Background(),photo);err!=nil{t.Fatal(err)}
 a.imageWG.Wait()
 a.mu.Lock()
 queued,notices:=len(a.state.PendingImages),len(a.state.Outbox)
 a.mu.Unlock()
 if queued!=1 || notices!=0 || tr.downloads!=1 {
  t.Fatalf("transient 503 incorrectly ended image: queued=%d notices=%d downloads=%d",queued,notices,tr.downloads)
 }
 // New adapter exercises persisted PendingImages after Server comes back.
 restored:=t11BoundTopicAdapter(t,path,tr,bridge,group,1)
 restored.startImageWorkers(context.Background())
 restored.imageWG.Wait()
 if err:=restored.HandleUpdate(context.Background(),photo);err!=nil {t.Fatal(err)}
 restored.imageWG.Wait()
 mu.Lock()
 at,ok:=attempts,accepted
 mu.Unlock()
 if at!=2 || ok!=1 {t.Fatalf("worker 503 replay: attempts=%d accepted=%d",at,ok)}
 restored.mu.Lock()
 queued,notices=len(restored.state.PendingImages),len(restored.state.Outbox)
 restored.mu.Unlock()
 if queued!=0 || notices!=0 {t.Fatalf("successful retry left queue: images=%d warnings=%d",queued,notices)}
}

func TestT11BlockedPendingInputsFreezeOriginalRequestBeforeQueuePromotion(t *testing.T) {
 group:=int64(-200)
 tr:=&t11BlockedDownload{fakeTransport:&fakeTransport{},entered:make(chan struct{}),unblock:make(chan struct{}),data:t11PNG(t)}
 server:=&t11RejectOnceServer{t11SignalServer:t11SignalServer{delivered:make(chan string,3)}}
 a:=t11BoundTopicAdapter(t,filepath.Join(t.TempDir(),"state.json"),tr,server,group,1)
 older:=t11Update(3100,group,5)
 blocked:=t11Update(3101,group,5)
 if err:=a.HandleUpdate(context.Background(),older);err!=nil {t.Fatal(err)}
 select{case <-tr.entered:case <-time.After(2*time.Second):t.Fatal("older image never started")}
 if err:=a.HandleUpdate(context.Background(),blocked);err!=nil {t.Fatal(err)}
 a.mu.Lock()
 if len(a.state.PendingInputs)!=1 || a.state.PendingInputs[0].WorkerRequestID==nil ||
  *a.state.PendingInputs[0].WorkerRequestID!="request-A" || a.state.PendingInputs[0].ImageWorkerRef!="worker-0" {
  a.mu.Unlock()
  t.Fatal("blocked image lost its original durable WorkerRef / pending request")
 }
 mapping:=a.state.Topics["worker-0"]
 mapping.PendingRequestID="request-B"
 a.state.Topics["worker-0"]=mapping
 err:=a.saveLocked()
 a.mu.Unlock()
 if err!=nil {t.Fatal(err)}
 close(tr.unblock)
 a.imageWG.Wait()
 server.workerMu.Lock()
 workerCalls:=server.calls
 server.workerMu.Unlock()
 server.mu.Lock()
 stored:=server.images
 server.mu.Unlock()
 if workerCalls!=0 || stored!=0 {t.Fatalf("old photo dispatched into request B: worker=%d stored=%d",workerCalls,stored)}
 a.mu.Lock()
 inputs,images,notices:=len(a.state.PendingInputs),len(a.state.PendingImages),len(a.state.Outbox)
 a.mu.Unlock()
 if inputs!=0 || images!=0 || notices!=0 {t.Fatalf("stale A images blocked B: inputs=%d images=%d warnings=%d",inputs,images,notices)}
}

type t11GateDownloadTransport struct {
 *fakeTransport
 mu sync.Mutex
 active int
 peak int
 calls int
 entered chan struct{}
 release chan struct{}
 failFirst bool
}
func (tr *t11GateDownloadTransport) GetFile(context.Context,string)(TelegramFile,error){
 return TelegramFile{FilePath:"image.png",FileSize:0},nil
}
func (tr *t11GateDownloadTransport) DownloadFile(ctx context.Context,_ string,_ int64)([]byte,error){
 tr.mu.Lock()
 tr.active++
 tr.calls++
 attempt:=tr.calls
 if tr.active>tr.peak {tr.peak=tr.active}
 tr.mu.Unlock()
 select{case tr.entered<-struct{}{}:default:}
 defer func(){tr.mu.Lock();tr.active--;tr.mu.Unlock()}()
 select{
 case <-ctx.Done():return nil,ctx.Err()
 case <-tr.release:
 }
 if tr.failFirst && attempt==1 {return nil,errors.New("temporary upstream 503")}
 return t11ValidTinyPNG,nil
}

// PNG payload is initialized from an existing tested fixture before starting
// concurrent image workers; the returned bytes are treated as immutable.
var t11ValidTinyPNG []byte

func TestT11ImageDownloadsBoundAcrossTopicsAndReleaseAfterError(t *testing.T){
 group:=int64(-200)
 const workers=7
 tr:=&t11GateDownloadTransport{fakeTransport:&fakeTransport{},entered:make(chan struct{},workers),release:make(chan struct{}),failFirst:true}
 t11ValidTinyPNG=t11PNG(t)
 srv:=&t11SignalServer{delivered:make(chan string,workers)}
 a:=t11BoundTopicAdapter(t,filepath.Join(t.TempDir(),"state.json"),tr,srv,group,workers)
 for i:=0;i<workers;i++{
  update:=t11Update(int64(3200+i),group,int64(i+5))
  if err:=a.HandleUpdate(context.Background(),update);err!=nil{t.Fatal(err)}
 }
 deadline:=time.After(3*time.Second)
 for {
  tr.mu.Lock()
  active,peak:=tr.active,tr.peak
  tr.mu.Unlock()
  if active==maxConcurrentTelegramImages {
   if peak>maxConcurrentTelegramImages {t.Fatalf("image pool overflow: peak=%d",peak)}
   break
  }
  select{case <-tr.entered:case <-deadline:t.Fatalf("never filled image pool: active=%d peak=%d",active,peak)}
 }
 // With slots occupied, additional Topics must not begin downloads.
 select{case <-tr.entered: t.Fatal("too many concurrent downloads")
 case <-time.After(50*time.Millisecond):}
 close(tr.release)
 a.imageWG.Wait()
 tr.mu.Lock()
 peak,calls,remaining:=tr.peak,tr.calls,tr.active
 tr.mu.Unlock()
 if peak>maxConcurrentTelegramImages || remaining!=0 || calls!=workers {
  t.Fatalf("image concurrency limit broken: peak=%d remaining=%d calls=%d",peak,remaining,calls)
 }
 a.mu.Lock()
 queued:=len(a.state.PendingImages)
 a.mu.Unlock()
 if queued!=1 {t.Fatalf("expected one durable transient retry after slot release: %d",queued)}
 a.startImageWorkers(context.Background())
 a.imageWG.Wait()
 a.mu.Lock()
 queued=len(a.state.PendingImages)
 a.mu.Unlock()
 if queued!=0 || len(a.imageSlots)!=0 {t.Fatalf("slot or failed image not released: queue=%d slots=%d",queued,len(a.imageSlots))}
}

func TestT11ImageDownloadCancellationReleasesPoolSlot(t *testing.T){
 group:=int64(-200)
 tr:=&t11GateDownloadTransport{fakeTransport:&fakeTransport{},entered:make(chan struct{},2),release:make(chan struct{})}
 t11ValidTinyPNG=t11PNG(t)
 srv:=&t11SignalServer{delivered:make(chan string,2)}
 a:=t11BoundTopicAdapter(t,filepath.Join(t.TempDir(),"state.json"),tr,srv,group,2)
 ctx,cancel:=context.WithCancel(context.Background())
 if err:=a.HandleUpdate(ctx,t11Update(3300,group,5));err!=nil {t.Fatal(err)}
 select{case <-tr.entered:case <-time.After(2*time.Second):t.Fatal("cancel fixture never entered downloader")}
 cancel()
 a.imageWG.Wait()
 if len(a.imageSlots)!=0 {t.Fatal("canceled worker leaked global download slot")}
 close(tr.release)
 a.startImageWorkers(context.Background())
 a.imageWG.Wait()
 if err:=a.HandleUpdate(context.Background(),t11Update(3301,group,6));err!=nil {t.Fatal(err)}
 a.imageWG.Wait()
 a.mu.Lock()
 n:=len(a.state.PendingImages)
 a.mu.Unlock()
 if n!=0 || len(a.imageSlots)!=0 {t.Fatalf("download slots remained occupied after cancellation/recovery: pending=%d slots=%d",n,len(a.imageSlots))}
}
