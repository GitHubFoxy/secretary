package webapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
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

type artifactWorkerRuntime struct {
	source []byte
	seen   chan string
}

func (r *artifactWorkerRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	matches, err := filepath.Glob(filepath.Join(request.Workspace, ".secretary-artifacts", "art_*"))
	if err != nil || len(matches) != 1 {
		return nil, os.ErrNotExist
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(body, r.source) {
		return nil, io.ErrUnexpectedEOF
	}
	r.seen <- request.Task
	result := make(chan node.Result, 1)
	result <- node.Result{Status: "succeeded", Summary: "Verified original bytes"}
	return &artifactWorkerSession{result: result}, nil
}

type artifactWorkerSession struct {
	result chan node.Result
}

func (*artifactWorkerSession) ID() string                                { return "artifact-worker-session" }
func (*artifactWorkerSession) Prompt(context.Context, string) error      { return nil }
func (*artifactWorkerSession) Steer(context.Context, string) (bool, error) { return false, nil }
func (*artifactWorkerSession) Cancel(context.Context) error              { return nil }
func (*artifactWorkerSession) Activity() <-chan node.Activity            { return nil }
func (s *artifactWorkerSession) Result() <-chan node.Result              { return s.result }
func (*artifactWorkerSession) Close() error                              { return nil }

func TestT08WebArtifactRemoteWorkerResultAndRestart(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "secretary.db")
	store, err := core.Open(ctx, databasePath)
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
		PairingTokens: []string{"pair-artifact-remote"}, AdminToken: "admin-artifact-remote",
	})
	if err != nil { t.Fatal(err) }
	api.AttachNodeService(manager)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "pair-artifact-remote", "artifact-remote")
	if err != nil { t.Fatal(err) }
	jar, err := cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	client := &http.Client{Jar: jar}
	login(t, client, server.URL)

	source := []byte("%PDF-1.7\n\x00\x01\xff\nOriginal, unmodified, binary content.\n")
	upload := func(key, name string, data []byte) (*http.Response, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/artifacts?name="+name, bytes.NewReader(data))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Idempotency-Key", key)
		res, err := client.Do(req)
		if err != nil { t.Fatal(err) }
		var payload map[string]any
		if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusOK {
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil { t.Fatal(err) }
		}
		res.Body.Close()
		return res, payload
	}
	first, payload := upload("stable-upload-1", "report.pdf", source)
	if first.StatusCode != http.StatusCreated { t.Fatalf("upload status = %d", first.StatusCode) }
	artifact := payload["artifact"].(map[string]any)
	id := artifact["id"].(string)
	ref := payload["reference"].(string)
	if !node.ValidArtifactID(id) || ref != "artifact://"+id || artifact["status"] != "ready" { t.Fatalf("artifact = %#v", payload) }
	again, repeated := upload("stable-upload-1", "report.pdf", source)
	if again.StatusCode != http.StatusOK || repeated["reference"] != ref { t.Fatalf("replay = %d %#v", again.StatusCode, repeated) }
	conflict, _ := upload("stable-upload-1", "report.pdf", []byte("changed"))
	if conflict.StatusCode != http.StatusConflict { t.Fatalf("conflict = %d", conflict.StatusCode) }
	unsupported, _ := upload("no-exe", "bad.exe", []byte("MZ"))
	if unsupported.StatusCode != http.StatusUnsupportedMediaType { t.Fatalf("unsupported = %d", unsupported.StatusCode) }
	oversize, _ := upload("too-large", "big.pdf", bytes.Repeat([]byte("x"), core.MaxArtifactBytes+1))
	if oversize.StatusCode != http.StatusRequestEntityTooLarge { t.Fatalf("oversize = %d", oversize.StatusCode) }

	anonymous, err := http.Get(server.URL+"/v1/artifacts/"+id+"?download=1")
	if err != nil { t.Fatal(err) }
	anonymous.Body.Close()
	if anonymous.StatusCode != http.StatusUnauthorized { t.Fatalf("anonymous download = %d", anonymous.StatusCode) }

	badURL := server.URL + "/v1/artifacts/" + id
	bad, err := http.NewRequest(http.MethodGet, badURL, nil)
	if err != nil { t.Fatal(err) }
	bad.Header.Set("X-Secretary-Node", string(identity.Node))
	bad.Header.Set("X-Artifact-Timestamp", "123")
	bad.Header.Set("X-Artifact-Signature", "bad")
	denied, err := http.DefaultClient.Do(bad)
	if err != nil { t.Fatal(err) }
	denied.Body.Close()
	if denied.StatusCode != http.StatusUnauthorized { t.Fatalf("bad signature = %d", denied.StatusCode) }

	fetched, err := identity.FetchArtifact(ctx, id)
	if err != nil || !bytes.Equal(fetched.Data, source) || fetched.Name != "report.pdf" {
		t.Fatalf("remote original mismatch: payload=%#v err=%v", fetched, err)
	}
	corrupt := fetched
	corrupt.Data = []byte("corrupted")
	runtime := &artifactWorkerRuntime{source: source, seen: make(chan string, 1)}
	nodeStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "remote-state.json"))
	if err != nil { t.Fatal(err) }
	defer nodeStore.Close()
	execution := node.NewExecutionNode(identity.Node, runtime, nodeStore)
	execution.SetArtifactFetcher(identity.FetchArtifact)
	instance := core.HarnessInstance{
		ID: core.HarnessInstanceID(string(identity.Node)+"/fx"), Node: identity.Node, Kind: core.HarnessFX,
		Version: "test", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true},
	}
	envelope := node.WorkerEnvelope{
		WorkerRef: "worker-file", TurnID: "turn-file", AttemptID: "attempt-file",
		OriginalUserIntent: "Read "+ref+" and verify bytes", HarnessInstance: instance,
		LegacyWorkerTemplate: true,
	}
	command := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{
		Metadata: core.CommandMetadata{
			CommandID: "cmd-artifact", Node: identity.Node, WorkerRef: envelope.WorkerRef,
			TurnID: envelope.TurnID, AttemptID: envelope.AttemptID, HarnessInstanceID: instance.ID,
			IssuedAt: time.Now().UTC(),
		}, Envelope: envelope,
	}}
	outcome, err := execution.HandleCommand(ctx, command)
	if err != nil || outcome.State != node.CommandAccepted { t.Fatalf("dispatch = %#v err=%v", outcome, err) }
	select {
	case prompt := <-runtime.seen:
		if !strings.Contains(prompt, ref) || !strings.Contains(prompt, ".secretary-artifacts") { t.Fatalf("worker prompt = %q", prompt) }
	case <-time.After(time.Second):
		t.Fatal("remote worker did not read original bytes")
	}
	deadline := time.Now().Add(2*time.Second)
	terminal := false
	for time.Now().Before(deadline) && !terminal {
		events, err := nodeStore.PendingEvents()
		if err != nil { t.Fatal(err) }
		for _, pending := range events {
			var event node.NodeEvent
			if json.Unmarshal(pending.Payload, &event) == nil && event.Outcome != nil &&
				event.Outcome.AttemptID == envelope.AttemptID && event.Outcome.Status == core.OutcomeSucceeded &&
				event.Outcome.Summary == "Verified original bytes" {
				terminal = true
			}
		}
		if !terminal { time.Sleep(10*time.Millisecond) }
	}
	if !terminal { t.Fatal("remote Worker did not produce a durable terminal Result") }

	copyOfResult, err := identity.FetchArtifact(ctx, id)
	if err != nil || !bytes.Equal(copyOfResult.Data, source) { t.Fatalf("second fetch = %v", err) }
	checksum := sha256.Sum256(source)
	if hex.EncodeToString(checksum[:]) != copyOfResult.SHA256 { t.Fatal("checksum lost") }
	if bytes.Equal(corrupt.Data, copyOfResult.Data) { t.Fatal("corrupted client payload was returned") }

	_, conversation, err := store.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil { t.Fatal(err) }
	n := 0
	for _, entry := range entries {
		if strings.Contains(entry.Body, ref) { n++ }
	}
	if n != 1 { t.Fatalf("upload announcement count = %d, want 1", n) }
	if err := store.Close(); err != nil { t.Fatal(err) }
	restarted, err := core.Open(ctx, databasePath)
	if err != nil { t.Fatal(err) }
	defer restarted.Close()
	restored, body, err := restarted.ArtifactForPerson(ctx, conversation.PersonID, id)
	if err != nil || restored.ID != id || !bytes.Equal(body, source) { t.Fatalf("restart original = %#v err=%v", restored, err) }
	if len(identity.Credential) == 0 { t.Fatal("missing persistent node identity") }
	if _, err := base64.RawURLEncoding.DecodeString(identity.Credential); err != nil { t.Fatal(err) }
}
