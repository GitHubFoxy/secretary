package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	if len(images)!=2||!strings.Contains(text,"clip-relative sample time"){t.Fatalf("missing frames or timestamp semantics: %d %q",len(images),text)}
	if originalDownloads!=1||frameDownloads!=2||statusRequests!=3 {t.Fatalf("transfers: original=%d frames=%d metadata=%d",originalDownloads,frameDownloads,statusRequests)}
	if bytesTransferred!=len(original)+2*len(webp) {t.Fatalf("retransferred full original: %d bytes",bytesTransferred)}
}
