package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type t10ImageRuntime struct {
	original []byte
	seen chan node.StartRequest
	starts int
}

func (r *t10ImageRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.starts++
	if len(request.Images) != 1 || request.Images[0].MediaType != "image/png" || !bytes.Equal(request.Images[0].Data, r.original) {
		return nil, fmt.Errorf("native Worker received no real image: %#v", request.Images)
	}
	if !strings.Contains(request.Task, "What is in the picture?") || !strings.Contains(request.Task, "artifact://"+request.Images[0].ArtifactID) {
		return nil, fmt.Errorf("Worker lost caption and canonical identity: %q", request.Task)
	}
	matches, err := filepath.Glob(filepath.Join(request.Workspace, ".secretary-artifacts", "art_*"))
	if err != nil || len(matches) != 1 { return nil, fmt.Errorf("staged original missing: %v %v", matches, err) }
	staged, err := os.ReadFile(matches[0])
	if err != nil || !bytes.Equal(staged, r.original) { return nil, fmt.Errorf("staged original changed: %v", err) }
	r.seen <- request
	results := make(chan node.Result, 1)
	results <- node.Result{Status: "succeeded", Summary: "Grounded result: picture contains a colored pixel"}
	return &artifactWorkerSession{result: results}, nil
}

func t10WebPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil { t.Fatal(err) }
	return out.Bytes()
}

