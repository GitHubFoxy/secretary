package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

type t08Inventory struct{ nodeID core.NodeReference }

func (i t08Inventory) Discover(context.Context) (core.HarnessInventorySnapshot, error) {
	return core.HarnessInventorySnapshot{
		Node: i.nodeID, ObservedAt: time.Now().UTC(),
		Instances: []core.HarnessInstance{{
			ID: core.HarnessInstanceID(string(i.nodeID) + "/fx"),
			Node: i.nodeID, Kind: core.HarnessFX, Version: "test",
			Authentication: core.HarnessAuthentication{Authenticated: true},
			Status: core.HarnessReady,
			Capabilities: core.HarnessCapabilities{
				Execution: []core.ExecutionCapability{core.CapabilityCancel},
				Activity: []core.ActivityCapability{core.ActivityStatus},
			},
		}},
	}, nil
}

func t08Eventually(t *testing.T, ctx context.Context, what string, ready func() bool) {
	t.Helper()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready() { return }
		select {
		case <-ctx.Done(): t.Fatalf("timed out waiting for %s: %v", what, ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestT08WebSocketDaemonResultAndApplicationRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	database := filepath.Join(t.TempDir(), "server.db")
	localPath := filepath.Join(t.TempDir(), "remote-node.json")
	identityPath := filepath.Join(t.TempDir(), "remote-identity.json")
	nodeID := core.NodeReference("artifact-remote")

	var muxMu sync.RWMutex
	current := http.Handler(http.NotFoundHandler())
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		muxMu.RLock()
		handler := current
		muxMu.RUnlock()
		handler.ServeHTTP(w, r)
	}))
	defer front.Close()
	install := func(handler http.Handler) {
		muxMu.Lock()
		current = handler
		muxMu.Unlock()
	}

	startApp := func() (*core.Store, *node.ServerManager, string) {
		t.Helper()
		store, err := core.Open(ctx, database)
		if err != nil { t.Fatal(err) }
		api, err := New(ctx, store, "bootstrap")
		if err != nil { t.Fatal(err) }
		manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{
			PairingTokens: []string{"t08-pair-token"}, AdminToken: "t08-admin-token",
		})
		if err != nil { t.Fatal(err) }
		manager.SetEventSink(node.NewStoreEventSink(store))
		manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
		api.AttachNodeService(manager)
		capability, err := store.RotateSecretaryCapability(ctx, api.OwnerID())
		if err != nil { t.Fatal(err) }
		profile := node.ManagedProfile{
			Version: "1", Name: "worker", Content: "Read files provided by the owner",
			SourceHash: "t08-source", Runtime: string(core.HarnessFX),
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
		install(api.Handler())
		return store, manager, capability
	}

	store, manager, capability := startApp()
	identity, err := node.EnrollNode(ctx, front.Client(), front.URL, "t08-pair-token", nodeID)
	if err != nil { t.Fatal(err) }
	if err := node.SaveNodeIdentity(identityPath, identity); err != nil { t.Fatal(err) }

	source := []byte("%PDF-1.7\n\x00\x01\xff\nOriginal, unmodified, binary content.\n")
	jar, err := cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	client := &http.Client{Jar: jar}
	login(t, client, front.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, front.URL+"/v1/artifacts?name=report.pdf", bytes.NewReader(source))
	if err != nil { t.Fatal(err) }
	req.Header.Set("Idempotency-Key", "t08-full-e2e-upload")
	res, err := client.Do(req)
	if err != nil { t.Fatal(err) }
	var upload struct {
		Reference string        `json:"reference"`
		Artifact core.Artifact `json:"artifact"`
	}
	err = json.NewDecoder(res.Body).Decode(&upload)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusCreated || !node.ValidArtifactID(upload.Artifact.ID) ||
		upload.Reference != "artifact://"+upload.Artifact.ID {
		t.Fatalf("upload status=%d value=%#v err=%v", res.StatusCode, upload, err)
	}

	var entryCount = func() (int, int, error) {
		res, err := client.Get(front.URL + "/v1/conversation?after_seq=0")
		if err != nil { return 0, 0, err }
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK { return 0, 0, fmt.Errorf("conversation HTTP %d", res.StatusCode) }
		var page struct{ Entries []core.ConversationEntry `json:"entries"` }
		if err := json.NewDecoder(res.Body).Decode(&page); err != nil { return 0, 0, err }
		uploads, results := 0, 0
		for _, entry := range page.Entries {
			if entry.Kind == core.EntrySystem && strings.Contains(entry.Body, upload.Reference) { uploads++ }
			if entry.Kind == core.EntryWorkerResult && entry.Body == "Verified original bytes" { results++ }
		}
		return uploads, results, nil
	}

	startDaemon := func(manager *node.ServerManager) (func(), *artifactWorkerRuntime) {
		t.Helper()
		loaded, err := node.LoadNodeIdentity(identityPath)
		if err != nil || loaded != identity { t.Fatalf("Node identity restore: %#v %v", loaded, err) }
		nodeStore, err := node.OpenLocalStore(localPath)
		if err != nil { t.Fatal(err) }
		runtime := &artifactWorkerRuntime{source: source, seen: make(chan string, 4)}
		daemon := &node.Daemon{
			Identity: loaded, Store: nodeStore, Runtime: runtime,
			Inventory: t08Inventory{nodeID: nodeID}, Capacity: 2,
			HeartbeatInterval: 30 * time.Millisecond, OutboxPollInterval: 10 * time.Millisecond,
			InventoryInterval: time.Hour, ReconnectMin: 10 * time.Millisecond,
			ReconnectMax: 40 * time.Millisecond,
		}
		daemonCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- daemon.Run(daemonCtx) }()
		t08Eventually(t, ctx, "authenticated Node connection", func() bool {
			ready, err := manager.CommandReady(context.Background(), nodeID)
			return err == nil && ready
		})
		return func() {
			stop()
			select {
			case err := <-done:
				if err != nil { t.Errorf("Node Daemon shutdown: %v", err) }
			case <-time.After(2 * time.Second):
				t.Error("Node Daemon failed to shut down")
			}
			nodeStore.Close()
		}, runtime
	}

	spawn := func(capability, key string) core.WorkerDetails {
		t.Helper()
		args := map[string]any{
			"intent": "Inspect "+upload.Reference+" and return verified bytes",
			"idempotency_key": key, "node_id": string(nodeID), "harness_kind": "fx",
		}
		payload, _ := json.Marshal(map[string]any{"name": "spawn_worker", "arguments": args})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, front.URL+"/v1/internal/secretary/tools/call", bytes.NewReader(payload))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+capability)
		res, err := front.Client().Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		var result struct {
			Value core.WorkerDetails `json:"value"`
			Error string             `json:"error"`
		}
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil { t.Fatal(err) }
		if res.StatusCode != http.StatusOK || result.Error != "" || result.Value.Worker.ID == "" {
			t.Fatalf("worker API: status %d error %q value %#v", res.StatusCode, result.Error, result.Value)
		}
		return result.Value
	}

	stopDaemon, workerRuntime := startDaemon(manager)
	first := spawn(capability, "t08-first-worker")
	select {
	case prompt := <-workerRuntime.seen:
		if !strings.Contains(prompt, upload.Reference) || !strings.Contains(prompt, ".secretary-artifacts") {
			t.Fatalf("Worker received no file mapping: %q", prompt)
		}
	case <-ctx.Done(): t.Fatal("first Worker did not read original file")
	}
	t08Eventually(t, ctx, "first terminal Result delivered to Personal Conversation", func() bool {
		uploads, results, err := entryCount()
		return err == nil && uploads == 1 && results == 1
	})
	replay := spawn(capability, "t08-first-worker")
	if replay.Worker.ID != first.Worker.ID { t.Fatal("duplicate dispatch created another Worker") }
	select {
	case <-workerRuntime.seen: t.Fatal("idempotent replay dispatched Worker twice")
	case <-time.After(60 * time.Millisecond):
	}
	stopDaemon()
	if err := store.Close(); err != nil { t.Fatal(err) }

	restartedStore, restartedManager, restartedCapability := startApp()
	defer restartedStore.Close()
	stopRestarted, newRuntime := startDaemon(restartedManager)
	defer stopRestarted()
	second := spawn(restartedCapability, "t08-after-restart")
	if second.Worker.ID == first.Worker.ID { t.Fatal("restart reused an unrelated Worker identity") }
	select {
	case prompt := <-newRuntime.seen:
		if !strings.Contains(prompt, upload.Reference) { t.Fatalf("same Artifact not available after restart: %q", prompt) }
	case <-ctx.Done(): t.Fatal("restarted Daemon could not use persisted Artifact")
	}
	t08Eventually(t, ctx, "second terminal Result in restarted Personal Conversation", func() bool {
		uploads, results, err := entryCount()
		return err == nil && uploads == 1 && results == 2
	})
	owner, _, err := restartedStore.EnsureOwner(ctx)
	if err != nil { t.Fatal(err) }
	restored, original, err := restartedStore.ArtifactForPerson(ctx, owner.ID, upload.Artifact.ID)
	if err != nil || restored.ID != upload.Artifact.ID || !bytes.Equal(original, source) {
		t.Fatalf("restarted canonical Artifact corrupted: %#v err=%v", restored, err)
	}
}
