package node

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

var unsafeArtifactFilename = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func (n *ExecutionNode) SetArtifactFetcher(fetcher ArtifactFetcher) {
	n.mu.Lock()
	n.artifactFetcher = fetcher
	n.mu.Unlock()
}

func (n *ExecutionNode) prepareArtifactPrompt(ctx context.Context, workspace, prompt string) (string, error) {
	text, images, err := n.prepareArtifactInput(ctx, workspace, prompt)
	if err != nil { return "", err }
	if len(images) > 0 { return "", errors.New("image: native image prompt is required; text-only fallback forbidden") }
	return text, nil
}

func (n *ExecutionNode) prepareArtifactInput(ctx context.Context, workspace, prompt string) (string, []ImageInput, error) {
	n.mu.Lock()
	fetch := n.artifactFetcher
	n.mu.Unlock()
	return PrepareArtifactInput(ctx, workspace, prompt, fetch)
}

func PrepareArtifactInput(ctx context.Context, workspace, prompt string, fetch ArtifactFetcher) (string, []ImageInput, error) {
	ids := ArtifactIDs(prompt)
	if len(ids) == 0 {
		return prompt, nil, nil
	}
	if len(ids) > 16 {
		return "", nil, errors.New("node artifact: too many references in a Task (max 16)")
	}
	if fetch == nil {
		return "", nil, errors.New("node artifact: server Artifact transfer is unavailable")
	}
	if workspace == "" {
		return "", nil, errors.New("node artifact: Worker workspace is unavailable")
	}
	dir := filepath.Join(workspace, ".secretary-artifacts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("node artifact: create staging directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", nil, errors.New("node artifact: unsafe staging directory")
	}
	images := make([]ImageInput, 0)
	var additional strings.Builder
	additional.WriteString("\n\nOriginal files fetched from Secretary Server (these paths are local copies; artifact IDs remain canonical):")
	for _, id := range ids {
		artifact, err := fetch(ctx, id)
		if err != nil {
			return "", nil, fmt.Errorf("node artifact %s: %w", id, err)
		}
		if artifact.ID != id || len(artifact.Data) == 0 || len(artifact.Data) > core.MaxArtifactBytes {
			return "", nil, errors.New("node artifact: mismatched or oversized transfer")
		}
		sum := sha256.Sum256(artifact.Data)
		if hex.EncodeToString(sum[:]) != artifact.SHA256 {
			return "", nil, errors.New("node artifact: transfer integrity failure")
		}
		ext := strings.ToLower(filepath.Ext(artifact.Name))
		if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
			if len(images) >= MaxPromptImages {
				return "", nil, fmt.Errorf("image: too many images in one Worker turn (max %d)", MaxPromptImages)
			}
			image, imageErr := ValidateStillImage(artifact.Name, artifact.Data)
			if imageErr != nil {
				return "", nil, fmt.Errorf("artifact://%s: %w", id, imageErr)
			}
			image.ArtifactID = id
			images = append(images, image)
		}
		name := unsafeArtifactFilename.ReplaceAllString(artifact.Name, "_")
		name = strings.Trim(name, "._")
		if len(name) > 120 {
			name = name[:120]
		}
		if name == "" {
			name = "document"
		}
		path := filepath.Join(dir, id+"_"+name)
		if err := saveStagedArtifact(dir, path, artifact.Data); err != nil {
			return "", nil, err
		}
		fmt.Fprintf(&additional, "\n- artifact://%s => %s", id, path)
	}
	return prompt + additional.String(), images, nil
}

func saveStagedArtifact(dir, destination string, data []byte) error {
	temporary, err := os.CreateTemp(dir, ".pending-*")
	if err != nil {
		return fmt.Errorf("node artifact: stage file: %w", err)
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporary.Name(), destination)
}
