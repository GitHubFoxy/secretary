package telegram

import(
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
type DocumentArtifactStore interface {
 StoreDocumentOriginal(context.Context,int64,Message,string,[]byte)(string,error)
}
func (a *Adapter)deliverDocumentUpdate(ctx context.Context,u Update,allowed bool)error{
 if !allowed||!a.imageAuthorized(u){return nil}
 m:=u.Message
 fail:=func(reason string)error{return a.imageNotice(ctx,u,"Не удалось обработать документ: "+reason)}
 if !a.imageRequestStillCurrent(u){return fail("вопрос Worker изменился; отправьте документ заново.")}
 if m.Document==nil||m.Document.FileID==""{return fail("Telegram не передал file_id.")}
 d:=m.Document
 name:=strings.TrimSpace(d.FileName)
 if len(name)==0||len(name)>180||filepath.Base(name)!=name||strings.HasPrefix(name,".")||strings.ContainsAny(name,"/\\"){
  return fail("некорректное имя документа.")
 }
 if d.FileSize<0||d.FileSize>core.MaxArtifactBytes{return fail("превышен предел 8 МиБ.")}
 downloader,ok:=a.transport.(VoiceDownloadTransport);if !ok{return fail("Bot API download недоступен.")}
 store,ok:=a.server.(DocumentArtifactStore);if !ok{return fail("хранилище документов недоступно.")}
 downloadCtx,stop:=context.WithTimeout(ctx,30*time.Second)
 file,err:=downloader.GetFile(downloadCtx,d.FileID)
 if err!=nil{stop();if errors.Is(err,ErrTelegramFileRejected)||errors.Is(err,ErrFileTooLarge){return fail("Telegram отклонил файл.")};return fmt.Errorf("telegram document getFile: %w",err)}
 if file.FileSize<0||file.FileSize>core.MaxArtifactBytes{stop();return fail("превышен предел 8 МиБ.")}
 original,err:=downloader.DownloadFile(downloadCtx,file.FilePath,core.MaxArtifactBytes)
 stop()
 if err!=nil{
  if ctx.Err()!=nil{return ctx.Err()}
  if errors.Is(err,ErrTelegramDownloadRejected)||errors.Is(err,ErrFileTooLarge){return fail("загрузка документа отклонена или превышен предел.")}
  return fmt.Errorf("telegram document download: %w",err)
 }
 if len(original)==0||len(original)>core.MaxArtifactBytes{return fail("документ пустой или превышает 8 МиБ.")}
 if !a.imageAuthorized(u)||!a.imageRequestStillCurrent(u){return fail("вопрос Worker изменился во время загрузки.")}
 id,err:=store.StoreDocumentOriginal(ctx,u.ID,*m,name,original)
 if err!=nil{var rejected *ImageSourceRejection;if errors.As(err,&rejected){return fail(rejected.Reason)};return fmt.Errorf("telegram document store: %w",err)}
 caption:=strings.TrimSpace(m.Caption)
 if caption==""{caption="Изучи прикреплённый документ."}
 copyMessage:=*m
 copyMessage.Document=nil
 copyMessage.Text=fmt.Sprintf("%s\n\nTelegram document (update %d, chat %d, message %d, file_unique_id %s, name %s, MIME %s, size %d): artifact://%s",
  caption,u.ID,m.ChatID,m.MessageID,d.FileUniqueID,name,d.MimeType,len(original),id)
 u.Message=&copyMessage
 return a.deliverTextUpdate(ctx,u,allowed,u.WorkerRequestID)
}
func(c *HTTPServerClient)StoreDocumentOriginal(ctx context.Context,updateID int64,m Message,name string,data []byte)(string,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,strings.TrimRight(c.BaseURL,"/")+"/v1/internal/telegram/document/source",bytes.NewReader(data))
 if err!=nil{return "",err}
 req.Header.Set("Authorization","Bearer "+c.Credential)
 req.Header.Set("Content-Type","application/octet-stream")
 req.Header.Set("X-Document-Update-ID",strconv.FormatInt(updateID,10))
 req.Header.Set("X-Document-Chat-ID",strconv.FormatInt(m.ChatID,10))
 req.Header.Set("X-Document-Message-ID",strconv.FormatInt(m.MessageID,10))
 req.Header.Set("X-Document-Name",name)
 req.Header.Set("X-Document-Mime-Type",m.Document.MimeType)
 response,err:=c.client().Do(req);if err!=nil{return "",err};defer response.Body.Close()
 body,err:=io.ReadAll(io.LimitReader(response.Body,maxServerErrorBody+1))
 if err!=nil{return "",err}
 if response.StatusCode<200||response.StatusCode>=300{
  if response.StatusCode>=400&&response.StatusCode<500{return "",&ImageSourceRejection{Reason:strings.TrimSpace(string(body))}}
  return "",fmt.Errorf("document Artifact HTTP %d",response.StatusCode)
 }
 var result struct{Artifact struct{ID string `json:"id"`} `json:"artifact"`}
 if err=json.Unmarshal(body,&result);err!=nil{return "",err}
 if !strings.HasPrefix(result.Artifact.ID,"art_"){return "",errors.New("invalid document Artifact ID")}
 return result.Artifact.ID,nil
}
