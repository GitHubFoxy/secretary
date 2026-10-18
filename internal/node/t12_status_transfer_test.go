package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT12StatusPollingNeverRetransfersEightMegabyteOriginal(t *testing.T) {
	id:="art_"+strings.Repeat("1",32)
	first:="art_"+strings.Repeat("2",32)
	second:="art_"+strings.Repeat("3",32)
	webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	original:=bytes.Repeat([]byte{'v'},core.MaxArtifactBytes)
	digest:=func(data []byte) string {h:=sha256.Sum256(data);return hex.EncodeToString(h[:])}
	frames:=[]core.VideoFrame{{ArtifactID:first,TimestampMS:0,TimestampKind:"nominal-grid"},{ArtifactID:second,TimestampMS:1000,TimestampKind:"nominal-grid"}}
	var originalDownloads,frameDownloads,statusRequests,bytesTransferred int
	fetch:=func(_ context.Context, requested string)(ArtifactPayload,error) {
		if requested==id {
			originalDownloads++
			bytesTransferred+=len(original)
			return ArtifactPayload{ID:id,Name:"clip.mp4",SHA256:digest(original),Data:original,
				VideoStatus:"queued",CheckVideoStatus:func(context.Context)(core.Artifact,error){
					statusRequests++
					switch statusRequests {
					case 1:return core.Artifact{ID:id,ProcessingStatus:"queued"},nil
					case 2:return core.Artifact{ID:id,ProcessingStatus:"processing"},nil
					default:return core.Artifact{ID:id,ProcessingStatus:"ready",VideoFrames:frames},nil
					}
				}},nil
		}
		if requested!=first && requested!=second {t.Fatalf("unexpected Artifact ID: %s",requested)}
		frameDownloads++
		bytesTransferred+=len(webp)
		return ArtifactPayload{ID:requested,Name:"frame.webp",SHA256:digest(webp),Data:webp},nil
	}
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	text,images,err:=PrepareArtifactInput(ctx,t.TempDir(),"Analyze "+ "artifact://"+id,fetch)
	if err!=nil{t.Fatal(err)}
	if len(images)!=2 || !strings.Contains(text,"approximate nominal-grid sample time 1.000 s [timestamp_kind=nominal-grid; NOT actual source-frame PTS]") {
		t.Fatalf("missing legacy frame's explicitly approximate timestamps: %d %q",len(images),text)
	}
	if strings.Count(text,"timestamp_kind=nominal-grid; NOT actual source-frame PTS")!=2 {
		t.Fatalf("every legacy frame must carry timestamp semantics, got %q",text)
	}
	if originalDownloads!=1||frameDownloads!=2||statusRequests!=3 {t.Fatalf("transfers: original=%d frames=%d metadata=%d",originalDownloads,frameDownloads,statusRequests)}
	if bytesTransferred!=len(original)+2*len(webp) {t.Fatalf("retransferred full original: %d bytes",bytesTransferred)}
}

// The full prompt preparation path must remain pending for the equivalent of
// 125 seconds of queued/processing polls and deliver its native frame afterward.
// Poll intervals are accelerated; a real dispatch uses 250 ms per poll.
func TestT12FullArtifactPreparationAcceptsLongQueuedVideo(t *testing.T) {
	id:="art_"+strings.Repeat("4",32)
	frameID:="art_"+strings.Repeat("5",32)
	webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	original:=[]byte("durable video source")
	sum:=func(data []byte)string{h:=sha256.Sum256(data);return hex.EncodeToString(h[:])}
	const queueChecks=500
	var polls,originalDownloads int
	fetch:=func(_ context.Context,key string)(ArtifactPayload,error){
		if key==id {
			originalDownloads++
			return ArtifactPayload{
				ID:id,Name:"queued.mp4",Data:original,SHA256:sum(original),
				VideoStatus:"queued",
				CheckVideoStatus:func(context.Context)(core.Artifact,error){
					polls++
					if polls<=queueChecks {
						state:="queued"
						if polls>300 {state="processing"}
						return core.Artifact{ID:id,ProcessingStatus:state},nil
					}
					return core.Artifact{ID:id,ProcessingStatus:"ready",VideoFrames:[]core.VideoFrame{
						{ArtifactID:frameID,TimestampMS:1250,TimestampKind:"source-pts-relative"},
					}},nil
				},
			},nil
		}
		if key!=frameID {return ArtifactPayload{},errors.New("unexpected frame")}
		return ArtifactPayload{ID:frameID,Name:"frame.webp",Data:webp,SHA256:sum(webp)},nil
	}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	task,images,err:=prepareArtifactInputWithPoll(ctx,t.TempDir(),"Describe artifact://"+id,fetch,0)
	if err!=nil{t.Fatalf("dispatch would return artifact_unavailable after long queue: %v",err)}
	if polls!=queueChecks+1 || originalDownloads!=1 || len(images)!=1 || !strings.Contains(task,"1.250 s [timestamp_kind=source-pts-relative]"){
		t.Fatalf("dependent Worker started incorrectly: polls=%d original=%d images=%d task=%q",polls,originalDownloads,len(images),task)
	}
}

