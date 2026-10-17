package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestT11ACPNeedsInputDeliversImageBlocksBeforeNativeResponse(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	record:=filepath.Join(t.TempDir(),"steered-image.json")
	imageRecord:=filepath.Join(t.TempDir(),"initial-image.json")
	proc:=exec.Command(os.Args[0],"-test.run=TestFakeACPProcess")
	runtime:=ACPRuntime{
		Command:proc.Path,Arguments:proc.Args[1:],
		Environment:[]string{
			"ACP_IMAGE_RECORD="+imageRecord,
			"ACP_T11_ASK_IMAGE=1",
			"ACP_T11_STEERING_RECORD="+record,
			"ACP_STEER_OUTCOME=injected",
		},
	}
	session,err:=runtime.Start(ctx,StartRequest{WorkerRef:"t11-needs-input-native",Task:"Ask the owner for a photo",Workspace:t.TempDir()})
	if err!=nil {t.Fatal(err)}
	defer session.Close()
	var requestID string
	for requestID=="" {
		select {
		case activity:=<-session.Activity():
			if activity.Kind==ActivityUserInput {requestID=activity.RequestID}
		case <-ctx.Done():
			t.Fatalf("native ACP did not request user input: %v",ctx.Err())
		}
	}
	if requestID!="t11-native-input" {t.Fatalf("wrong native input ID: %s",requestID)}
	picture:=t10PNG(t)
	err=session.(ImageResponseSession).RespondImages(ctx,requestID,"Please analyze the original photo",[]ImageInput{{
		ArtifactID:"art_test",MediaType:"image/png",Data:picture,
	}})
	if err!=nil {t.Fatalf("multimodal needs_input native handoff failed: %v",err)}
	raw,err:=os.ReadFile(record)
	if err!=nil {t.Fatal(err)}
	var got struct {Prompt []map[string]string `json:"prompt"`}
	if err:=json.Unmarshal(raw,&got);err!=nil {t.Fatal(err)}
	if len(got.Prompt)!=2 || got.Prompt[0]["type"]!="text" ||
		got.Prompt[1]["type"]!="image" || got.Prompt[1]["mimeType"]!="image/png" {
		t.Fatalf("ACP needs_input did not receive native image blocks: %#v",got.Prompt)
	}
	original,err:=base64.StdEncoding.DecodeString(got.Prompt[1]["data"])
	if err!=nil || !bytes.Equal(original,picture) {t.Fatalf("ACP image bytes were changed: %v",err)}
}
