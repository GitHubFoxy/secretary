package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
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
