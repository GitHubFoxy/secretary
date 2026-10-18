package telegram

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "path/filepath"
 "strconv"
 "strings"
 "time"

 "github.com/beruseruko/secretary/internal/core"
)

// Video is the original Telegram video or circular video note, never a transcoded copy.
type Video struct {
 FileID string `json:"file_id"`
 FileUniqueID string `json:"file_unique_id,omitempty"`
 FileName string `json:"file_name,omitempty"`
 MimeType string `json:"mime_type,omitempty"`
 FileSize int64 `json:"file_size,omitempty"`
 Duration int `json:"duration,omitempty"`
}

type VideoArtifactStore interface {
 StoreVideoOriginal(context.Context, int64, Message, string, []byte) (string,error)
 VideoStatus(context.Context,string) (string,string,error)
 VideoLimits(context.Context) (int,error)
}

func telegramVideoName(name string) bool {
 if len(name)==0 || len(name)>180 || strings.HasPrefix(name,".") || filepath.Base(name)!=name || strings.ContainsAny(name,"/\\") { return false }
 switch strings.ToLower(filepath.Ext(name)) { case ".mp4",".webm",".mov": return true }
 return false
}

func (a *Adapter) deliverVideoUpdate(ctx context.Context,u Update,allowed bool) error {
 if !allowed || !a.imageAuthorized(u) { return nil }
 m:=u.Message
 fail:=func(reason string)error{return a.imageNotice(ctx,u,"Не удалось обработать видео: "+reason)}
 if !a.imageRequestStillCurrent(u) { return fail("вопрос Worker изменился; отправьте видео снова.") }
 if m.Video==nil || m.Video.FileID=="" { return fail("Telegram не передал video file_id.") }
 if m.Video.FileSize<0 || m.Video.FileSize>core.MaxArtifactBytes { return fail("файл превышает лимит 8 МиБ.") }
 if !telegramVideoName(m.Video.FileName) { return fail("поддерживаются только MP4, WebM и MOV.") }
 store,ok:=a.server.(VideoArtifactStore)
 if !ok { return fail("Server video pipeline недоступен.") }
 maxDuration,limitErr:=store.VideoLimits(ctx)
 if limitErr!=nil { return fmt.Errorf("telegram video limits: %w",limitErr) }
 if m.Video.Duration > maxDuration { return fail(fmt.Sprintf("длительность видео превышает настроенный предел %d секунд.",maxDuration)) }
 download,ok:=a.transport.(VoiceDownloadTransport)
 if !ok { return fail("Bot API download недоступен.") }
 downloadCtx,stop:=context.WithTimeout(ctx,30*time.Second)
 file,err:=download.GetFile(downloadCtx,m.Video.FileID)
 if err!=nil {
  stop()
  if errors.Is(err,ErrTelegramFileRejected)||errors.Is(err,ErrFileTooLarge) {return fail("Telegram отклонил исходный файл.")}
  return fmt.Errorf("telegram video getFile: %w",err)
 }
 if file.FileSize<0 || file.FileSize>core.MaxArtifactBytes {stop();return fail("файл превышает лимит 8 МиБ.")}
 data,err:=download.DownloadFile(downloadCtx,file.FilePath,core.MaxArtifactBytes)
 stop()
 if err!=nil {
  if ctx.Err()!=nil{return ctx.Err()}
  if errors.Is(err,ErrFileTooLarge) {return fail("файл превышает лимит 8 МиБ.")}
  if errors.Is(err,ErrTelegramDownloadRejected) {return fail("Telegram отклонил скачивание.")}
  return fmt.Errorf("telegram video download: %w",err)
 }
 if len(data)==0||len(data)>core.MaxArtifactBytes {return fail("пустой или слишком большой файл.")}
 if !a.imageAuthorized(u)||!a.imageRequestStillCurrent(u) {return fail("авторизация или назначение Worker изменилось.") }
 artifactID,err:=store.StoreVideoOriginal(ctx,u.ID,*m,m.Video.FileName,data)
 if err!=nil {
  var rejection *ImageSourceRejection
  if errors.As(err,&rejection) { return fail(rejection.Reason) }
  return fmt.Errorf("telegram video artifact: %w",err)
 }
 if artifactID=="" {return errors.New("telegram video: missing Artifact ID")}
 // Server performs the one shared T12 conversion. Wait for a terminal status
 // before dispatch, so conversion failures cannot be reported as analyzed.
 ticker:=time.NewTicker(250*time.Millisecond)
 defer ticker.Stop()
 for {
  status,reason,e:=store.VideoStatus(ctx,artifactID)
  if e!=nil {
   var rejection *ImageSourceRejection
   if errors.As(e,&rejection) {return fail(rejection.Reason)}
   return fmt.Errorf("telegram video status: %w",e)
  }
  if status=="failed" {return fail("конвертация не удалась: "+reason)}
  if status=="ready" {break}
  if status!="queued"&&status!="processing" {return fail("неизвестное состояние обработки видео.")}
  select {case <-ctx.Done():return ctx.Err();case <-ticker.C:}
 }
 if !a.imageAuthorized(u)||!a.imageRequestStillCurrent(u) {return fail("авторизация или назначение Worker изменилось.") }
 caption:=strings.TrimSpace(m.Caption)
 if caption=="" {caption="Проанализируй видео по кадрам и временным меткам."}
 prompt:=fmt.Sprintf("%s\n\nTelegram video (update %d, chat %d, message %d, file_unique_id %s): artifact://%s",caption,u.ID,m.ChatID,m.MessageID,m.Video.FileUniqueID,artifactID)
 copyMessage:=*m
 copyMessage.Video=nil
 copyMessage.Text=prompt
 u.Message=&copyMessage
 return a.deliverTextUpdate(ctx,u,allowed,u.WorkerRequestID)
}