// Simulate a full two-converter queue exceeding 120 seconds: at 250 ms per
// poll, 500 queued/processing status checks represent more than 125 seconds.
// The test uses a zero-delay injectable poll rather than sleeping 125 seconds.
func TestT12QueuedVideoDoesNotFailAfterFiveHundredPolls(t *testing.T) {
	id:="art_"+strings.Repeat("a",32)
	frame:="art_"+strings.Repeat("b",32)
	const queuedPolls=500
	var checks int
	artifact:=ArtifactPayload{
		ID:id,VideoStatus:"queued",
		CheckVideoStatus:func(ctx context.Context)(core.Artifact,error){
			checks++
			if checks<=queuedPolls {
				state:="queued"
				if checks>300 {state="processing"}
				return core.Artifact{ID:id,ProcessingStatus:state},nil
			}
			return core.Artifact{ID:id,ProcessingStatus:"ready",
				VideoFrames:[]core.VideoFrame{{ArtifactID:frame,TimestampMS:1250,TimestampKind:"source-pts-relative"}}},nil
		},
	}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	if err:=awaitVideoProcessing(ctx,id,&artifact,0);err!=nil {t.Fatal(err)}
	if checks!=queuedPolls+1 || artifact.VideoStatus!="ready" || artifact.VideoFrames[0].TimestampMS!=1250 {
		t.Fatalf("long-queued conversion incorrectly failed: checks=%d state=%s frames=%+v",checks,artifact.VideoStatus,artifact.VideoFrames)
	}
}

func TestT12QueuedVideoWaitsUntilContextCanceled(t *testing.T) {
	id:="art_"+strings.Repeat("c",32)
	ctx,cancel:=context.WithCancel(context.Background())
	var checks int
	artifact:=ArtifactPayload{ID:id,VideoStatus:"queued",
		CheckVideoStatus:func(context.Context)(core.Artifact,error){
			checks++
			if checks==3 {cancel()}
			return core.Artifact{ID:id,ProcessingStatus:"queued"},nil
		},
	}
	err:=awaitVideoProcessing(ctx,id,&artifact,0)
	if !errors.Is(err,context.Canceled) {t.Fatalf("wanted graceful context cancellation, got %v",err)}
	if checks!=3 {t.Fatalf("wrong canceled polling count: %d",checks)}
}

