package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT08ArtifactStagingChecksIdentityIntegrityAndFailure(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("remote", &fakeRuntime{}, store)
	id := "art_" + strings.Repeat("a", 32)
	second := "art_" + strings.Repeat("b", 32)
	content := []byte("plain\x00binary\xffdata")
	sum := sha256.Sum256(content)
	good := ArtifactPayload{ID: id, Name: "sample.txt", SHA256: hex.EncodeToString(sum[:]), Data: content}
	calls := 0
	execution.SetArtifactFetcher(func(_ context.Context, requested string) (ArtifactPayload, error) {
		calls++
		if requested != id { return ArtifactPayload{}, core.ErrNotFound }
		return good, nil
	})
	prompt, err := execution.prepareArtifactPrompt(context.Background(), dir, "Process artifact://"+id+" twice: artifact://"+id)
	if err != nil { t.Fatal(err) }
	if calls != 1 || !strings.Contains(prompt, "artifact://"+id+" => ") { t.Fatalf("prompt=%q calls=%d", prompt, calls) }
	path := filepath.Join(dir, ".secretary-artifacts", id+"_sample.txt")
	staged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(staged, content) { t.Fatalf("staged=%q err=%v", staged, err) }
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 { t.Fatalf("permissions=%v err=%v", info, err) }

	_, err = execution.prepareArtifactPrompt(context.Background(), dir, "missing artifact://"+second)
	if !errors.Is(err, core.ErrNotFound) { t.Fatalf("missing ref err=%v", err) }

	execution.SetArtifactFetcher(func(context.Context, string) (ArtifactPayload, error) {
		broken := good
		broken.SHA256 = strings.Repeat("0", 64)
		return broken, nil
	})
	_, err = execution.prepareArtifactPrompt(context.Background(), dir, "corrupt artifact://"+id)
	if err == nil || !strings.Contains(err.Error(), "integrity") { t.Fatalf("corrupt err=%v", err) }

	execution.SetArtifactFetcher(nil)
	_, err = execution.prepareArtifactPrompt(context.Background(), dir, "missing transfer artifact://"+id)
	if err == nil || !strings.Contains(err.Error(), "unavailable") { t.Fatalf("unavailable err=%v", err) }
}
