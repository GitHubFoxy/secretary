package webapi

import (
 "errors"
 "fmt"
 "io"
 "mime"
 "net/http"
 "strconv"
 "strings"
 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/node"
)

func(s *Server)telegramDocumentSource(w http.ResponseWriter,r *http.Request){
 conversation,ok:=s.authorizedTelegramVoice(w,r);if !ok{return}
 update,e1:=strconv.ParseInt(r.Header.Get("X-Document-Update-ID"),10,64)
 chat,e2:=strconv.ParseInt(r.Header.Get("X-Document-Chat-ID"),10,64)
 msg,e3:=strconv.ParseInt(r.Header.Get("X-Document-Message-ID"),10,64)
 if _,valid:=telegramVoiceIdentity(update,chat,msg);!valid||e1!=nil||e2!=nil||e3!=nil{
  http.Error(w,"Invalid document source identity",400);return
 }
 name:=strings.TrimSpace(r.Header.Get("X-Document-Name"))
 if !artifactNameValid(name){http.Error(w,"Unsupported document filename or extension",415);return}
 if r.ContentLength>core.MaxArtifactBytes{http.Error(w,"Document exceeds 8 MiB",413);return}
 r.Body=http.MaxBytesReader(w,r.Body,core.MaxArtifactBytes+1)
 data,err:=io.ReadAll(r.Body)
 if err!=nil||len(data)==0||len(data)>core.MaxArtifactBytes{http.Error(w,"Document empty or exceeds 8 MiB",413);return}
 mediaType:=strings.TrimSpace(r.Header.Get("X-Document-Mime-Type"))
 if mediaType==""{mediaType=mime.TypeByExtension(strings.ToLower(name[strings.LastIndex(name,"."):]))}
 if mediaType==""{mediaType="application/octet-stream"}
 if len(mediaType)>160||strings.ContainsAny(mediaType,"\r\n"){http.Error(w,"Invalid document MIME",415);return}
 key:=fmt.Sprintf("telegram:document:%d:%d:%d:source",update,chat,msg)
 artifact,duplicate,err:=s.store.SaveArtifact(r.Context(),conversation.PersonID,conversation.ID,key,name,mediaType,data)
 if errors.Is(err,core.ErrIdempotencyConflict){http.Error(w,"Conflicting Telegram document source",409);return}
 if err!=nil{http.Error(w,"Document storage unavailable",500);return}
 status:=http.StatusCreated;if duplicate{status=http.StatusOK}
 writeJSON(w,status,map[string]any{"artifact":artifact,"reference":"artifact://"+artifact.ID,"duplicate":duplicate})
}

func(s *Server)telegramDocumentDownload(w http.ResponseWriter,r *http.Request){
 conversation,ok:=s.authorizedTelegramVoice(w,r);if !ok{return}
 id:=r.PathValue("id")
 if !node.ValidArtifactID(id){http.Error(w,"Invalid Artifact ID",400);return}
 artifact,body,err:=s.store.ArtifactForPerson(r.Context(),conversation.PersonID,id)
 if errors.Is(err,core.ErrNotFound){http.NotFound(w,r);return}
 if err!=nil{http.Error(w,"Artifact unavailable",500);return}
 w.Header().Set("Content-Type",artifact.MediaType)
 w.Header().Set("Content-Disposition",mime.FormatMediaType("attachment",map[string]string{"filename":artifact.Name}))
 w.Header().Set("Content-Length",strconv.Itoa(len(body)))
 _,_=w.Write(body)
}
