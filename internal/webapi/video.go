package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type VideoOptions struct {
	FPS float64
	MaxDurationSeconds int
	MaxFrames int
	TimeoutSeconds int
}

func (v VideoOptions) Defaults() VideoOptions {
	if v.FPS == 0 { v.FPS = 1 }
	if v.MaxDurationSeconds == 0 { v.MaxDurationSeconds = 12 }
	if v.MaxFrames == 0 { v.MaxFrames = 12 }
	if v.TimeoutSeconds == 0 { v.TimeoutSeconds = 45 }
	return v
}

func (v VideoOptions) Validate() error {
	if v.FPS < .25 || v.FPS > 4 || v.MaxDurationSeconds < 1 || v.MaxDurationSeconds > 60 ||
		v.MaxFrames < 1 || v.MaxFrames > 16 || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 120 {
		return errors.New("video: fps must be 0.25–4, duration 1–60 s, frames 1–16, timeout 1–120 s")
	}
	return nil
}

type VideoProcessor interface {
	Process(context.Context, []byte, VideoOptions) ([]core.VideoFrameData,error)
}

type ffmpegVideoProcessor struct{}

func (p ffmpegVideoProcessor) Process(ctx context.Context, original []byte, options VideoOptions) ([]core.VideoFrameData,error) {
	if err:=options.Validate();err!=nil{return nil,err}
	dir,err:=os.MkdirTemp("","secretary-video-*")
	if err!=nil{return nil,err}
	defer os.RemoveAll(dir)
	input:=filepath.Join(dir,"original.video")
	if err=os.WriteFile(input,original,0600);err!=nil{return nil,err}
	probe:=exec.CommandContext(ctx,"ffprobe","-v","error","-select_streams","v:0","-show_entries","stream=codec_type,width,height,duration:format=duration","-of","json",input)
	raw,err:=probe.Output()
	if err!=nil{return nil,fmt.Errorf("video: ffprobe failed: %w",err)}
	var info struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width int `json:"width"`
			Height int `json:"height"`
			Duration string `json:"duration"`
		} `json:"streams"`
		Format struct {Duration string `json:"duration"`} `json:"format"`
	}
	if err=json.Unmarshal(raw,&info);err!=nil{return nil,fmt.Errorf("video: unreadable media metadata: %w",err)}
	if len(info.Streams)!=1 || info.Streams[0].Width<1 || info.Streams[0].Height<1 ||
		info.Streams[0].Width>8192 || info.Streams[0].Height>8192 ||
		int64(info.Streams[0].Width)*int64(info.Streams[0].Height)>24_000_000 {
		return nil,errors.New("video: no supported video stream or frame dimensions exceed limits")
	}
	durationValue:=info.Streams[0].Duration
	if durationValue=="" || durationValue=="N/A" {durationValue=info.Format.Duration}
	duration,err:=strconv.ParseFloat(durationValue,64)
	if err!=nil || math.IsNaN(duration) || math.IsInf(duration,0) || duration<=0 {
		return nil,errors.New("video: unknown or invalid duration")
	}
	count:=int(math.Ceil(duration*options.FPS - 0.000001))
	if duration>float64(options.MaxDurationSeconds) || count<1 || count>options.MaxFrames {
		return nil,fmt.Errorf("video: %.2f s requires %d frames at %.2f FPS; limits are %d s and %d frames",duration,count,options.FPS,options.MaxDurationSeconds,options.MaxFrames)
	}
	pattern:=filepath.Join(dir,"frame_%05d.webp")
	rate:=strconv.FormatFloat(options.FPS,'f',-1,64)
	filter:="setpts=PTS-STARTPTS,fps="+rate+":start_time=0,scale=w='min(960,iw)':h='min(960,ih)':force_original_aspect_ratio=decrease"
	command:=exec.CommandContext(ctx,"ffmpeg","-nostdin","-hide_banner","-loglevel","error","-threads","1","-i",input,
		"-an","-vf",filter,"-frames:v",strconv.Itoa(count+1),"-c:v","libwebp","-quality","70",pattern)
	output,err:=command.CombinedOutput()
	if err!=nil{return nil,fmt.Errorf("video: transcoding failed: %w (%s)",err,strings.TrimSpace(string(output)))}
	files,err:=filepath.Glob(filepath.Join(dir,"frame_*.webp"))
	if err!=nil{return nil,err}
	sort.Strings(files)
	if len(files)!=count {return nil,fmt.Errorf("video: extracted %d frames, expected %d; rejecting incomplete/extra samples",len(files),count)}
	frames:=make([]core.VideoFrameData,0,count)
	for i,p:=range files {
		data,err:=os.ReadFile(p)
		if err!=nil{return nil,err}
		if _,err=node.ValidateStillImage(filepath.Base(p),data);err!=nil{return nil,fmt.Errorf("video frame %d: %w",i,err)}
		timestamp:=int64(math.Round(float64(i)*1000/options.FPS))
		frames=append(frames,core.VideoFrameData{TimestampMS:timestamp,Data:data})
	}
	return frames,nil
}

