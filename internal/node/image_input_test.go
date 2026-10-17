package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"strings"
	"testing"
)

func t10PNG(t *testing.T) []byte {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.Set(1, 1, color.RGBA{R: 100, G: 50, B: 30, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil { t.Fatal(err) }
	return encoded.Bytes()
}

func TestT10ValidatesStillImageAndBadData(t *testing.T) {
	picture := t10PNG(t)
	image, err := ValidateStillImage("sample.png", picture)
	if err != nil || image.MediaType != "image/png" || !bytes.Equal(image.Data, picture) {
		t.Fatalf("valid PNG: %#v %v", image, err)
	}
	for _, tc := range []struct { name string; data []byte }{
		{"sample.jpg", picture}, {"sample.png", picture[:18]}, {"sample.png", []byte("bad")}, {"sample.gif", picture},
	} {
		if _, err := ValidateStillImage(tc.name, tc.data); err == nil { t.Fatalf("accepted %s (%d bytes)", tc.name, len(tc.data)) }
	}
}

func TestT10ACPNativeImagePromptNegotiationAndWire(t *testing.T) {
	if parseAgentCapabilities(nil).PromptImage || parseAgentCapabilities(map[string]any{"promptCapabilities": map[string]any{"image": false}}).PromptImage {
		t.Fatal("agent without explicit image capability must not be trusted")
	}
	if !parseAgentCapabilities(map[string]any{"promptCapabilities": map[string]any{"image": true}}).PromptImage {
		t.Fatal("advertised ACP image capability ignored")
	}
	picture := t10PNG(t)
	blocks := acpPromptBlocks("What color is this image?", []ImageInput{{ArtifactID: "art_1", MediaType: "image/png", Data: picture}})
	if len(blocks) != 2 || blocks[0]["text"] != "What color is this image?" || blocks[1]["type"] != "image" || blocks[1]["mimeType"] != "image/png" {
		t.Fatalf("wrong ACP content blocks: %#v", blocks)
	}
	decoded, err := base64.StdEncoding.DecodeString(blocks[1]["data"])
	if err != nil || !bytes.Equal(decoded, picture) { t.Fatalf("ACP image is not original bytes: %v", err) }
}

type t10TextOnlySession struct{}
func (t10TextOnlySession) ID() string { return "text-only" }
func (t10TextOnlySession) Prompt(context.Context, string) error { return nil }
func (t10TextOnlySession) Steer(context.Context, string) (bool,error) { return false,nil }
func (t10TextOnlySession) Cancel(context.Context) error { return nil }
func (t10TextOnlySession) Activity() <-chan Activity { return nil }
func (t10TextOnlySession) Result() <-chan Result { return nil }
func (t10TextOnlySession) Close() error { return nil }

func TestT10NoSilentTextOnlyFallback(t *testing.T) {
	err := promptWithImages(context.Background(), t10TextOnlySession{}, "what's pictured", []ImageInput{{MediaType: "image/png", Data: t10PNG(t)}})
	if err == nil || !strings.Contains(err.Error(), "does not support image") { t.Fatalf("false visual analysis accepted: %v", err) }
}

func TestT10ACPActualSubprocessImagePayload(t *testing.T) {
	record := filepath.Join(t.TempDir(), "image-prompt.json")
	binary := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: binary.Path, Arguments: binary.Args[1:], Environment: []string{"ACP_IMAGE_RECORD=" + record}}
	picture := t10PNG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "visual-worker", Workspace: t.TempDir(), Task: "Identify the pictured object", Images: []ImageInput{{MediaType: "image/png", Data: picture}}})
	if err != nil { t.Fatal(err) }
	defer session.Close()
	var raw []byte
	for {
		raw, err = os.ReadFile(record)
		if err == nil { break }
		select { case <-ctx.Done(): t.Fatalf("ACP subprocess did not get image: %v", ctx.Err()); case <-time.After(10*time.Millisecond): }
	}
	var received struct { Prompt []map[string]string `json:"prompt"` }
	if err := json.Unmarshal(raw, &received); err != nil { t.Fatal(err) }
	if len(received.Prompt) != 2 || received.Prompt[0]["text"] != "Identify the pictured object" || received.Prompt[1]["type"] != "image" || received.Prompt[1]["mimeType"] != "image/png" {
		t.Fatalf("ACP image wire payload: %#v",received.Prompt)
	}
	original, err := base64.StdEncoding.DecodeString(received.Prompt[1]["data"])
	if err != nil || !bytes.Equal(original, picture) { t.Fatalf("native ACP process did not receive exact image bytes: %v",err) }
}

func TestT10ClaudeActualSubprocessImagePayload(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\nexec \"$CLAUDE_TEST_BINARY\" -test.run=TestFakeClaudeInteractiveProcess\n"
	if err := os.WriteFile(launcher, []byte(script), 0o700); err != nil { t.Fatal(err) }
	record := filepath.Join(t.TempDir(), "claude-image.json")
	picture := t10PNG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := ClaudeCodeRuntime{Command: launcher, Environment: []string{"CLAUDE_TEST_BINARY=" + os.Args[0], "CLAUDE_IMAGE_RECORD=" + record}}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "visual-claude", Workspace: t.TempDir(), Task: "What is in this picture?", Images: []ImageInput{{MediaType: "image/png", Data: picture}}})
	if err != nil { t.Fatal(err) }
	defer session.Close()
	var raw []byte
	for {
		raw, err = os.ReadFile(record)
		if err == nil { break }
		select { case <-ctx.Done(): t.Fatalf("Claude subprocess did not receive image: %v",ctx.Err()); case <-time.After(10*time.Millisecond): }
	}
	var received struct {
		Message struct { Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Source struct {
				Type string `json:"type"`
				MediaType string `json:"media_type"`
				Data string `json:"data"`
			} `json:"source"`
		} `json:"content"` } `json:"message"`
	}
	if err := json.Unmarshal(raw, &received); err != nil { t.Fatal(err) }
	if len(received.Message.Content) != 2 || received.Message.Content[0].Text != "What is in this picture?" || received.Message.Content[1].Type != "image" || received.Message.Content[1].Source.MediaType != "image/png" || received.Message.Content[1].Source.Type != "base64" {
		t.Fatalf("Claude multimodal wire payload = %#v", received)
	}
	original, err := base64.StdEncoding.DecodeString(received.Message.Content[1].Source.Data)
	if err != nil || !bytes.Equal(original, picture) { t.Fatalf("Claude process did not receive image bytes: %v", err) }
}
