package core

import (
 "context"
 "path/filepath"
 "testing"
)

func TestVideoOneHundredFortyFramesPersistAndRejectOneHundredFortyOne(t *testing.T) {
 ctx:=context.Background()
 store,err:=Open(ctx,filepath.Join(t.TempDir(),"video.db"))
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 person,conversation,err:=store.EnsureOwner(ctx)
 if err!=nil{t.Fatal(err)}
 original,dup,err:=store.SaveVideoArtifact(ctx,person.ID,conversation.ID,"large-frame-job","clip.mp4","video/mp4",[]byte("small original"),2,MaxVideoFrames,70,120)
 if err!=nil||dup{t.Fatalf("create 140-frame video job: %v, duplicate=%v",err,dup)}
 if _,ok,err:=store.ClaimVideoJob(ctx,person.ID);err!=nil||!ok{t.Fatalf("claim video job: %v, ok=%v",err,ok)}
 frames:=make([]VideoFrameData,MaxVideoFrames)
 for i:=range frames{frames[i]=VideoFrameData{TimestampMS:int64(500*i),TimestampKind:"source-pts-relative",Data:[]byte("frame")}}
 if err:=store.CompleteVideo(ctx,person.ID,original.ID,frames);err!=nil{t.Fatalf("persist 140 frames: %v",err)}
 meta,err:=store.ArtifactMetadataForPerson(ctx,person.ID,original.ID)
 if err!=nil||len(meta.VideoFrames)!=MaxVideoFrames{t.Fatalf("load 140 frames: count=%d err=%v",len(meta.VideoFrames),err)}
 if err:=store.CompleteVideo(ctx,person.ID,original.ID,append(frames,VideoFrameData{TimestampMS:70000,Data:[]byte("frame")}));err==nil{t.Fatal("141 frames accepted")}
 if _,_,err:=store.SaveVideoArtifact(ctx,person.ID,conversation.ID,"invalid-frame-job","clip.mp4","video/mp4",[]byte("small original"),2,MaxVideoFrames+1,70,120);err==nil{t.Fatal("141 frame job accepted")}
}
