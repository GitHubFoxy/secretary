package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

type t09PublishingRuntime struct {
	data  []byte
	ready chan core.Artifact
}

func (r *t09PublishingRuntime) Start(ctx context.Context, request node.StartRequest) (node.Session, error) {
	var publisher node.ArtifactPublisher
	found := false
	for _, server := range request.MCPServers {
		if server.Name != "secretary_artifacts" {
			continue
		}
		found = true
		for _, env := range server.Env {
			switch env.Name {
			case "SECRETARY_ARTIFACT_SERVER":
				publisher.ServerURL = env.Value
			case "SECRETARY_ARTIFACT_NODE":
				publisher.Node = core.NodeReference(env.Value)
			case "SECRETARY_ARTIFACT_WORKER":
				publisher.WorkerRef = env.Value
			case "SECRETARY_ARTIFACT_ATTEMPT":
				publisher.AttemptID = env.Value
			case "SECRETARY_ARTIFACT_WORKSPACE":
				publisher.Workspace = env.Value
			case "SECRETARY_ARTIFACT_CAPABILITY":
				publisher.Capability = env.Value
			}
		}
	}
	if !found || publisher.Workspace != request.Workspace || publisher.WorkerRef != request.WorkerRef {
		return nil, errors.New("Worker did not receive its scoped Artifact publisher")
	}
	path := filepath.Join(request.Workspace, "result.zip")
	if err := os.WriteFile(path, r.data, 0o600); err != nil {
		return nil, err
	}
	artifact, err := publisher.Publish(ctx, "result.zip", "result.zip")
	if err != nil {
		return nil, err
	}
	again, err := publisher.Publish(ctx, "result.zip", "result.zip")
	if err != nil || again.ID != artifact.ID {
		return nil, fmt.Errorf("duplicate publish not idempotent: %v", err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		return nil, err
	}
	if _, err := publisher.Publish(ctx, "result.zip", "result.zip"); err == nil {
		return nil, errors.New("changed output was accepted under same publication key")
	}
	r.ready <- artifact
	out := make(chan node.Result, 1)
	out <- node.Result{Status: "succeeded", Summary: "Generated archive"}
	return &artifactWorkerSession{result: out}, nil
}

func TestT09RemoteWorkerPublishToWebResultAndRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	db := filepath.Join(t.TempDir(), "server.db")
	nodeID := core.NodeReference("t09-remote")
	data := []byte{'P', 'K', 0, 255, '\n', 1, 2, 3}
	store, err := core.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
		PairingTokens: []string{"t09-pair"}, AdminToken: "t09-admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetEventSink(node.NewStoreEventSink(store))
	manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	api.AttachNodeService(manager)
	capability, err := store.RotateSecretaryCapability(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	profile := node.ManagedProfile{
		Version: "1", Name: "worker", Content: "Generate a ZIP",
		SourceHash: "t09-source", Runtime: string(core.HarnessFX),
	}
	profile.Hash = profile.SnapshotHash()
	service := ctl.WorkerService{
		Store: store, PersonID: api.OwnerID(), Capability: capability,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX},
		WorkerProfileSource: func() (node.ManagedProfile, error) { return profile, nil },
		Runtime: ctl.NodeRuntime{Manager: manager},
	}
	api.AttachSecretaryWorkerTools(service)
	api.AttachWorkerResponder(service)
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "t09-pair", nodeID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &t09PublishingRuntime{data: data, ready: make(chan core.Artifact, 1)}
	local, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	daemon := &node.Daemon{
		Identity: identity, Store: local, Runtime: runtime,
		Inventory: t08Inventory{nodeID: nodeID}, Capacity: 1,
		HeartbeatInterval: 25 * time.Millisecond, OutboxPollInterval: 10 * time.Millisecond,
		InventoryInterval: time.Hour, ReconnectMin: 10 * time.Millisecond,
		ReconnectMax: 40 * time.Millisecond,
	}
	daemonCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- daemon.Run(daemonCtx) }()
	stopped := false
	stopDaemon := func() {
		if stopped {
			return
		}
		stopped = true
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Node Daemon shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Node Daemon shutdown timed out")
		}
		local.Close()
	}
	defer stopDaemon()
	t08Eventually(t, ctx, "remote Node connection", func() bool {
		ready, err := manager.CommandReady(context.Background(), nodeID)
		return err == nil && ready
	})
	args := map[string]any{
		"intent": "Create and publish a ZIP archive", "idempotency_key": "t09-create",
		"node_id": string(nodeID), "harness_kind": "fx",
	}
	payload, _ := json.Marshal(map[string]any{"name": "spawn_worker", "arguments": args})
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/internal/secretary/tools/call", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("Authorization", "Bearer "+capability)
	res, err := server.Client().Do(post)
	if err != nil {
		t.Fatal(err)
	}
	var spawned struct {
		Value core.WorkerDetails
		Error string
	}
	if err := json.NewDecoder(res.Body).Decode(&spawned); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || spawned.Error != "" || spawned.Value.Worker.ID == "" {
		t.Fatalf("spawn response %d: %#v", res.StatusCode, spawned)
	}
	var artifact core.Artifact
	select {
	case artifact = <-runtime.ready:
	case <-ctx.Done():
		t.Fatal("remote Worker did not publish output")
	}
	if !node.ValidArtifactID(artifact.ID) || artifact.Size != int64(len(data)) {
		t.Fatalf("invalid output Artifact: %#v", artifact)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	login(t, client, server.URL)
	ref := "artifact://" + artifact.ID
	t08Eventually(t, ctx, "terminal Result with owner-download link", func() bool {
		pageResponse, err := client.Get(server.URL + "/v1/conversation?after_seq=0")
		if err != nil {
			return false
		}
		defer pageResponse.Body.Close()
		var page struct { Entries []core.ConversationEntry }
		if err := json.NewDecoder(pageResponse.Body).Decode(&page); err != nil {
			return false
		}
		for _, entry := range page.Entries {
			if entry.Kind == core.EntryWorkerResult && strings.Contains(entry.Body, "Generated archive") &&
				strings.Contains(entry.Body, ref) && strings.Contains(entry.Body, "/v1/artifacts/"+artifact.ID+"?download=1") {
				return true
			}
		}
		return false
	})
	download, err := client.Get(server.URL + "/v1/artifacts/" + artifact.ID + "?download=1")
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(download.Body)
	download.Body.Close()
	if readErr != nil || download.StatusCode != http.StatusOK || !bytes.Equal(got, data) {
		t.Fatalf("owner download: HTTP %d, bytes %v, error %v", download.StatusCode, got, readErr)
	}
	guest, err := http.Get(server.URL + "/v1/artifacts/" + artifact.ID + "?download=1")
	if err != nil {
		t.Fatal(err)
	}
	guest.Body.Close()
	if guest.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized download succeeded: %d", guest.StatusCode)
	}
	authRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/artifacts/publish", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	authRequest.Header.Set("X-Secretary-Node", string(nodeID))
	authRequest.Header.Set("X-Artifact-Worker", spawned.Value.Worker.WorkerRef)
	authRequest.Header.Set("X-Artifact-Attempt", spawned.Value.Attempts[0].ID)
	authRequest.Header.Set("X-Artifact-Publish-Token", "invalid")
	authRequest.Header.Set("X-Artifact-Name", "leak.zip")
	authRequest.Header.Set("Idempotency-Key", "invalid")
	denied, err := server.Client().Do(authRequest)
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized publication succeeded: %d", denied.StatusCode)
	}
	turnID := spawned.Value.Turns[0].ID
	currentResult, err := store.Phase4Result(ctx, turnID)
	if err != nil || !strings.Contains(currentResult.ArtifactRefs, ref) {
		t.Fatalf("terminal Result has no ArtifactRefs: %#v %v", currentResult, err)
	}
	owner, _, err := store.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stopDaemon()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := core.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	persistedArtifact, savedBytes, err := restarted.ArtifactForPerson(ctx, owner.ID, artifact.ID)
	if err != nil || persistedArtifact.ID != artifact.ID || !bytes.Equal(savedBytes, data) {
		t.Fatalf("published output did not survive restart: %#v %v", persistedArtifact, err)
	}
	savedResult, err := restarted.Phase4Result(ctx, turnID)
	if err != nil || !strings.Contains(savedResult.ArtifactRefs, ref) || !strings.Contains(savedResult.Summary, ref) {
		t.Fatalf("terminal Result did not survive restart: %#v %v", savedResult, err)
	}
}
