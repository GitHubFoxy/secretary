package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
