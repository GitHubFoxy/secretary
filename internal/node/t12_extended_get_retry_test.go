package node

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/beruseruko/secretary/internal/core"
)

// Five back-to-back 503s are a single temporary outage, not a terminal
// artifact_unavailable. Both original and frame transfers must wait for the
// sixth successful signed GET, without a duplicate native dispatch.
func TestT12Extended503OriginalAndFrameNeverTerminateDispatch(t *testing.T){
	for _,failing:=range []string{"original","frame"}{
		t.Run(failing,func(t *testing.T){
			origID:="art_"+strings.Repeat("b",32)
			frameID:="art_"+strings.Repeat("c",32)
			orig:=[]byte("ready video")
			webp,err:=base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
			if err!=nil{t.Fatal(err)}
			digest:=func(b []byte)string{sum:=sha256.Sum256(b);return hex.EncodeToString(sum[:])}
			var origGets,frameGets atomic.Int32
			server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
				if r.Header.Get("X-Artifact-Signature")==""||r.Header.Get("X-Secretary-Node")!="macbook"{
					http.Error(w,"unauthorized",403);return
				}
				id:=strings.TrimPrefix(r.URL.Path,"/v1/artifacts/")
				var data []byte
				var filename string
				switch id{
				case origID:
					if origGets.Add(1)<=5 && failing=="original"{http.Error(w,"outage",503);return}
					data,filename=orig,"clip.mp4"
					w.Header().Set("X-Artifact-Video-Status","ready")
					refs,_:=json.Marshal([]core.VideoFrame{{ArtifactID:frameID,TimestampMS:0,TimestampKind:"source-pts-relative"}})
					w.Header().Set("X-Artifact-Video-Frames",base64.RawURLEncoding.EncodeToString(refs))
				case frameID:
					if frameGets.Add(1)<=5 && failing=="frame"{http.Error(w,"outage",503);return}
					data,filename=webp,"frame.webp"
				default:http.NotFound(w,r);return
				}
				w.Header().Set("X-Artifact-SHA256",digest(data))
				w.Header().Set("X-Artifact-Name",base64.RawURLEncoding.EncodeToString([]byte(filename)))
				_,_=w.Write(data)
			}))
			defer server.Close()
			identity:=NodeIdentity{Node:"macbook",
				Credential:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41},32)),
				ConnectURL:"ws"+strings.TrimPrefix(server.URL,"http")+"/v1/nodes/connect"}
			state,err:=OpenLocalStore(filepath.Join(t.TempDir(),"node-state.json"))
			if err!=nil{t.Fatal(err)}
			defer state.Close()
			runtime:=&daemonCountingRuntime{}
			execution:=NewExecutionNode("macbook",runtime,state)
			execution.SetArtifactFetcher(identity.FetchArtifact)
			cmd:=daemonDispatchFixture("macbook","t12-extended-503-"+failing)
			cmd.Dispatch.Envelope.OriginalUserIntent="Analyze artifact://"+origID
			cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution=append(cmd.Dispatch.Envelope.HarnessInstance.Capabilities.Execution,core.CapabilityImageInput)
			ctx,cancel:=context.WithTimeout(context.Background(),13*time.Second)
			defer cancel()
			outcome,err:=execution.HandleCommand(ctx,cmd)
			if err!=nil||outcome.State!=CommandAccepted {
				t.Fatalf("temporary 503 exhausted short retry and lost Worker: outcome=%+v err=%v",outcome,err)
			}
			if runtime.starts.Load()!=1{
				t.Fatalf("native worker started %d times",runtime.starts.Load())
			}
			if failing=="original"{
				if origGets.Load()!=6||frameGets.Load()!=1{t.Fatalf("request count original=%d frame=%d",origGets.Load(),frameGets.Load())}
			}else if origGets.Load()!=1||frameGets.Load()!=6{
				t.Fatalf("request count original=%d frame=%d",origGets.Load(),frameGets.Load())
			}
			record,err:=state.Command(cmd.Metadata().CommandID)
			if err!=nil||record.State!=CommandAccepted || record.Outcome.ErrorCode=="artifact_unavailable"{
				t.Fatalf("durable prestart became terminal: %+v %v",record,err)
			}
			again,err:=execution.HandleCommand(ctx,cmd)
			if err!=nil || again.State!=CommandAccepted || runtime.starts.Load()!=1 {
				t.Fatalf("native dispatch replayed after recovered GET: %+v err=%v starts=%d",again,err,runtime.starts.Load())
			}
		})
	}
}

// Permanent integrity/auth errors must remain final even after transient
// retries have otherwise been enabled.
func TestT12CorruptOriginalChecksumStillTerminal(t *testing.T){
	id:="art_"+strings.Repeat("d",32)
	var requests atomic.Int32
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		requests.Add(1)
		w.Header().Set("X-Artifact-SHA256",strings.Repeat("0",64))
		w.Header().Set("X-Artifact-Name",base64.RawURLEncoding.EncodeToString([]byte("clip.mp4")))
		_,_=w.Write([]byte("corrupt"))
	}))
	defer server.Close()
	identity:=NodeIdentity{Node:"macbook",Credential:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x41},32)),
		ConnectURL:"ws"+strings.TrimPrefix(server.URL,"http")+"/v1/nodes/connect"}
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer cancel()
	_,err:=identity.FetchArtifact(ctx,id)
	if err==nil || errors.Is(err,ErrTransientArtifactStatus)||requests.Load()!=1{
		t.Fatalf("checksum violation retried or accepted: %v count=%d",err,requests.Load())
	}
}