func TestT10WebImageToRemoteWorkerWithDedupAndErrors(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{PairingTokens: []string{"t10-pair"}, AdminToken: "t10-admin"})
	if err != nil { t.Fatal(err) }
	api.AttachNodeService(manager)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "t10-pair", "t10-remote")
	if err != nil { t.Fatal(err) }
	jar, err := cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	client := &http.Client{Jar: jar}
	login(t, client, server.URL)
	picture := t10WebPNG(t)

	upload := func(key, filename string, data []byte) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/artifacts?name="+filename, bytes.NewReader(data))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Idempotency-Key", key)
		res, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		var payload map[string]any
		if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil { t.Fatal(err) }
		}
		return res.StatusCode, payload
	}
	if code,_ := upload("corrupt", "bad.png", []byte("not an image")); code != http.StatusUnprocessableEntity { t.Fatalf("corrupt image accepted: %d", code) }
	if code,_ := upload("wrong-format", "bad.jpg", picture); code != http.StatusUnprocessableEntity { t.Fatalf("disguised image accepted: %d", code) }
	if code,_ := upload("too-large", "huge.png", bytes.Repeat([]byte("x"), core.MaxArtifactBytes+1)); code != http.StatusRequestEntityTooLarge { t.Fatalf("oversize image accepted: %d", code) }
	code, first := upload("image-stable-id", "photo.png", picture)
	if code != http.StatusCreated { t.Fatalf("image upload failed: %d", code) }
	ref := first["reference"].(string)
	id := first["artifact"].(map[string]any)["id"].(string)
	if first["artifact"].(map[string]any)["media_type"] != "image/png" { t.Fatalf("MIME lost: %#v", first) }
	if code,again := upload("image-stable-id", "photo.png", picture); code != http.StatusOK || again["reference"] != ref {
		t.Fatalf("upload replay = %d %#v", code, again)
	}
	remote, err := identity.FetchArtifact(ctx, id)
	if err != nil || remote.Name != "photo.png" || !bytes.Equal(remote.Data, picture) { t.Fatalf("Node fetch: %#v %v", remote, err) }

	caption := "What is in the picture? " + ref + " " + ref
	message := func() map[string]any {
		t.Helper()
		body,_ := json.Marshal(map[string]string{"external_message_id":"t10-web-caption", "body":caption})
		req,err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", bytes.NewReader(body))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Content-Type","application/json")
		req.Header.Set("Idempotency-Key","t10-caption-identity")
		res,err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		if res.StatusCode != http.StatusAccepted { t.Fatalf("Web caption rejected: %d",res.StatusCode) }
		var ack map[string]any
		if err := json.NewDecoder(res.Body).Decode(&ack); err != nil { t.Fatal(err) }
		return ack
	}
	ack := message()
	replayed := message()
	if ack["message_id"] != replayed["message_id"] { t.Fatalf("Web message identity duplicated: %#v / %#v",ack,replayed) }
	entry := ack["entry"].(map[string]any)
	if entry["body"] != caption { t.Fatalf("Web caption not preserved: %#v",entry) }

	runtime := &t10ImageRuntime{original: picture, seen: make(chan node.StartRequest, 2)}
	local,err := node.OpenLocalStore(filepath.Join(t.TempDir(),"node.json"))
	if err != nil { t.Fatal(err) }
	defer local.Close()
	execution := node.NewExecutionNode(identity.Node,runtime,local)
	execution.SetArtifactFetcher(identity.FetchArtifact)
	instance := core.HarnessInstance{
		ID: core.HarnessInstanceID(string(identity.Node)+"/fx"),Node: identity.Node,Kind: core.HarnessFX,
		Version:"test",Status:core.HarnessReady,Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityImageInput}},
	}
	envelope := node.WorkerEnvelope{WorkerRef:"t10-worker",TurnID:"t10-turn",AttemptID:"t10-attempt",OriginalUserIntent:entry["body"].(string),HarnessInstance:instance,LegacyWorkerTemplate:true}
	cmd := node.Command{Kind:node.CommandDispatch, Dispatch:&node.DispatchCommand{Metadata:core.CommandMetadata{
		CommandID:"t10-command",Node:identity.Node,WorkerRef:envelope.WorkerRef,TurnID:envelope.TurnID,AttemptID:envelope.AttemptID,HarnessInstanceID:instance.ID,IssuedAt:time.Now().UTC(),
	},Envelope:envelope}}
	result,err := execution.HandleCommand(ctx,cmd)
	if err != nil || result.State != node.CommandAccepted { t.Fatalf("image dispatch failed: %#v %v",result,err) }
	select {
	case incoming := <-runtime.seen:
		if len(incoming.Images) != 1 || incoming.Images[0].ArtifactID != id { t.Fatalf("identity deduplication failed: %#v",incoming.Images) }
	case <-time.After(2*time.Second):
		t.Fatal("Worker did not receive image")
	}
	result,err = execution.HandleCommand(ctx,cmd)
	if err != nil || (result.State != node.CommandAccepted && !(result.State == node.CommandFailed && result.ErrorCode == "runtime_session_unavailable")) || runtime.starts != 1 { t.Fatalf("command replay duplicated Worker: %#v %v starts=%d",result,err,runtime.starts) }
	deadline := time.Now().Add(2*time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		pending,err := local.PendingEvents()
		if err != nil { t.Fatal(err) }
		for _,item := range pending {
			var event node.NodeEvent
			if json.Unmarshal(item.Payload,&event)==nil && event.Outcome!=nil && event.Outcome.AttemptID==envelope.AttemptID && event.Outcome.Status==core.OutcomeSucceeded && strings.Contains(event.Outcome.Summary,"Grounded result") { found=true }
		}
		if !found { time.Sleep(10*time.Millisecond) }
	}
	if !found { t.Fatal("no durable Worker terminal Result after visual input") }

	unsupported := cmd
	unsupportedEnvelope := envelope
	unsupportedEnvelope.WorkerRef="t10-unsupported"
	unsupportedEnvelope.TurnID="t10-unsupported-turn"
	unsupportedEnvelope.AttemptID="t10-unsupported-attempt"
	unsupportedEnvelope.HarnessInstance.Capabilities = core.HarnessCapabilities{}
	unsupportedDispatch := *cmd.Dispatch
	unsupportedDispatch.Envelope = unsupportedEnvelope
	unsupportedDispatch.Metadata.CommandID="t10-unsupported-command"
	unsupportedDispatch.Metadata.WorkerRef=unsupportedEnvelope.WorkerRef
	unsupportedDispatch.Metadata.TurnID=unsupportedEnvelope.TurnID
	unsupportedDispatch.Metadata.AttemptID=unsupportedEnvelope.AttemptID
	unsupported.Dispatch=&unsupportedDispatch
	denied,err := execution.HandleCommand(ctx,unsupported)
	if err != nil || denied.State != node.CommandFailed || denied.ErrorCode != "image_input_unsupported" || runtime.starts != 1 {
		t.Fatalf("unsupported harness did not fail closed: %#v %v starts=%d",denied,err,runtime.starts)
	}
}
