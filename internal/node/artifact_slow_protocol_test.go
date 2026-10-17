package node

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT08SlowArtifactDownloadDoesNotBlockHeartbeatOrACK(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "slow-pair", "slow-admin")
	if err != nil { t.Fatal(err) }
	manager.SetEventSink(func(context.Context, NodeEvent) error { return nil })
	outcomes := make(chan CommandOutcome, 2)
	manager.SetCommandOutcomeSink(func(_ context.Context, _ core.NodeReference, outcome CommandOutcome) error {
		outcomes <- outcome
		return nil
	})
	artifactID := "art_" + strings.Repeat("a", 32)
	source := []byte("slow but intact source")
	hash := sha256.Sum256(source)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	mux.HandleFunc("GET /v1/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != artifactID { http.NotFound(w, r); return }
		nodeRef := core.NodeReference(r.Header.Get("X-Secretary-Node"))
		epoch, err := strconv.ParseInt(r.Header.Get("X-Artifact-Timestamp"), 10, 64)
		credential, credentialErr := store.NodeCredential(r.Context(), nodeRef)
		if err != nil || credentialErr != nil || NewAuthenticator(credential).VerifyArtifactRequest(nodeRef, artifactID, epoch, r.Header.Get("X-Artifact-Signature"), time.Now()) != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		select { case started <- struct{}{}: default: }
		select {
		case <-release:
		case <-r.Context().Done(): return
		}
		w.Header().Set("X-Artifact-SHA256", hex.EncodeToString(hash[:]))
		w.Header().Set("X-Artifact-Name", base64.RawURLEncoding.EncodeToString([]byte("slow.txt")))
		w.Header().Set("Content-Length", fmt.Sprint(len(source)))
		w.Write(source)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "slow-pair", "slow-node")
	if err != nil { t.Fatal(err) }
	nodeStore, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil { t.Fatal(err) }
	defer nodeStore.Close()
	runtime := &daemonCountingRuntime{}
	daemon := &Daemon{
		Identity: identity, Store: nodeStore, Runtime: runtime,
		Inventory: daemonStaticInventory{snapshot: daemonInventoryFixture(identity.Node)},
		Capacity: 1, HeartbeatInterval: 25 * time.Millisecond,
		OutboxPollInterval: 10 * time.Millisecond, InventoryInterval: time.Hour,
		ReconnectMin: 10 * time.Millisecond, ReconnectMax: 40 * time.Millisecond,
	}
	daemonCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- daemon.Run(daemonCtx) }()
	defer func() {
		stop()
		releaseOnce.Do(func() { close(release) })
		select {
		case err := <-done:
			if err != nil { t.Errorf("daemon exit: %v", err) }
		case <-time.After(time.Second): t.Error("daemon shutdown timed out")
		}
	}()
	waitFor(t, ctx, "slow-test Node connection", func() bool {
		ready, err := manager.CommandReady(context.Background(), identity.Node)
		return err == nil && ready
	})
	command := daemonDispatchFixture(identity.Node, "slow-command")
	command.Dispatch.Envelope.OriginalUserIntent = "Process artifact://" + artifactID
	if err := manager.SendCommand(ctx, identity.Node, command); err != nil { t.Fatal(err) }
	select {
	case <-started:
	case <-ctx.Done(): t.Fatal("file fetch did not start")
	}
	fast := daemonDispatchFixture(identity.Node, "fast-command")
	fast.Dispatch.Metadata.WorkerRef, fast.Dispatch.Envelope.WorkerRef = "independent-worker", "independent-worker"
	fast.Dispatch.Metadata.TurnID, fast.Dispatch.Envelope.TurnID = "independent-turn", "independent-turn"
	fast.Dispatch.Metadata.AttemptID, fast.Dispatch.Envelope.AttemptID = "independent-attempt", "independent-attempt"
	fast.Dispatch.Envelope.OriginalUserIntent = "independent work"
	if err := manager.SendCommand(ctx, identity.Node, fast); err != nil { t.Fatal(err) }
	select {
	case outcome := <-outcomes:
		if outcome.CommandID != "fast-command" || outcome.State != CommandAccepted {
			t.Fatalf("unrelated command was blocked by staging: %#v", outcome)
		}
	case <-ctx.Done(): t.Fatal("unrelated Worker did not dispatch during slow transfer")
	}
	before, err := manager.Status(ctx, identity.Node)
	if err != nil { t.Fatal(err) }
	_, err = nodeStore.QueueOutcome(core.AttemptOutcomeEnvelope{
		EventID: "unrelated-event-during-fetch", Node: identity.Node,
		HarnessInstanceID: command.Dispatch.Envelope.HarnessInstance.ID,
		WorkerRef: "other-worker", TurnID: "other-turn", AttemptID: "other-attempt",
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal,
		Summary: "unrelated delivery", OccurredAt: time.Now().UTC(),
	})
	if err != nil { t.Fatal(err) }
	waitFor(t, ctx, "heartbeat and outbox ACK during blocked download", func() bool {
		record, err := manager.Status(context.Background(), identity.Node)
		if err != nil || !record.Online || !record.LastHeartbeatAt.After(before.LastHeartbeatAt) { return false }
		pending, err := nodeStore.PendingEvents()
		return err == nil && len(pending) == 0
	})
	if runtime.starts.Load() != 1 { t.Fatalf("expected only unrelated Worker to start, got %d", runtime.starts.Load()) }
	releaseOnce.Do(func() { close(release) })
	select {
	case outcome := <-outcomes:
		if outcome.CommandID != "slow-command" || outcome.State != CommandAccepted {
			t.Fatalf("slow dispatch result: %#v", outcome)
		}
	case <-ctx.Done(): t.Fatal("Worker was not dispatched after slow download completed")
	}
	if runtime.starts.Load() != 2 { t.Fatalf("both Workers should start exactly once, got %d", runtime.starts.Load()) }
}