// True source PTS may be irregular (VFR); the Worker must not describe those
// timings as a nominal grid, and mixed/legacy frames must be labeled separately.
func TestT12FrameKindsShownIndividuallyToWorker(t *testing.T) {
	id:="art_"+strings.Repeat("d",32)
	first:="art_"+strings.Repeat("e",32)
	second:="art_"+strings.Repeat("f",32)
	webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	original:=[]byte("source video")
	digest:=func(data []byte)string{sum:=sha256.Sum256(data);return hex.EncodeToString(sum[:])}
	fetch:=func(_ context.Context,key string)(ArtifactPayload,error) {
		if key==id {
			return ArtifactPayload{ID:id,Name:"vfr.mp4",Data:original,SHA256:digest(original),
				VideoStatus:"ready",
				VideoFrames:[]core.VideoFrame{
					{ArtifactID:first,TimestampMS:0,TimestampKind:"nominal-grid"},
					{ArtifactID:second,TimestampMS:1250,TimestampKind:"source-pts-relative"},
				}},nil
		}
		return ArtifactPayload{ID:key,Name:"frame.webp",Data:webp,SHA256:digest(webp)},nil
	}
	task,images,err:=PrepareArtifactInput(context.Background(),t.TempDir(),"Describe artifact://"+id,fetch)
	if err!=nil{t.Fatal(err)}
	if len(images)!=2 {t.Fatalf("frame image count %d",len(images))}
	if !strings.Contains(task,"approximate nominal-grid sample time 0.000 s [timestamp_kind=nominal-grid; NOT actual source-frame PTS]") ||
		!strings.Contains(task,"source-frame PTS (relative to clip start) 1.250 s [timestamp_kind=source-pts-relative]") {
		t.Fatalf("mixed timestamp semantics lost in native Worker prompt: %s",task)
	}
}

func TestT12QueuedVideoHTTP503ThenReconnectReadyNativeDispatchOnce(t *testing.T) {
	id:="art_"+strings.Repeat("a",32)
	frameID:="art_"+strings.Repeat("b",32)
	data:=[]byte("original")
	webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	digest:=func(data []byte)string{v:=sha256.Sum256(data);return hex.EncodeToString(v[:])}
	var statusRequests,downloads,frameDownloads int
	fetch:=func(ctx context.Context,key string)(ArtifactPayload,error) {
		switch key {
		case id:
			downloads++
			return ArtifactPayload{ID:id,Name:"clip.mp4",Data:data,SHA256:digest(data),VideoStatus:"queued",
				CheckVideoStatus:func(ctx context.Context)(core.Artifact,error){
					statusRequests++
					switch statusRequests {
					case 1,2: return core.Artifact{},fmt.Errorf("%w: HTTP 503",ErrTransientArtifactStatus)
					case 3: return core.Artifact{ID:id,ProcessingStatus:"queued"},nil
					case 4: return core.Artifact{},fmt.Errorf("%w: server reconnect",ErrTransientArtifactStatus)
					default: return core.Artifact{ID:id,ProcessingStatus:"ready",VideoFrames:[]core.VideoFrame{
						{ArtifactID:frameID,TimestampMS:1250,TimestampKind:"source-pts-relative"},
					}},nil
					}
				}},nil
		case frameID:
			frameDownloads++
			return ArtifactPayload{ID:frameID,Name:"frame.webp",Data:webp,SHA256:digest(webp)},nil
		}
		return ArtifactPayload{},errors.New("unrecognized artifact")
	}
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	task,images,err:=prepareArtifactInputWithPoll(ctx,t.TempDir(),"Describe artifact://"+id,fetch,0)
	if err!=nil{t.Fatalf("transient metadata failure permanently rejected dispatch: %v",err)}
	if downloads!=1 || statusRequests!=5 || frameDownloads!=1 || len(images)!=1 || !strings.Contains(task,"1.250 s [timestamp_kind=source-pts-relative]"){
		t.Fatalf("native dispatch duplicated or missing: original=%d status=%d frames=%d images=%d task=%q",downloads,statusRequests,frameDownloads,len(images),task)
	}
}

func TestT12MetadataFailedIsTerminalNotTransient(t *testing.T) {
	id:="art_"+strings.Repeat("c",32)
	var requests int
	a:=ArtifactPayload{ID:id,VideoStatus:"processing",CheckVideoStatus:func(context.Context)(core.Artifact,error){
		requests++
		return core.Artifact{ID:id,ProcessingStatus:"failed",ProcessingError:"invalid media"},nil
	}}
	if err:=awaitVideoProcessing(context.Background(),id,&a,0);err!=nil{t.Fatal(err)}
	if requests!=1 || a.VideoStatus!="failed"{t.Fatalf("conversion failure was retried: %+v %d",a,requests)}
}

