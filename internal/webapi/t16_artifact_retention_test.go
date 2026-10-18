package webapi

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/cookiejar"
 "net/http/httptest"
 "path/filepath"
 "testing"

 "github.com/beruseruko/secretary/internal/core"
)

func TestT16OwnerWebArtifactDeleteAndMissingDownload(t *testing.T){
 ctx:=context.Background()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"secretary.db"));if err!=nil{t.Fatal(err)};defer store.Close()
 api,err:=New(ctx,store,"bootstrap");if err!=nil{t.Fatal(err)}
 server:=httptest.NewServer(api.Handler());defer server.Close()
 jar,err:=cookiejar.New(nil);if err!=nil{t.Fatal(err)}
 client:=&http.Client{Jar:jar};login(t,client,server.URL)
 upload,err:=http.NewRequest(http.MethodPost,server.URL+"/v1/artifacts?name=report.txt",bytes.NewReader([]byte("report bytes")))
 if err!=nil{t.Fatal(err)}
 upload.Header.Set("Idempotency-Key","t16-delete-http")
 result,err:=client.Do(upload);if err!=nil{t.Fatal(err)}
 if result.StatusCode!=http.StatusCreated{t.Fatalf("upload HTTP %d",result.StatusCode)}
 var data struct { Artifact struct{ID string `json:"id"`} `json:"artifact"` }
 if err=json.NewDecoder(result.Body).Decode(&data);err!=nil{result.Body.Close();t.Fatal(err)}
 result.Body.Close()
 if data.Artifact.ID==""{t.Fatal("missing Artifact ID")}
 unauthenticated,err:=http.NewRequest(http.MethodDelete,server.URL+"/v1/artifacts/"+data.Artifact.ID,nil);if err!=nil{t.Fatal(err)}
 response,err:=http.DefaultClient.Do(unauthenticated);if err!=nil{t.Fatal(err)}
 if response.StatusCode==http.StatusNoContent{t.Fatal("unauthenticated delete succeeded")};response.Body.Close()
 deletion,err:=http.NewRequest(http.MethodDelete,server.URL+"/v1/artifacts/"+data.Artifact.ID,nil);if err!=nil{t.Fatal(err)}
 response,err=client.Do(deletion);if err!=nil{t.Fatal(err)}
 if response.StatusCode!=http.StatusNoContent{t.Fatalf("delete HTTP %d",response.StatusCode)};response.Body.Close()
 download,err:=client.Get(server.URL+"/v1/artifacts/"+data.Artifact.ID+"?download=1");if err!=nil{t.Fatal(err)}
 if download.StatusCode!=http.StatusNotFound{t.Fatalf("deleted Artifact returned HTTP %d",download.StatusCode)};download.Body.Close()
}