func (s *Server) AttachVideoOptions(options VideoOptions) error {
	options=options.Defaults()
	if err:=options.Validate();err!=nil{return err}
	s.videoOptions=options
	return nil
}

func (s *Server) AttachVideoProcessor(processor VideoProcessor) { s.videoProcessor=processor }

func (s *Server) recoverVideoJobs(ctx context.Context) error {
	if err:=s.store.RecoverVideoJobs(ctx,s.owner.ID);err!=nil{return err}
	jobs,err:=s.store.PendingVideoJobs(ctx,s.owner.ID)
	if err!=nil{return err}
	if len(jobs)>0 {s.signalVideoWork()}
	return nil
}

// A durable SQLite queue allows 32 pending jobs, and exactly two workers run
// ffmpeg concurrently. Duplicate uploads only wake the scheduler.
func (s *Server) signalVideoWork() {
	s.videoWorkersOnce.Do(func(){
		for i:=0;i<2;i++ {go s.videoWorkLoop()}
	})
	for i:=0;i<2;i++ {
		select {case s.videoWake<-struct{}{}:default:}
	}
}

func (s *Server) videoWorkLoop() {
	for range s.videoWake {
		for {
			ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
			job,found,err:=s.store.ClaimVideoJob(ctx,s.owner.ID)
			cancel()
			if err!=nil {time.Sleep(100*time.Millisecond);s.signalVideoWork();break}
			if !found {break}
			s.processVideo(job)
		}
	}
}

func (s *Server) processVideo(job core.VideoJob) {
	ctx,cancel:=context.WithTimeout(context.Background(),time.Duration(job.TimeoutSeconds)*time.Second)
	defer cancel()
	_,bytes,err:=s.store.ArtifactForPerson(ctx,job.PersonID,job.ArtifactID)
	if err!=nil { _=s.store.FailVideo(context.Background(),job.PersonID,job.ArtifactID,"video: original unavailable: "+err.Error());return }
	p:=s.videoProcessor
	if p==nil {p=ffmpegVideoProcessor{}}
	frames,err:=p.Process(ctx,bytes,VideoOptions{FPS:job.FPS,MaxFrames:job.MaxFrames,MaxDurationSeconds:job.MaxDuration,TimeoutSeconds:job.TimeoutSeconds})
	if err==nil && ctx.Err()!=nil {err=ctx.Err()}
	if err==nil {err=s.store.CompleteVideo(context.Background(),job.PersonID,job.ArtifactID,frames)}
	if err!=nil {
		message:=err.Error()
		if ctx.Err()!=nil {message="video processing timeout: "+ctx.Err().Error()}
		_=s.store.FailVideo(context.Background(),job.PersonID,job.ArtifactID,message)
	}
}
