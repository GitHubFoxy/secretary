package node

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "fmt"
 "strings"
 "testing"

 "github.com/beruseruko/secretary/internal/core"
)

func TestVideoOneHundredFortyFramesReachWorkerNativePrompt(t *testing.T) {
 webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
 if err!=nil{t.Fatal(err)}
 originalID:="art_"+strings.Repeat("a",32)
 original:=[]byte("video original")
 digest:=func(b []byte)string{sum:=sha256.Sum256(b);return hex.EncodeToString(sum[:])}
 frames:=make([]core.VideoFrame,core.MaxVideoFrames)
 for i:=range frames {
  frames[i]=core.VideoFrame{ArtifactID:fmt.Sprintf("art_%032x",i+1),TimestampMS:int64(i*500),TimestampKind:"source-pts-relative"}
 }
 fetch:=func(_ context.Context,id string)(ArtifactPayload,error){
  if id==originalID{return ArtifactPayload{ID:id,Name:"clip.mp4",Data:original,SHA256:digest(original),VideoStatus:"ready",VideoFrames:frames},nil}
  for _,f:=range frames {if f.ArtifactID==id{return ArtifactPayload{ID:id,Name:"frame.webp",Data:webp,SHA256:digest(webp)},nil}}
  return ArtifactPayload{},fmt.Errorf("unrecognized frame %q",id)
 }
 task,images,err:=PrepareArtifactInput(context.Background(),t.TempDir(),"Analyze artifact://"+originalID,fetch)
 if err!=nil{t.Fatal(err)}
 if len(images)!=core.MaxVideoFrames || strings.Count(task,"timestamp_kind=source-pts-relative")!=core.MaxVideoFrames{
  t.Fatalf("Worker received %d frames and %d timestamps; want %d",len(images),strings.Count(task,"timestamp_kind=source-pts-relative"),core.MaxVideoFrames)
 }
 if !bytes.Equal(images[core.MaxVideoFrames-1].Data,webp){t.Fatal("last frame was lost")}
 frames=append(frames,core.VideoFrame{ArtifactID:fmt.Sprintf("art_%032x",core.MaxVideoFrames+1),TimestampMS:70000,TimestampKind:"source-pts-relative"})
 if _,_,err:=PrepareArtifactInput(context.Background(),t.TempDir(),"Analyze artifact://"+originalID,fetch);err==nil||!strings.Contains(err.Error(),"at most"){
  t.Fatalf("141 frames were accepted: %v",err)
 }
}
