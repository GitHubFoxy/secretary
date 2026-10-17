package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT08ArtifactIDsIncludeUniqueAfterRepeatedReferences(t *testing.T) {
	first := "art_" + strings.Repeat("a", 32)
	last := "art_" + strings.Repeat("b", 32)
	text := strings.Repeat("artifact://"+first+" ", 45) + "artifact://" + last
	ids := ArtifactIDs(text)
	if len(ids) != 2 || ids[0] != first || ids[1] != last {
		t.Fatalf("references after duplicate prefix were dropped: %#v", ids)
	}
	var many strings.Builder
	for i := range 17 {
		fmt.Fprintf(&many, "artifact://art_%032x ", i)
	}
	if len(ArtifactIDs(many.String())) != 17 {
		t.Fatal("16 unique Artifact limit can be bypassed")
	}
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("test-node", &fakeRuntime{}, store)
	execution.SetArtifactFetcher(func(context.Context, string) (ArtifactPayload, error) {
		t.Fatal("over-limit Task must not start downloads")
		return ArtifactPayload{}, nil
	})
	_, err = execution.prepareArtifactPrompt(context.Background(), t.TempDir(), many.String())
	if err == nil || !strings.Contains(err.Error(), "max 16") {
		t.Fatalf("over-limit Task accepted: %v", err)
	}
}

func TestT08SteerAndRuntimeQueueStageAttachments(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprintf("queued-%t", queued), func(t *testing.T) {
			store, err := OpenLocalStore(filepath.Join(t.TempDir(), "state.json"))
			if err != nil { t.Fatal(err) }
			defer store.Close()
			runtime := &fakeRuntime{nonSteerable: queued}
			execution := NewExecutionNode("remote", runtime, store)
			dispatch := daemonDispatchFixture("remote", "initial")
			outcome, err := execution.HandleCommand(context.Background(), dispatch)
			if err != nil || outcome.State != CommandAccepted { t.Fatalf("initial dispatch: %#v %v", outcome, err) }
			id := "art_" + strings.Repeat("a", 32)
			data := []byte("original attached to steering")
			hash := sha256.Sum256(data)
			execution.SetArtifactFetcher(func(_ context.Context, requested string) (ArtifactPayload, error) {
				if requested != id { t.Fatalf("requested %q", requested) }
				return ArtifactPayload{ID: id, Name: "doc.txt", SHA256: hex.EncodeToString(hash[:]), Data: data}, nil
			})
			message := "Read artifact://" + id
			command := Command{Kind: CommandSteering, Steering: &SteeringCommand{
				Metadata: core.CommandMetadata{
					CommandID: "steering", Node: "remote", WorkerRef: "worker-1",
					TurnID: "turn-1", AttemptID: "attempt-1", HarnessInstanceID: "remote/fx",
					IssuedAt: time.Now().UTC(),
				},
				Text: message,
			}}
			outcome, err = execution.HandleCommand(context.Background(), command)
			if err != nil || outcome.State != CommandAccepted { t.Fatalf("steering: %#v %v", outcome, err) }
			var received string
			if queued {
				select { case received = <-runtime.session.queued: case <-time.After(time.Second): t.Fatal("queued attachment not delivered") }
			} else {
				select { case <-runtime.session.activity: case <-time.After(time.Second): t.Fatal("missing ready event") }
				select { case item := <-runtime.session.activity: received = item.Text; case <-time.After(time.Second): t.Fatal("steering attachment not delivered") }
			}
			if !strings.Contains(received, ".secretary-artifacts") || !strings.Contains(received, "artifact://"+id) {
				t.Fatalf("missing staged file reference: %q", received)
			}
			matches, err := filepath.Glob(filepath.Join(runtime.workspace, ".secretary-artifacts", id+"_*"))
			if err != nil || len(matches) != 1 { t.Fatalf("staged files: %v %v", matches, err) }
			staged, err := os.ReadFile(matches[0])
			if err != nil || !bytes.Equal(staged, data) { t.Fatalf("staged bytes: %q %v", staged, err) }
			outcome, err = execution.HandleCommand(context.Background(), command)
			if err != nil || outcome.State != CommandAccepted { t.Fatalf("duplicate steering: %#v %v", outcome, err) }
			if queued && len(runtime.session.queued) != 0 { t.Fatal("duplicate queued message") }
		})
	}
}
