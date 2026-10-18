package webapi

import (
 "errors"
 "fmt"
 "io"
 "net/http"
 "path/filepath"
 "strconv"
 "strings"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/node"
)

// telegramVideoSource is a narrow authenticated ingress into the shared T12 queue.
// The Telegram bot never runs ffmpeg and has no independent artifact store.
func (s *Server) telegramVideoSource(w http.ResponseWriter,r *http.Request) {
 conversation,ok:=s.authorizedTelegramVoice(w,r)
 if !ok{return}
 updateID,e1:=strconv.ParseInt(r.Header.Get("X-Video-Update-ID"),10,64)
 chatID,e2:=strconv.ParseInt(r.Header.Get("X-Video-Chat-ID"),10,64)
 messageID,e3:=strconv.ParseInt(r.Header.Get("X-Video-Message-ID"),10,64)
 _,valid:=telegramVoiceIdentity(updateID,chatID,messageID)
 if e1!=nil||e2!=nil||e3!=nil||!valid{http.Error(w,"Invalid Telegram video identity",http.StatusBadRequest);return}
 name:=strings.TrimSpace(r.Header.Get("X-Video-Name"))
 if !artifactNameValid(name){http.Error(w,"Invalid video filename",http.StatusUnsupportedMediaType);return}
 mediaType:=""
 switch strings.ToLower(filepath.Ext(name)){
 case ".mp4":mediaType="video/mp4"
 case ".webm":mediaType="video/webm"
 case ".mov":mediaType="video/quicktime"
 default:http.Error(w,"Unsupported video format",http.StatusUnsupportedMediaType);return
 }
 declared:=strings.ToLower(strings.TrimSpace(r.Header.Get("X-Video-Mime-Type")))
 if declared!=""&&declared!="application/octet-stream"&&declared!="binary/octet-stream"&&declared!=mediaType{
  http.Error(w,"Video MIME and filename disagree",http.StatusUnsupportedMediaType);return
 }
 if r.ContentLength>core.MaxArtifactBytes{http.Error(w,"Video exceeds 8 MiB",http.StatusRequestEntityTooLarge);return}
 r.Body=http.MaxBytesReader(w,r.Body,core.MaxArtifactBytes+1)
 data,err:=io.ReadAll(r.Body)
 if err!=nil||len(data)>core.MaxArtifactBytes{http.Error(w,"Video exceeds 8 MiB",http.StatusRequestEntityTooLarge);return}
 if len(data)==0{http.Error(w,"Empty video",http.StatusBadRequest);return}
 key:=fmt.Sprintf("telegram:video:%d:%d:%d:source",updateID,chatID,messageID)
 opts:=s.videoOptions.Defaults()
 original,duplicate,err:=s.store.SaveVideoArtifact(r.Context(),conversation.PersonID,conversation.ID,key,name,mediaType,data,opts.FPS,opts.MaxFrames,opts.MaxDurationSeconds,opts.TimeoutSeconds)
 if errors.Is(err,core.ErrIdempotencyConflict){http.Error(w,"Conflicting Telegram video source identity",http.StatusConflict);return}
 if errors.Is(err,core.ErrVideoQueueFull){http.Error(w,"Video processing queue is full",http.StatusTooManyRequests);return}
 if err!=nil{http.Error(w,"Could not store Telegram video",http.StatusInternalServerError);return}
 s.signalVideoWork()
 original,err=s.store.ArtifactMetadataForPerson(r.Context(),conversation.PersonID,original.ID)
 if err!=nil{http.Error(w,"Could not read video metadata",http.StatusInternalServerError);return}
 status:=http.StatusCreated
 if duplicate{status=http.StatusOK}
 writeJSON(w,status,map[string]any{"artifact":original,"duplicate":duplicate})
}

func (s *Server) telegramVideoStatus(w http.ResponseWriter,r *http.Request){
 conversation,ok:=s.authorizedTelegramVoice(w,r)
 if !ok{return}
 id:=r.URL.Query().Get("id")
 if !node.ValidArtifactID(id){http.Error(w,"Invalid Artifact ID",http.StatusBadRequest);return}
 artifact,err:=s.store.ArtifactMetadataForPerson(r.Context(),conversation.PersonID,id)
 if errors.Is(err,core.ErrNotFound){http.NotFound(w,r);return}
 if err!=nil{http.Error(w,"Artifact status unavailable",http.StatusInternalServerError);return}
 if !strings.HasPrefix(artifact.MediaType,"video/"){http.Error(w,"Not a video",http.StatusBadRequest);return}
 writeJSON(w,http.StatusOK,artifact)
}

func (s *Server) telegramVideoLimits(w http.ResponseWriter,r *http.Request){
 _,ok:=s.authorizedTelegramVoice(w,r)
 if !ok{return}
 opts:=s.videoOptions.Defaults()
 writeJSON(w,http.StatusOK,map[string]any{"max_duration_seconds":opts.MaxDurationSeconds})
}
