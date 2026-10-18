package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	_ "golang.org/x/image/webp"
)

const MaxPromptImages = 4
const MaxImageDimension = 8192
const MaxImagePixels = 24_000_000

type ImageInput struct {
	ArtifactID string
	MediaType  string
	Data       []byte
}

type ImagePromptSession interface {
	PromptImages(context.Context, string, []ImageInput) error
}

type ImageSteeringSession interface {
	SteerImages(context.Context, string, []ImageInput) (bool, error)
}

type ImageQueueSession interface {
	QueueImages(context.Context, string, []ImageInput) error
}

// ImageResponseSession handles a pending native needs_input request. It must
// guarantee image bytes are accepted by the native model BEFORE signaling
// success, instead of responding with an artifact:// string alone.
type ImageResponseSession interface {
	RespondImages(context.Context, string, string, []ImageInput) error
}

// Native runtimes supporting a split image-steering/reply protocol can
// confirm injection separately from the subsequent needs_input reply ACK.
type ImageResponseInjector interface {
	InjectResponseImages(context.Context, string, string, []ImageInput) error
}

// A runtime may implement the image-response interface while a particular
// native session has negotiated text-only capabilities (notably ACP).
// Check this before claiming the durable needs_input response.
type ImageResponseCapability interface {
	SupportsImageResponse() bool
}

var ErrImageResponseUnsupported = errors.New("image_input_unsupported: native session cannot accept image responses")

func ValidateStillImage(name string, data []byte) (ImageInput, error) {
	if len(data) == 0 || len(data) > core.MaxArtifactBytes {
		return ImageInput{}, errors.New("image: empty or oversized original (maximum 8 MiB)")
	}
	ext := strings.ToLower(filepath.Ext(name))
	expected := map[string]string{".png": "png", ".jpg": "jpeg", ".jpeg": "jpeg", ".webp": "webp"}[ext]
	if expected == "" {
		return ImageInput{}, errors.New("image: supported formats are PNG, JPEG and WebP")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != expected {
		return ImageInput{}, errors.New("image: invalid image data or filename/format mismatch")
	}
	if config.Width < 1 || config.Height < 1 || config.Width > MaxImageDimension || config.Height > MaxImageDimension || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return ImageInput{}, fmt.Errorf("image: dimensions exceed %d px per side or %d pixels total", MaxImageDimension, MaxImagePixels)
	}
	if _, decoded, err := image.Decode(bytes.NewReader(data)); err != nil || decoded != expected {
		return ImageInput{}, errors.New("image: corrupt or truncated image data")
	}
	mimeType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	return ImageInput{MediaType: mimeType, Data: data}, nil
}

func promptWithImages(ctx context.Context, session Session, text string, images []ImageInput) error {
	if len(images) == 0 {
		return session.Prompt(ctx, text)
	}
	multimodal, ok := session.(ImagePromptSession)
	if !ok {
		return errors.New("image: selected native Worker session does not support image prompts")
	}
	return multimodal.PromptImages(ctx, text, images)
}

func supportsImageHarness(instance core.HarnessInstance) bool {
	return instance.Capabilities.SupportsExecution(core.CapabilityImageInput)
}
