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
	"time"

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
	return prepareArtifactInputWithPoll(ctx, workspace, prompt, fetch, 250*time.Millisecond)
}

// Same preparation path for real dispatch and accelerated queue regression.
func prepareArtifactInputWithPoll(ctx context.Context, workspace, prompt string, fetch ArtifactFetcher, pollInterval time.Duration) (string, []ImageInput, error) {
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

		if artifact.VideoStatus=="processing" || artifact.VideoStatus=="queued" {
			// Jobs are bounded but may spend minutes waiting for one of the two
			// FFmpeg slots. The Node connection/command context, not a fixed
			// wall-clock deadline, owns cancellation of this dependent dispatch.
			if err:=awaitVideoProcessing(ctx,id,&artifact,pollInterval);err!=nil {
				return "",nil,err
			}
		}
		if artifact.VideoStatus=="failed" {return "",nil,fmt.Errorf("video: original artifact://%s could not be processed: %s",id,artifact.VideoError)}
		if artifact.ID != id || len(artifact.Data) == 0 || len(artifact.Data) > core.MaxArtifactBytes {
			return "", nil, errors.New("node artifact: mismatched or oversized transfer")
		}
		sum := sha256.Sum256(artifact.Data)
		if hex.EncodeToString(sum[:]) != artifact.SHA256 {
			return "", nil, errors.New("node artifact: transfer integrity failure")
		}

		if artifact.VideoStatus=="ready" {
			if len(images)+len(artifact.VideoFrames)>16 {return "",nil,errors.New("video: at most 16 frames can be delivered in a Worker turn")}
			var previous int64=-1
			seenFrames:=make(map[string]bool)
			for _,frame:=range artifact.VideoFrames {
				if !ValidArtifactID(frame.ArtifactID) || frame.ArtifactID==id || seenFrames[frame.ArtifactID] || frame.TimestampMS<=previous {
					return "",nil,errors.New("video: invalid frame identities or timestamps")
				}
				seenFrames[frame.ArtifactID]=true
				previous=frame.TimestampMS
				child,e:=fetch(ctx,frame.ArtifactID)
				if e!=nil {return "",nil,fmt.Errorf("video: fetch frame %s: %w",frame.ArtifactID,e)}
				if child.ID!=frame.ArtifactID || len(child.Data)==0 || len(child.Data)>core.MaxArtifactBytes {return "",nil,errors.New("video: invalid frame Artifact payload")}
				sha:=sha256.Sum256(child.Data)
				if hex.EncodeToString(sha[:])!=child.SHA256 {return "",nil,errors.New("video: frame checksum mismatch")}
				validated,e:=ValidateStillImage(child.Name,child.Data)
				if e!=nil || validated.MediaType!="image/webp" {return "",nil,fmt.Errorf("video: invalid WebP frame %s: %v",frame.ArtifactID,e)}
				validated.ArtifactID=frame.ArtifactID
				images=append(images,validated)
				switch frame.TimestampKind {
				case "source-pts-relative":
					fmt.Fprintf(&additional,"\n- video artifact://%s source-frame PTS (relative to clip start) %.3f s [timestamp_kind=source-pts-relative]: frame artifact://%s (native image supplied)",id,float64(frame.TimestampMS)/1000,frame.ArtifactID)
				case "nominal-grid", "":
					// Empty kind can come from old frame metadata. Never imply the
					// nominal time is the presentation timestamp of the source.
					fmt.Fprintf(&additional,"\n- video artifact://%s approximate nominal-grid sample time %.3f s [timestamp_kind=nominal-grid; NOT actual source-frame PTS]: frame artifact://%s (native image supplied)",id,float64(frame.TimestampMS)/1000,frame.ArtifactID)
				default:
					return "",nil,fmt.Errorf("video: frame artifact://%s has unknown timestamp kind %q",frame.ArtifactID,frame.TimestampKind)
				}
			}
			fmt.Fprintf(&additional,"\n- Video artifact://%s contains discrete frame samples only; events between frames were NOT observed. Each frame's timestamp_kind identifies whether its time is source-frame PTS or an approximate legacy nominal grid.",id)
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

// awaitVideoProcessing polls signed metadata only. It never imposes a hidden
// 120-second ceiling: a queued video remains pending until ready, failed, or
// the connected Node's command context is canceled (e.g. on disconnect).
// pollInterval is injectable to cover queues longer than 120 seconds without
// slowing down regression tests.
func awaitVideoProcessing(ctx context.Context, id string, artifact *ArtifactPayload, pollInterval time.Duration) error {
	if artifact.CheckVideoStatus==nil {return errors.New("video: authenticated metadata polling unavailable")}
	if pollInterval<0 {return errors.New("video: invalid polling interval")}
	for artifact.VideoStatus=="queued" || artifact.VideoStatus=="processing" {
		if err:=ctx.Err();err!=nil {
			return fmt.Errorf("video: waiting for artifact://%s interrupted: %w",id,err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("video: waiting for artifact://%s interrupted: %w",id,ctx.Err())
		case <-time.After(pollInterval):
		}
		// If a zero-interval test timer and cancellation become ready together,
		// cancellation must win before starting another metadata request.
		if err:=ctx.Err();err!=nil {
			return fmt.Errorf("video: waiting for artifact://%s interrupted: %w",id,err)
		}
		meta,err:=artifact.CheckVideoStatus(ctx)
		if err!=nil {return fmt.Errorf("video: metadata status unavailable: %w",err)}
		if meta.ID!=id {return errors.New("video: status metadata identifier mismatch")}
		switch meta.ProcessingStatus {
		case "queued","processing","ready","failed":
		default:
			return fmt.Errorf("video: unexpected processing status %q for artifact://%s",meta.ProcessingStatus,id)
		}
		artifact.VideoStatus=meta.ProcessingStatus
		artifact.VideoError=meta.ProcessingError
		artifact.VideoFrames=meta.VideoFrames
	}
	return nil
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
