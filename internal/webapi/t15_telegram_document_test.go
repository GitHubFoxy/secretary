package webapi

import(
 "bytes"
 "context"
 "net/http/cookiejar"
 "github.com/beruseruko/secretary/internal/telegram"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "testing"

 "github.com/beruseruko/secretary/internal/core"
)

func TestT15DocumentSourceAuthorizationIntegrityReplayAndDownload(t *testing.T){
 ctx:=context.Background()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"db.sqlite"))
 if err!=nil{t.Fatal(err)};defer store.Close()
 api,err:=New(ctx,store,"bootstrap");if err!=nil{t.Fatal(err)}
 api.AttachInternalCredential("document-adapter-secret")
 srv:=httptest.NewServer(api.Handler());defer srv.Close()
 data:=[]byte("original report contents")
 source:=func(secret string,body []byte,name string)*http.Response{
  t.Helper()
  req,err:=http.NewRequest(http.MethodPost,srv.URL+"/v1/internal/telegram/document/source",bytes.NewReader(body))
  if err!=nil{t.Fatal(err)}
  req.Header.Set("Authorization","Bearer "+secret)
  req.Header.Set("X-Document-Update-ID","720")
  req.Header.Set("X-Document-Chat-ID","100")
  req.Header.Set("X-Document-Message-ID","15")
  req.Header.Set("X-Document-Name",name)
  req.Header.Set("X-Document-Mime-Type","application/pdf")
  res,err:=srv.Client().Do(req);if err!=nil{t.Fatal(err)}
  return res
 }
 unauthorized:=source("wrong",data,"report.pdf");unauthorized.Body.Close()
 if unauthorized.StatusCode!=http.StatusUnauthorized{t.Fatalf("unauthorized source HTTP %d",unauthorized.StatusCode)}
 first:=source("document-adapter-secret",data,"report.pdf")
 defer first.Body.Close()
 if first.StatusCode!=http.StatusCreated{t.Fatalf("first source HTTP %d",first.StatusCode)}
 var saved struct{Artifact core.Artifact `json:"artifact"`;Duplicate bool `json:"duplicate"`}
 if err=json.NewDecoder(first.Body).Decode(&saved);err!=nil{t.Fatal(err)}
 if saved.Artifact.ID==""||saved.Artifact.Name!="report.pdf"||saved.Artifact.MediaType!="application/pdf"||saved.Artifact.Size!=int64(len(data))||saved.Duplicate{
  t.Fatalf("wrong durable document metadata: %+v",saved)
 }
 again:=source("document-adapter-secret",data,"report.pdf");defer again.Body.Close()
 if again.StatusCode!=http.StatusOK{t.Fatalf("replayed source HTTP %d",again.StatusCode)}
 var repeated struct{Artifact core.Artifact `json:"artifact"`;Duplicate bool `json:"duplicate"`}
 if err=json.NewDecoder(again.Body).Decode(&repeated);err!=nil{t.Fatal(err)}
 if !repeated.Duplicate||repeated.Artifact.ID!=saved.Artifact.ID{t.Fatalf("source idempotency: %+v",repeated)}
 conflict:=source("document-adapter-secret",[]byte("conflicting"),"report.pdf")
 conflict.Body.Close()
 if conflict.StatusCode!=http.StatusConflict{t.Fatalf("conflicting source HTTP %d",conflict.StatusCode)}
 bad:=source("document-adapter-secret",data,"../report.pdf")
 bad.Body.Close()
 if bad.StatusCode!=http.StatusUnsupportedMediaType{t.Fatalf("unsafe filename HTTP %d",bad.StatusCode)}
 download:=func(token string)*http.Response{
  t.Helper()
  req,_:=http.NewRequest(http.MethodGet,srv.URL+"/v1/internal/telegram/document/"+saved.Artifact.ID,nil)
  req.Header.Set("Authorization","Bearer "+token)
  res,err:=srv.Client().Do(req);if err!=nil{t.Fatal(err)}
  return res
 }
 forbidden:=download("wrong");forbidden.Body.Close()
 if forbidden.StatusCode!=http.StatusUnauthorized{t.Fatalf("unauthorized document read HTTP %d",forbidden.StatusCode)}
 res:=download("document-adapter-secret");defer res.Body.Close()
 if res.StatusCode!=http.StatusOK||res.Header.Get("Content-Type")!="application/pdf"{t.Fatalf("document GET: %d %q",res.StatusCode,res.Header.Get("Content-Type"))}
 restored,err:=io.ReadAll(res.Body)
 if err!=nil||!bytes.Equal(restored,data){t.Fatalf("document bytes changed: %v",err)}
}

func TestT15OwnerArtifactFallbackIsDownloadableButNotPublic(t *testing.T){
 ctx:=context.Background()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"fallback.db"))
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 api,err:=New(ctx,store,"bootstrap")
 if err!=nil{t.Fatal(err)}
 api.AttachInternalCredential("t15-adapter-credential")
 web:=httptest.NewServer(api.Handler())
 defer web.Close()
 owner,conversation,err:=store.EnsureOwner(ctx)
 if err!=nil{t.Fatal(err)}
 body:=[]byte("Worker original saved on central Server")
 artifact,_,err:=store.SaveArtifact(ctx,owner.ID,conversation.ID,"fallback-t15","published.zip","application/zip",body)
 if err!=nil{t.Fatal(err)}
 client:=&telegram.HTTPServerClient{BaseURL:web.URL,Credential:"t15-adapter-credential",OwnerWebBaseURL:"https://owner.example"}
 link,err:=client.OwnerArtifactDownloadURL(ctx,artifact.ID)
 if err!=nil{t.Fatal(err)}
 if link!="https://owner.example/v1/artifacts/"+artifact.ID+"?download=1"{
  t.Fatalf("fallback URL must refer to owner-authorized Web route: %s",link)
 }
 localDownload:=web.URL+"/v1/artifacts/"+artifact.ID+"?download=1"
 unauthorized,err:=http.Get(localDownload)
 if err!=nil{t.Fatal(err)}
 unauthorized.Body.Close()
 if unauthorized.StatusCode!=http.StatusUnauthorized{
  t.Fatalf("guest retrieved private output: HTTP %d",unauthorized.StatusCode)
 }
 jar,err:=cookiejar.New(nil)
 if err!=nil{t.Fatal(err)}
 browser:=&http.Client{Jar:jar}
 login(t,browser,web.URL)
 response,err:=browser.Get(localDownload)
 if err!=nil{t.Fatal(err)}
 got,readErr:=io.ReadAll(response.Body)
 response.Body.Close()
 if readErr!=nil||response.StatusCode!=http.StatusOK||!bytes.Equal(got,body){
  t.Fatalf("owner fallback failed: HTTP %d, err=%v",response.StatusCode,readErr)
 }
}
