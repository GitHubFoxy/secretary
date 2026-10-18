package webapi

import (
	"bytes"
	"image"
	_ "golang.org/x/image/webp"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func t12RealVFRClip(t *testing.T) []byte {
	t.Helper()
	for _,tool:=range []string{"ffmpeg","ffprobe"} {
		if _,err:=exec.LookPath(tool);err!=nil{t.Fatalf("%s must be installed for non-skippable real transcoder acceptance",tool)}
	}
	filename:=filepath.Join(t.TempDir(),"offset_vfr.mp4")
	// Nonzero first PTS=5s and a half-second VFR gap in the middle.
	cmd:=exec.Command("ffmpeg","-hide_banner","-v","error",
		"-f","lavfi","-i","testsrc2=size=32x24:rate=4:duration=2",
		"-vf","setpts=PTS+5/TB+if(gte(N\\,3)\\,0.5/TB\\,0)",
		"-fps_mode","vfr","-c:v","mpeg4","-q:v","3",filename)
	if out,err:=cmd.CombinedOutput();err!=nil{t.Fatalf("generate VFR fixture: %v %s",err,out)}
	data,err:=os.ReadFile(filename)
	if err!=nil{t.Fatal(err)}
	return data
}

func TestT12RealFFmpegNonzeroPTSAndVFRSourcePTS(t *testing.T) {
	clip:=t12RealVFRClip(t)
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
	defer cancel()
	frames,err:=(ffmpegVideoProcessor{}).Process(ctx,clip,VideoOptions{FPS:1,MaxDurationSeconds:12,MaxFrames:12,TimeoutSeconds:20})
	if err!=nil{t.Fatalf("VFR sample should work: %v",err)}
	if len(frames)!=2 {t.Fatalf("expected 2 actual source samples from VFR clip, got %d",len(frames))}
	for i,frame:=range frames {
		expected:=[]int64{0,1250}
		if frame.TimestampMS!=expected[i] || frame.TimestampKind!="source-pts-relative" {
			t.Fatalf("frame %d: got actual PTS %d (%s), want %d source-pts-relative",i,frame.TimestampMS,frame.TimestampKind,expected[i])
		}
		valid,e:=node.ValidateStillImage("frame.webp",frame.Data)
		if e!=nil||valid.MediaType!="image/webp"{t.Fatalf("invalid sampled WebP: %v",e)}
	}
	_,err=(ffmpegVideoProcessor{}).Process(ctx,clip,VideoOptions{FPS:1,MaxDurationSeconds:1,MaxFrames:12,TimeoutSeconds:20})
	if err==nil||!strings.Contains(err.Error(),"limits"){t.Fatalf("duration must be rejected, not clipped: %v",err)}
	_,err=(ffmpegVideoProcessor{}).Process(ctx,clip,VideoOptions{FPS:1,MaxDurationSeconds:12,MaxFrames:2,TimeoutSeconds:20})
	if err==nil||!strings.Contains(err.Error(),"limits"){t.Fatalf("frame ceiling must reject, not truncate: %v",err)}
	_,err=(ffmpegVideoProcessor{}).Process(ctx,bytes.Repeat([]byte("broken"),100),VideoOptions{FPS:1,MaxDurationSeconds:12,MaxFrames:12,TimeoutSeconds:20})
	if err==nil{t.Fatal("broken media accepted")}
}

func TestT12ServerRestartsUnfinishedVideoJobAndPreservesOriginal(t *testing.T) {
	clip:=t12RealVFRClip(t)
	dbpath:=filepath.Join(t.TempDir(),"restart.db")
	ctx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
	defer cancel()
	store,err:=core.Open(ctx,dbpath)
	if err!=nil{t.Fatal(err)}
	person,conv,err:=store.EnsureOwner(ctx)
	if err!=nil{t.Fatal(err)}
	source,dup,err:=store.SaveVideoArtifact(ctx,person.ID,conv.ID,"restart-video","sample.mp4","video/mp4",clip,1,12,12,20)
	if err!=nil||dup{t.Fatalf("atomic source: %+v %v",source,err)}
	_,claimed,err:=store.ClaimVideoJob(ctx,person.ID)
	if err!=nil||!claimed{t.Fatalf("could not simulate crash during conversion: %v",err)}
	if err=store.Close();err!=nil{t.Fatal(err)}
	reopened,err:=core.Open(ctx,dbpath)
	if err!=nil{t.Fatal(err)}
	defer reopened.Close()
	server,err:=New(ctx,reopened,"bootstrap")
	if err!=nil{t.Fatal(err)}
	if server.OwnerID()!=person.ID{t.Fatal("server owner changed")}
	for {
		meta,e:=reopened.ArtifactMetadataForPerson(ctx,person.ID,source.ID)
		if e!=nil && !errors.Is(e,core.ErrNotFound){t.Fatal(e)}
		if e==nil && meta.ProcessingStatus=="ready"{
			if len(meta.VideoFrames)!=2 || meta.VideoFrames[1].TimestampMS!=1250 || meta.VideoFrames[1].TimestampKind!="source-pts-relative" {
				t.Fatalf("recovered frames: %+v",meta.VideoFrames)
			}
			saved,original,e:=reopened.ArtifactForPerson(ctx,person.ID,source.ID)
			if e!=nil || saved.ID!=source.ID || !bytes.Equal(original,clip){t.Fatalf("original lost across conversion: %v",e)}
			return
		}
		if e==nil && meta.ProcessingStatus=="failed"{t.Fatalf("recovery failed: %s",meta.ProcessingError)}
		select{case <-ctx.Done():t.Fatalf("recovery never completed: %v",ctx.Err());case <-time.After(25*time.Millisecond):}
	}
}

func TestT12FFmpegExtractsExactlyProbedVideoTrackNotDefault(t *testing.T) {
	for _,name:=range []string{"ffmpeg","ffprobe"} {
		if _,err:=exec.LookPath(name);err!=nil{t.Fatalf("mandatory video track-selection acceptance needs %s: %v",name,err)}
	}
	path:=filepath.Join(t.TempDir(),"two-tracks.mp4")
	// v:0 is a tiny RED video. v:1 is a BLUE default stream at a different
	// resolution. An unqualified FFmpeg output selects the wrong (default)
	// stream even though ffprobe validates v:0.
	cmd:=exec.Command("ffmpeg","-nostdin","-hide_banner","-loglevel","error",
		"-f","lavfi","-i","color=c=red:s=32x24:r=1:d=2",
		"-f","lavfi","-i","color=c=blue:s=256x192:r=1:d=2",
		"-map","0:v:0","-map","1:v:0","-c:v","mpeg4",
		"-disposition:v:0","0","-disposition:v:1","default",path)
	if output,err:=cmd.CombinedOutput();err!=nil{t.Fatalf("produce two video streams: %v %s",err,output)}
	clip,err:=os.ReadFile(path)
	if err!=nil{t.Fatal(err)}
	ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second)
	defer cancel()
	frames,err:=(ffmpegVideoProcessor{}).Process(ctx,clip,VideoOptions{FPS:1,MaxFrames:12,MaxDurationSeconds:12,TimeoutSeconds:20})
	if err!=nil{t.Fatalf("transcode mapped video track: %v",err)}
	if len(frames)!=2{t.Fatalf("unexpected sample count from v:0: %d",len(frames))}
	for i,frame:=range frames {
		img,format,err:=image.Decode(bytes.NewReader(frame.Data))
		if err!=nil || format!="webp"{t.Fatalf("frame %d not WebP: %s %v",i,format,err)}
		bounds:=img.Bounds()
		if bounds.Dx()!=32 || bounds.Dy()!=24{
			t.Fatalf("frame %d extracted wrong default v:1 dimensions %dx%d",i,bounds.Dx(),bounds.Dy())
		}
		r,g,b,_:=img.At(16,12).RGBA()
		if r<45000 || g>14000 || b>14000 {
			t.Fatalf("frame %d extracted blue default track rather than red probed v:0: rgb=%d,%d,%d",i,r,g,b)
		}
	}
	_,err=(ffmpegVideoProcessor{}).Process(ctx,clip,VideoOptions{FPS:1,MaxFrames:1,MaxDurationSeconds:12,TimeoutSeconds:20})
	if err==nil || !strings.Contains(err.Error(),"limits"){t.Fatalf("v:0 frame ceiling not enforced: %v",err)}
}