func TestT12SignedVideoMetadata503IsTransientBut404IsPermanent(t *testing.T) {
	id:="art_"+strings.Repeat("d",32)
	var requests int
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.Header.Get("X-Artifact-Signature")=="" || r.Header.Get("X-Secretary-Node")!="macbook" {
			http.Error(w,"missing signed Node credentials",http.StatusUnauthorized);return
		}
		requests++
		switch requests {
		case 1:
			http.Error(w,"Server restarting",http.StatusServiceUnavailable)
		case 2:
			w.Header().Set("Content-Type","application/json")
			_,_=fmt.Fprintf(w,`{"id":%q,"processing_status":"ready"}`,id)
		default:
			http.NotFound(w,r)
		}
	}))
	defer server.Close()
	credential:=base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42},32))
	identity:=NodeIdentity{
		Node:"macbook",Credential:credential,
		ConnectURL:"ws"+strings.TrimPrefix(server.URL,"http")+"/v1/nodes/connect",
	}
	ctx:=context.Background()
	if _,err:=identity.FetchArtifactStatus(ctx,id);!errors.Is(err,ErrTransientArtifactStatus) {
		t.Fatalf("signed HTTP 503 must retry, got %v",err)
	}
	ready,err:=identity.FetchArtifactStatus(ctx,id)
	if err!=nil || ready.ID!=id || ready.ProcessingStatus!="ready" {
		t.Fatalf("signed 503→200 retry failed: %+v %v",ready,err)
	}
	if _,err:=identity.FetchArtifactStatus(ctx,id);err==nil||errors.Is(err,ErrTransientArtifactStatus) {
		t.Fatalf("HTTP 404 must remain terminal, got %v",err)
	}
}

func TestT12DispatchAfter503StartsExactlyOneNativeSession(t *testing.T) {
	id:="art_"+strings.Repeat("e",32)
	frameID:="art_"+strings.Repeat("f",32)
	webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err!=nil{t.Fatal(err)}
	original:=[]byte("video")
	digest:=func(data []byte)string{hash:=sha256.Sum256(data);return hex.EncodeToString(hash[:])}
	checked:=0
	fetched:=0
	local,err:=OpenLocalStore(filepath.Join(t.TempDir(),"state.json"))
	if err!=nil{t.Fatal(err)}
	defer local.Close()
	runtime:=&daemonCountingRuntime{}
	execution:=NewExecutionNode("macbook",runtime,local)
	execution.SetArtifactFetcher(func(ctx context.Context,key string)(ArtifactPayload,error){
		if key==frameID{return ArtifactPayload{ID:frameID,Name:"frame.webp",Data:webp,SHA256:digest(webp)},nil}
		if key!=id{return ArtifactPayload{},errors.New("unrecognized original")}
		fetched++
		return ArtifactPayload{
			ID:id,Name:"clip.mp4",Data:original,SHA256:digest(original),VideoStatus:"queued",
			CheckVideoStatus:func(context.Context)(core.Artifact,error){
				checked++
				if checked==1 {return core.Artifact{},fmt.Errorf("%w: HTTP 503",ErrTransientArtifactStatus)}
				return core.Artifact{ID:id,ProcessingStatus:"ready",VideoFrames:[]core.VideoFrame{
					{ArtifactID:frameID,TimestampMS:0,TimestampKind:"source-pts-relative"},
				}},nil
			},
		},nil
	})
	cmd:=daemonDispatchFixture("macbook","dispatch-after-503")
	cmd.Dispatch.Envelope.OriginalUserIntent="Inspect artifact://"+id
	cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution=append(cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution,core.CapabilityImageInput)
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	result,err:=execution.HandleCommand(ctx,cmd)
	if err!=nil||result.State!=CommandAccepted {
		t.Fatalf("native Worker dispatch rejected after recoverable status error: %+v %v",result,err)
	}
	if fetched!=1 || checked!=2 || runtime.starts.Load()!=1 {
		t.Fatalf("dispatch was lost or replayed: original=%d status=%d starts=%d",fetched,checked,runtime.starts.Load())
	}
}
