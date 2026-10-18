package webapi

import (
 "bytes"
 "context"
 "net/http"
 "net/http/cookiejar"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
 "github.com/beruseruko/secretary/internal/ctl"
 "github.com/beruseruko/secretary/internal/node"
)

// T09 local production route: real WorkerService -> LocalNode -> path-only
// Attempt broker -> Server Store -> canonical Result -> authenticated download.
func TestT09LocalWorkerPublishToOwnerDownload(t *testing.T) {
 ctx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
 defer cancel()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"secretary.db"))
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 api,err:=New(ctx,store,"bootstrap")
 if err!=nil{t.Fatal(err)}
 if _,err:=store.EnrollNode(ctx,"local");err!=nil{t.Fatal(err)}
 instance:=core.HarnessInstance{
  ID:"local/fx",Node:"local",Kind:core.HarnessFX,Version:"1",
  Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
  Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityShell}},
 }
 if err:=store.UpdateNodeHeartbeat(ctx,"local",core.HarnessInventorySnapshot{
  Node:"local",ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{instance},
 },core.NodeHeartbeat{Capacity:1});err!=nil{t.Fatal(err)}
 workspace:=t.TempDir()
 project,err:=store.CreateProject(ctx,core.ProjectSpec{
  ID:"t09-local-publish",Name:"Local output publication",
  Mappings:[]core.ProjectPathMapping{{Node:"local",Path:workspace}},
  Policy:core.ProjectPolicy{AllowedHarnessKinds:[]core.HarnessKind{core.HarnessFX}},
 })
 if err!=nil{t.Fatal(err)}
 capability,err:=store.RotateSecretaryCapability(ctx,api.OwnerID())
 if err!=nil{t.Fatal(err)}
 payload:=[]byte{'P','K',0,5,13,17,19}
 runtime:=&t09PublishingRuntime{data:payload,ready:make(chan core.Artifact,1)}
 local:=node.NewLocal(runtime)
 defer local.Close()
 executable,err:=os.Executable()
 if err!=nil{t.Fatal(err)}
 if err:=local.SetLocalArtifactPublisher(func(saveCtx context.Context,workerRef,attemptID,key,name,media string,data []byte)(core.Artifact,error){
  artifact,_,err:=store.SavePublishedArtifact(saveCtx,"local",workerRef,attemptID,key,name,media,data)
  return artifact,err
 },executable);err!=nil{t.Fatal(err)}
 profile:=node.ManagedProfile{Version:"1",Name:"worker",Content:"Create an archive",SourceHash:"t09-local",Runtime:string(core.HarnessFX)}
 profile.Hash=profile.SnapshotHash()
 service:=ctl.WorkerService{
  Store:store,PersonID:api.OwnerID(),Capability:capability,
  WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
  WorkerProfileSource:func()(node.ManagedProfile,error){return profile,nil},
  Runtime:ctl.NodeRuntime{Local:local},
 }
 details,err:=service.SpawnWorker(ctx,ctl.SpawnWorkerRequest{
  Intent:"Create and publish a ZIP",ProjectID:project.ID,IdempotencyKey:"t09-local-create",
  Preferences:ctl.WorkerPreferences{NodeID:"local",HarnessKind:core.HarnessFX},
 })
 if err!=nil{t.Fatalf("local Dispatch failed: %v",err)}
 var artifact core.Artifact
 select {
 case artifact=<-runtime.ready:
 case <-ctx.Done(): t.Fatal("local Worker never published output")
 }
 if artifact.Size!=int64(len(payload)) || !node.ValidArtifactID(artifact.ID){t.Fatalf("invalid local Artifact %#v",artifact)}
 // The local broker has no raw-binary upload route. The fake native Worker
 // above also proves ../ and changed-byte replay are rejected.
 if _,err:=store.SetPhase4AttemptActive(ctx,details.Attempts[0].ID);err!=nil{t.Fatal(err)}
 summary:="Generated archive artifact://"+artifact.ID+" bare URL: /v1/artifacts/"+artifact.ID+"?download=1"
 if _,_,_,err:=store.RecordNodeAttemptOutcome(ctx,core.AttemptOutcomeEnvelope{
  Node:"local",WorkerRef:details.Worker.WorkerRef,AttemptID:details.Attempts[0].ID,
  TurnID:details.Turns[0].ID,HarnessInstanceID:core.HarnessInstanceID(details.Worker.HarnessInstanceID),
  Status:core.OutcomeSucceeded,Classification:core.OutcomeFinal,Summary:summary,
 });err!=nil{t.Fatal(err)}
 result,err:=store.Phase4Result(ctx,details.Turns[0].ID)
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(result.ArtifactRefs,"artifact://"+artifact.ID) ||
  strings.Count(result.Summary,"[Download output](/v1/artifacts/"+artifact.ID+"?download=1)")!=1 {
  t.Fatalf("local Result missing canonical link or provenance: %#v",result)
 }
 server:=httptest.NewServer(api.Handler())
 defer server.Close()
 jar,err:=cookiejar.New(nil)
 if err!=nil{t.Fatal(err)}
 owner:=&http.Client{Jar:jar}
 login(t,owner,server.URL)
 downloadURL:=server.URL+"/v1/artifacts/"+artifact.ID+"?download=1"
 response,err:=owner.Get(downloadURL)
 if err!=nil{t.Fatal(err)}
 var body bytes.Buffer
 _,readErr:=body.ReadFrom(response.Body)
 response.Body.Close()
 if readErr!=nil||response.StatusCode!=http.StatusOK||!bytes.Equal(body.Bytes(),payload){
  t.Fatalf("local owner download: status=%d bytes=%v err=%v",response.StatusCode,body.Bytes(),readErr)
 }
 guest,err:=http.Get(downloadURL)
 if err!=nil{t.Fatal(err)}
 guest.Body.Close()
 if guest.StatusCode!=http.StatusUnauthorized{t.Fatalf("guest read local output: HTTP %d",guest.StatusCode)}
 forged,err:=http.NewRequestWithContext(ctx,http.MethodPost,server.URL+"/v1/artifacts/publish",bytes.NewReader([]byte("arbitrary raw data")))
 if err!=nil{t.Fatal(err)}
 forged.Header.Set("Content-Type","application/octet-stream")
 forbidden,err:=owner.Do(forged)
 if err!=nil{t.Fatal(err)}
 forbidden.Body.Close()
 if forbidden.StatusCode==http.StatusOK||forbidden.StatusCode==http.StatusCreated{
  t.Fatal("local publishing accepts unverified raw Server upload")
 }
 existing,all,err:=store.ArtifactForPerson(ctx,api.OwnerID(),artifact.ID)
 if err!=nil||existing.ID!=artifact.ID||!bytes.Equal(all,payload){t.Fatalf("local stored bytes %#v %v",existing,err)}
}
