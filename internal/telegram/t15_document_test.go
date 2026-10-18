package telegram

import (
 "encoding/json"
 "testing"
)

func TestT15TelegramOrdinaryDocumentEnvelope(t *testing.T) {
 var input botUpdateDTO
 raw:=`{"update_id":720,"message":{"message_id":15,"from":{"id":100},"chat":{"id":100,"type":"private"},"caption":"Please analyze this report","document":{"file_id":"file-720","file_unique_id":"unique-720","file_name":"report.pdf","mime_type":"application/pdf","file_size":4096}}}`
 if err:=json.Unmarshal([]byte(raw),&input);err!=nil{t.Fatal(err)}
 u:=input.update()
 if u.Message==nil||u.Message.Document==nil{t.Fatalf("document not decoded: %+v",u)}
 d:=u.Message.Document
 if d.FileID!="file-720"||d.FileUniqueID!="unique-720"||d.FileName!="report.pdf"||d.MimeType!="application/pdf"||d.FileSize!=4096||u.Message.Caption!="Please analyze this report"{
  t.Fatalf("ordinary document identity/caption lost: %+v",u)
 }
 if u.Message.Voice!=nil||u.Message.Image!=nil||u.Message.Video!=nil{
  t.Fatal("ordinary Telegram document misrouted as audio/image/video")
 }
}
