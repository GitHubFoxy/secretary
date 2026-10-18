package webapi

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
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

func TestT16ArchivedWorkerArtifactDownloadThroughWeb(t *testing.T){
 ctx:=context.Background()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"server.db"));if err!=nil{t.Fatal(err)};defer store.Close()
 owner,conv,err:=store.CreatePersonWithConversation(ctx);if err!=nil{t.Fatal(err)}
 spec:=core.WorkerSpec{WorkerRef:"t16-archived-worker",Title:"Archive result",Intent:"Publish report",ProjectID:"project-1",NodeID:"node-1",HarnessInstanceID:"node-1/fx",PolicySnapshot:`{"harness":"fx","model":"default"}`}
 worker,_,attempt,err:=store.CreateWorker(ctx,conv.ID,spec,core.TurnSpec{Input:"generate report"});if err!=nil{t.Fatal(err)}
 if _,err=store.SetPhase4AttemptActive(ctx,attempt.ID);err!=nil{t.Fatal(err)}
 artifact,_,err:=store.SavePublishedArtifact(ctx,"node-1",worker.WorkerRef,attempt.ID,"t16-web-archive","report.txt","text/plain",[]byte("archived bytes"))
 if err!=nil{t.Fatal(err)}
 if _,_,_,err=store.RecordAttemptOutcome(ctx,attempt.ID,core.AttemptOutcomeInput{Status:core.OutcomeSucceeded,Classification:core.OutcomeFinal,Summary:"report artifact://"+artifact.ID});err!=nil{t.Fatal(err)}
 if _,err=store.CloseWorker(ctx,worker.ID);err!=nil{t.Fatal(err)}
 api,err:=New(ctx,store,"bootstrap");if err!=nil{t.Fatal(err)}
 server:=httptest.NewServer(api.Handler());defer server.Close()
 jar,err:=cookiejar.New(nil);if err!=nil{t.Fatal(err)}
 client:=&http.Client{Jar:jar};login(t,client,server.URL)
 res,err:=client.Get(server.URL+"/v1/artifacts/"+artifact.ID+"?download=1");if err!=nil{t.Fatal(err)}
 defer res.Body.Close()
 body,err:=io.ReadAll(res.Body);if err!=nil{t.Fatal(err)}
 if res.StatusCode!=http.StatusOK||!bytes.Equal(body,[]byte("archived bytes")){t.Fatalf("archive download HTTP %d body %q",res.StatusCode,body)}
 if _,_,err=store.ArtifactForPerson(ctx,owner.ID,artifact.ID);err!=nil{t.Fatal(err)}
}
