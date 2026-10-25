package ctl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type t10LocalQueuedVisionSession struct {
	*t10LocalVisionSession
	queued chan []node.ImageInput
}

func (s *t10LocalQueuedVisionSession) QueueImages(_ context.Context, _ string, images []node.ImageInput) error {
	s.queued <- images
	return nil
}

func t10LocalSteeringSetup(t *testing.T, session node.Session, supportsImages bool, fetch node.ArtifactFetcher) (legacyLocalTestRuntime, core.CommandMetadata, string) {
	t.Helper()
	started := make(chan node.StartRequest, 1)
	local := node.NewLocal(&t10LocalVisionRuntime{started: started, session: session})
	checkpoint, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "local-image-responses.json"))
	if err != nil { t.Fatal(err) }
	local.SetImageResponseStore(checkpoint)
	t.Cleanup(func() { _ = local.Close() })
	local.SetArtifactFetcher(fetch)
	capabilities := []core.ExecutionCapability{core.CapabilityCancel}
	if supportsImages {
		capabilities = append(capabilities, core.CapabilityImageInput)
	}
	harness := core.HarnessInstance{
		ID: "local/fx", Node: "local", Kind: core.HarnessFX, Version: "test", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities: core.HarnessCapabilities{Execution: capabilities},
	}
	meta := core.CommandMetadata{
		CommandID: "dispatch-test", Node: "local", WorkerRef: "local-steering-test",
		TurnID: "local-turn", AttemptID: "local-attempt", HarnessInstanceID: harness.ID,
		IssuedAt: time.Now().UTC(),
	}
	envelope := node.WorkerEnvelope{
		WorkerRef: meta.WorkerRef, TurnID: meta.TurnID, AttemptID: meta.AttemptID,
		OriginalUserIntent: "Start native Worker", HarnessInstance: harness, LegacyWorkerTemplate: true,
	}
	rt := legacyLocalTestRuntime{Local: local}
	command := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{
		Metadata: meta, Envelope: envelope,
	}}
	outcome, err := rt.LocalCommand(context.Background(), command)
	if err != nil || outcome.State != node.CommandAccepted {
		t.Fatalf("local Worker setup failed: outcome=%#v err=%v", outcome, err)
	}
	select {
	case request := <-started:
		return rt, meta, request.Workspace
	default:
		t.Fatal("local Worker did not start")
		return legacyLocalTestRuntime{}, core.CommandMetadata{}, ""
	}
}

func t10SteerLocal(rt legacyLocalTestRuntime, meta core.CommandMetadata, text string) (node.CommandOutcome, error) {
	meta.CommandID = "local-steering"
	return rt.LocalCommand(context.Background(), node.Command{Kind: node.CommandSteering,
		Steering: &node.SteeringCommand{Metadata: meta, Text: text},
	})
}

func t10PNGFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func t10LocalOriginal(id, filename string, bytes []byte) node.ArtifactFetcher {
	return func(_ context.Context, ref string) (node.ArtifactPayload, error) {
		if ref != id {
			return node.ArtifactPayload{}, core.ErrNotFound
		}
		sum := sha256.Sum256(bytes)
		return node.ArtifactPayload{
			ID: id, Name: filename, SHA256: hex.EncodeToString(sum[:]), Data: bytes,
		}, nil
	}
}

func TestT10LocalSteeringFalseNilMustNotAcknowledgeLostImage(t *testing.T) {
	original := t10PNGFixture(t)
	id := "art_" + strings.Repeat("c", 32)
	session := &t10LocalVisionSession{
		steered: make(chan []node.ImageInput, 1), injected: false, // SteerImages returns (false, nil).
	}
	rt, meta, _ := t10LocalSteeringSetup(t, session, true, t10LocalOriginal(id, "picture.png", original))
	outcome, err := t10SteerLocal(rt, meta, "Please inspect artifact://"+id)
	if err == nil || outcome.State != node.CommandFailed || outcome.ErrorCode != "image_input_unsupported" {
		t.Fatalf("image was not injected but local steering was acknowledged: outcome=%#v err=%v", outcome, err)
	}
	var commandErr *node.NodeCommandError
	if !errors.As(err, &commandErr) || commandErr.Outcome.State != node.CommandFailed {
		t.Fatalf("failure must propagate to WorkerService as a rejected command: %v", err)
	}
	select {
	case got := <-session.steered:
		if len(got) != 1 || got[0].ArtifactID != id || !bytes.Equal(got[0].Data, original) {
			t.Fatalf("native image steering fixture received wrong bytes: %#v", got)
		}
	default:
		t.Fatal("SteerImages(false,nil) fixture was not exercised")
	}
}

func TestT10LocalSteeringQueuesNonInjectedImageWithoutLosingBytes(t *testing.T) {
	original := t10PNGFixture(t)
	id := "art_" + strings.Repeat("d", 32)
	session := &t10LocalQueuedVisionSession{
		t10LocalVisionSession: &t10LocalVisionSession{steered: make(chan []node.ImageInput, 1), injected: false},
		queued: make(chan []node.ImageInput, 1),
	}
	rt, meta, _ := t10LocalSteeringSetup(t, session, true, t10LocalOriginal(id, "queued.png", original))
	outcome, err := t10SteerLocal(rt, meta, "Queue image artifact://"+id)
	if err != nil || outcome.State != node.CommandAccepted {
		t.Fatalf("native image queue failed: outcome=%#v err=%v", outcome, err)
	}
	select {
	case got := <-session.queued:
		if len(got) != 1 || got[0].ArtifactID != id || got[0].MediaType != "image/png" || !bytes.Equal(got[0].Data, original) {
			t.Fatalf("queued native image bytes lost: %#v", got)
		}
	default:
		t.Fatal("QueueImages was not invoked after SteerImages(false,nil)")
	}
}

func TestT10LocalTextOnlyHarnessCanSteerDocumentArtifacts(t *testing.T) {
	for _, item := range []struct{ name string; data []byte }{
		{name: "report.pdf", data: []byte("%PDF-1.4\n1 0 obj\nendobj\n%%EOF\n")},
		{name: "notes.txt", data: []byte("a plain text document\n")},
	} {
		t.Run(item.name, func(t *testing.T) {
			id := "art_" + strings.Repeat("e", 32)
			session := &t10LocalVisionSession{texts: make(chan string, 1), injected: false}
			rt, meta, workspace := t10LocalSteeringSetup(t, session, false, t10LocalOriginal(id, item.name, item.data))
			outcome, err := t10SteerLocal(rt, meta, "Summarize this file artifact://"+id)
			if err != nil || outcome.State != node.CommandAccepted {
				t.Fatalf("text-only Worker rejected non-image Artifact: outcome=%#v err=%v", outcome, err)
			}
			select {
			case forwarded := <-session.texts:
				path := filepath.Join(workspace, ".secretary-artifacts", id+"_"+item.name)
				if !strings.Contains(forwarded, "artifact://"+id+" => "+path) {
					t.Fatalf("document reference was not staged for local Worker: %q", forwarded)
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(got, item.data) {
					t.Fatalf("staged document differed from original: %v", readErr)
				}
			default:
				t.Fatal("native text steering was not invoked")
			}
		})
	}
}
