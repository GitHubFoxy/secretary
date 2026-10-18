package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
)

// DeliverImageResponse is shared by co-located LocalCommand and remote
// ExecutionNode.respond. Image steering is checkpointed before calling ACP,
// and its native confirmation is checkpointed separately from response ACK.
func DeliverImageResponse(ctx context.Context, store *LocalStore, session Session,
	metadata core.CommandMetadata, requestID, text string, images []ImageInput) error {
	if len(images) == 0 {
		return errors.New("node: image response delivery requires images")
	}
	if store == nil {
		// Fail closed rather than silently using an in-memory dedupe cache.
		return errors.New("image_response_checkpoint_unavailable: no durable Node state")
	}
	if _, ok := session.(ImageResponseSession); !ok {
		return ErrImageResponseUnsupported
	}
	if c, ok := session.(ImageResponseCapability); ok && !c.SupportsImageResponse() {
		return ErrImageResponseUnsupported
	}

	digest := sha256.New()
	fmt.Fprintf(digest, "%d:%s:%s:%s", len(text), text, metadata.TurnID, metadata.AttemptID)
	for _, img := range images {
		fmt.Fprintf(digest, "|%s|%s|%d|", img.ArtifactID, img.MediaType, len(img.Data))
		_, _ = digest.Write(img.Data)
	}
	fingerprint := hex.EncodeToString(digest.Sum(nil))
	stage, err := store.BeginImageResponse(requestID, metadata.AttemptID, fingerprint)
	if err != nil {
		return fmt.Errorf("image_response_checkpoint_failed: %w", err)
	}
	switch stage {
	case "started":
		// A previous attempt might have injected bytes before crashing.
		// Never repeat native side effects until reconciliation.
		return errors.New("image_response_injection_unknown: native image acceptance requires reconciliation")
	case "started-new":
		if injector, ok := session.(ImageResponseInjector); ok {
			if err := injector.InjectResponseImages(ctx, requestID, text, images); err != nil {
				return err // checkpoint remains started / unsafe to reinject
			}
			if err := store.ConfirmImageResponse(requestID, metadata.AttemptID, fingerprint); err != nil {
				return fmt.Errorf("image_response_checkpoint_failed: %w", err)
			}
		} else {
			// Legacy atomic interfaces cannot expose a distinct injection ACK.
			// A failed/uncertain call leaves the durable started guard in place.
			if err := session.(ImageResponseSession).RespondImages(ctx, requestID, text, images); err != nil {
				return err
			}
			if err := store.ConfirmImageResponse(requestID, metadata.AttemptID, fingerprint); err != nil {
				return fmt.Errorf("image_response_checkpoint_failed: %w", err)
			}
			return nil // the legacy call has already delivered the native reply
		}
	case "injected":
		// Already injected; retry only the native text reply/ACK.
	default:
		return fmt.Errorf("image_response_checkpoint_invalid: unknown stage %q", stage)
	}
	responder, ok := session.(Responder)
	if !ok {
		return errors.New("image_response_unsupported: native interaction response not available")
	}
	return responder.Respond(ctx, requestID, text)
}
