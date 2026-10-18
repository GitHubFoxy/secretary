package telegram

import(
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "mime"
 "mime/multipart"
 "path/filepath"
 "strconv"
 "strings"
 "time"
 "github.com/beruseruko/secretary/internal/core"
)
type Document struct {
 FileID string `json:"file_id"`
 FileUniqueID string `json:"file_unique_id,omitempty"`
 FileName string `json:"file_name,omitempty"`
 MimeType string `json:"mime_type,omitempty"`
 FileSize int64 `json:"file_size,omitempty"`
}
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

type DocumentDownloadStore interface {
 DownloadDocument(context.Context,string)(string,string,[]byte,error)
}
type DocumentSendTransport interface{
 SendDocument(context.Context,int64,int64,string,string,[]byte,string)error
}
func(a *Adapter)sendDocumentAttachment(ctx context.Context,message OutgoingMessage)error{
 source,ok:=a.server.(DocumentDownloadStore)
 if !ok{return errors.New("telegram: Server document retrieval unavailable")}
 sender,ok:=a.transport.(DocumentSendTransport)
 if !ok{return errors.New("telegram: Bot sendDocument unavailable")}
 name,mediaType,data,err:=source.DownloadDocument(ctx,message.DocumentArtifactID)
 if err!=nil{return err}
 if len(data)==0||len(data)>core.MaxArtifactBytes{return errors.New("telegram: published document exceeds 8 MiB or is empty")}
 return sender.SendDocument(ctx,message.ChatID,message.ThreadID,name,mediaType,data,message.Text)
}
func(c *HTTPServerClient)DownloadDocument(ctx context.Context,id string)(string,string,[]byte,error){
 if !strings.HasPrefix(id,"art_"){return "","",nil,errors.New("invalid Artifact ID")}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,strings.TrimRight(c.BaseURL,"/")+"/v1/internal/telegram/document/"+url.PathEscape(id),nil)
 if err!=nil{return "","",nil,err}
 req.Header.Set("Authorization","Bearer "+c.Credential)
 res,err:=c.client().Do(req);if err!=nil{return "","",nil,err};defer res.Body.Close()
 if res.StatusCode!=200{return "","",nil,fmt.Errorf("telegram document retrieval HTTP %d",res.StatusCode)}
 data,err:=io.ReadAll(io.LimitReader(res.Body,core.MaxArtifactBytes+1));if err!=nil{return "","",nil,err}
 if len(data)==0||len(data)>core.MaxArtifactBytes{return "","",nil,errors.New("document exceeds 8 MiB")}
 _,params,err:=mime.ParseMediaType(res.Header.Get("Content-Disposition"))
 if err!=nil{return "","",nil,err}
 name:=params["filename"]
 if name==""||filepath.Base(name)!=name{return "","",nil,errors.New("invalid published filename")}
 return name,res.Header.Get("Content-Type"),data,nil
}
func(t *BotAPITransport)SendDocument(ctx context.Context,chatID,threadID int64,name,mediaType string,data []byte,caption string)error{
 if chatID==0||len(data)==0||len(data)>core.MaxArtifactBytes{return errors.New("invalid Telegram document delivery")}
 var body bytes.Buffer
 writer:=multipart.NewWriter(&body)
 _=writer.WriteField("chat_id",strconv.FormatInt(chatID,10))
 if threadID!=0{_=writer.WriteField("message_thread_id",strconv.FormatInt(threadID,10))}
 if caption!=""{if len(caption)>1024{caption=caption[:1024]};_=writer.WriteField("caption",caption)}
 part,err:=writer.CreateFormFile("document",name)
 if err!=nil{return err}
 if _,err=part.Write(data);err!=nil{return err}
 if err=writer.Close();err!=nil{return err}
 base:=strings.TrimRight(t.BaseURL,"/")
 if base==""{base="https://api.telegram.org"}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,base+"/bot"+t.BotToken+"/sendDocument",&body)
 if err!=nil{return err}
 req.Header.Set("Content-Type",writer.FormDataContentType())
 res,err:=t.client().Do(req);if err!=nil{return err};defer res.Body.Close()
 reply,err:=io.ReadAll(io.LimitReader(res.Body,4096));if err!=nil{return err}
 if res.StatusCode<200||res.StatusCode>=300{return fmt.Errorf("Telegram sendDocument HTTP %d",res.StatusCode)}
 var envelope struct{OK bool `json:"ok"`}
 if err=json.Unmarshal(reply,&envelope);err!=nil{return err}
 if !envelope.OK{return errors.New("Telegram rejected document upload")}
 _=mediaType
 return nil
}

func(a *Adapter)deliverEventDocuments(ctx context.Context,event Event)error{
 if event.WorkerRef=="" || event.TerminalIdentity=="" || len(event.Payload)==0{return nil}
 // Only accepted Results, not provisional Attempt outcomes, may announce
 // output Artifacts to Telegram. Both events share the same Turn identity.
 var payload struct{
  ID string `json:"id"`
  ArtifactRefs []string `json:"artifact_refs"`
 }
 if err:=json.Unmarshal(event.Payload,&payload);err!=nil{return nil}
 if !strings.HasPrefix(payload.ID,"res_"){return nil}
 seen:=map[string]bool{}
 for _,ref:=range payload.ArtifactRefs{
  id:=strings.TrimPrefix(ref,"artifact://")
  if !strings.HasPrefix(id,"art_")||seen[id]{continue}
  seen[id]=true
  message:=OutgoingMessage{Text:"Файл результата Worker",Identity:"file-result:"+payload.ID+":"+id,
   DocumentArtifactID:id,DurableSequence:event.Sequence}
  for _,chat:=range a.personalChats(){
   copy:=message;copy.ChatID=chat
   if err:=a.sendMessage(ctx,copy);err!=nil{return err}
  }
  if a.config.GroupChatID!=nil{
   mapping,err:=a.ensureTopic(ctx,event.WorkerRef,event.Title)
   if err!=nil{return err}
   if mapping.ChatID!=0{
    copy:=message;copy.ChatID=mapping.ChatID;copy.ThreadID=mapping.ThreadID
    if err:=a.sendMessage(ctx,copy);err!=nil{return err}
   }
  }
 }
 return nil
}