func (c *HTTPServerClient) StoreVideoOriginal(ctx context.Context,updateID int64,m Message,name string,data []byte)(string,error) {
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,strings.TrimRight(c.BaseURL,"/")+"/v1/internal/telegram/video/source",bytes.NewReader(data))
 if err!=nil{return "",err}
 req.Header.Set("Authorization","Bearer "+c.Credential)
 req.Header.Set("Content-Type","application/octet-stream")
 req.Header.Set("X-Video-Update-ID",strconv.FormatInt(updateID,10))
 req.Header.Set("X-Video-Chat-ID",strconv.FormatInt(m.ChatID,10))
 req.Header.Set("X-Video-Message-ID",strconv.FormatInt(m.MessageID,10))
 req.Header.Set("X-Video-Name",name)
 req.Header.Set("X-Video-Mime-Type",m.Video.MimeType)
 res,err:=c.client().Do(req)
 if err!=nil{return "",err}
 defer res.Body.Close()
 body,e:=io.ReadAll(io.LimitReader(res.Body,maxServerErrorBody+1))
 if e!=nil{return "",e}
 if len(body)>maxServerErrorBody{return "",errors.New("video source response exceeds limit")}
 if res.StatusCode<200||res.StatusCode>=300 {
  switch res.StatusCode {case 400,409,413,415,422,429:return "",&ImageSourceRejection{Reason:strings.TrimSpace(string(body))}}
  return "",fmt.Errorf("video Artifact source HTTP %d",res.StatusCode)
 }
 var result struct{ Artifact struct{ID string `json:"id"`} `json:"artifact"` }
 if err=json.Unmarshal(body,&result);err!=nil{return "",err}
 if !strings.HasPrefix(result.Artifact.ID,"art_"){return "",errors.New("invalid video Artifact ID")}
 return result.Artifact.ID,nil
}

func (c *HTTPServerClient) VideoStatus(ctx context.Context,id string)(string,string,error) {
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,strings.TrimRight(c.BaseURL,"/")+"/v1/internal/telegram/video/status?id="+id,nil)
 if err!=nil{return "","",err}
 req.Header.Set("Authorization","Bearer "+c.Credential)
 res,err:=c.client().Do(req)
 if err!=nil{return "","",err}
 defer res.Body.Close()
 if res.StatusCode!=http.StatusOK {
  if res.StatusCode>=400&&res.StatusCode<500{return "","",&ImageSourceRejection{Reason:fmt.Sprintf("video status HTTP %d",res.StatusCode)}}
  return "","",fmt.Errorf("video status HTTP %d",res.StatusCode)
 }
 var result struct{ ProcessingStatus string `json:"processing_status"`; ProcessingError string `json:"processing_error"` }
 if err=json.NewDecoder(io.LimitReader(res.Body,16<<10)).Decode(&result);err!=nil{return "","",err}
 return result.ProcessingStatus,result.ProcessingError,nil
}

func (c *HTTPServerClient) VideoLimits(ctx context.Context)(int,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,strings.TrimRight(c.BaseURL,"/")+"/v1/internal/telegram/video/limits",nil)
 if err!=nil{return 0,err}
 req.Header.Set("Authorization","Bearer "+c.Credential)
 res,err:=c.client().Do(req)
 if err!=nil{return 0,err}
 defer res.Body.Close()
 if res.StatusCode!=http.StatusOK{return 0,fmt.Errorf("video limits HTTP %d",res.StatusCode)}
 var out struct{MaxDurationSeconds int `json:"max_duration_seconds"`}
 if err=json.NewDecoder(io.LimitReader(res.Body,1024)).Decode(&out);err!=nil{return 0,err}
 if out.MaxDurationSeconds<1{return 0,errors.New("invalid video duration configuration")}
 return out.MaxDurationSeconds,nil
}
