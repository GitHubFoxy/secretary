package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestT11RestoredACPPendingRequestAcceptsOriginalImageBeforeNativeAck(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
	defer cancel()
	steered:=filepath.Join(t.TempDir(),"steering.json")
	replies:=filepath.Join(t.TempDir(),"replies.jsonl")
	command:=exec.Command(os.Args[0],"-test.run=TestFakeACPReconnectProcess")
	runtime:=ACPRuntime{Command:command.Path,Arguments:command.Args[1:],
		Environment:[]string{"ACP_RECONNECT_KIND=input","ACP_RECONNECT_IMAGE=1",
			"ACP_STEERING_RECORD="+steered,"ACP_RESPONSES_FILE="+replies}}
	session,err:=runtime.Resume(ctx,StartRequest{WorkerRef:"worker",Workspace:t.TempDir(),
		PendingRequests:[]PendingRequest{{RequestID:"durable-input",Kind:ActivityUserInput}}},"saved-session")
	if err!=nil {t.Fatal(err)}
	defer session.Close()
	if !session.(ImageResponseCapability).SupportsImageResponse(){t.Fatal("native image capability not negotiated")}
	picture:=t10PNG(t)
	err=session.(ImageResponseSession).RespondImages(ctx,"durable-input","Please analyze this original image",
		[]ImageInput{{ArtifactID:"art_original",MediaType:"image/png",Data:picture}})
	if err!=nil {t.Fatalf("rebound input image was not delivered to native ACP: %v",err)}
	recorded,err:=os.ReadFile(steered)
	if err!=nil {t.Fatal(err)}
	var params struct{Prompt []map[string]string `json:"prompt"`}
	if err:=json.Unmarshal(recorded,&params);err!=nil {t.Fatal(err)}
	if len(params.Prompt)!=2 || params.Prompt[1]["type"]!="image" || params.Prompt[1]["mimeType"]!="image/png" {
		t.Fatalf("missing native image block: %+v",params.Prompt)
	}
	original,err:=base64.StdEncoding.DecodeString(params.Prompt[1]["data"])
	if err!=nil || !bytes.Equal(original,picture){t.Fatalf("original image bytes lost: %v",err)}
	deadline:=time.Now().Add(time.Second)
	for {
		data,readErr:=os.ReadFile(replies)
		if readErr==nil && strings.Count(string(data),"\n")==1 {
			if !strings.Contains(string(data),`"input":"Please analyze this original image"`) {
				t.Fatalf("native response was not acknowledged: %s",data)
			}
			break
		}
		if time.Now().After(deadline){t.Fatalf("native ACK not recorded: %v",readErr)}
		time.Sleep(10*time.Millisecond)
	}
}

func TestT11ACPImageCapabilityIsNegotiatedNotInferredFromInterface(t *testing.T) {
	session:=&acpSession{}
	if session.SupportsImageResponse(){t.Fatal("ACP text-only session falsely advertises image response")}
	session.imageSupported=true
	if !session.SupportsImageResponse(){t.Fatal("ACP negotiated image capability not exposed")}
}
