package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
)

func TestClaudeOnlyStartupDoesNotRequireCodexInventory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	snapshot := config.Snapshot{Config: config.Config{
		Secretary: config.SecretaryPolicy{Harness: "claude_code"},
		WorkerPolicy: config.WorkerPolicy{DefaultHarness: "claude_code"},
	}}
	if err := prepareSelectedCodexRuntime(context.Background(), snapshot); err != nil {
		t.Fatalf("Claude-only startup depended on Codex: %v", err)
	}
}

func TestStartupFailureClosesMCPListenerAndStopsSecretary(t *testing.T) {
	expected := errors.New("Codex startup failed")
	var address string
	served, stopped := false, false
	listener, err := listenWhenSecretaryReady(func() error {
		if !served {
			t.Fatal("Secretary started before the HTTP transport was served")
		}
		return expected
	}, func() error {
		stopped = true
		return nil
	}, func() (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}, func(l net.Listener) {
		served = true
		address = l.Addr().String()
	})
	if listener != nil || !errors.Is(err, expected) || !stopped {
		t.Fatalf("failed startup: listener=%v err=%v stopped=%v", listener, err, stopped)
	}
	connection, dialErr := net.DialTimeout("tcp", address, time.Second)
	if dialErr == nil {
		_ = connection.Close()
		t.Fatal("failed Secretary startup left the MCP listener open")
	}
}

func TestMCPTransportAvailableBeforePublicSecretaryReadiness(t *testing.T) {
	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/internal/secretary/mcp/observe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/secretary", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Handler: secretaryStartupHandler(&ready, mux)}
	defer server.Close()
	var address string
	listener, err := listenWhenSecretaryReady(func() error {
		mcpResponse, err := http.Post("http://"+address+"/v1/internal/secretary/mcp/observe", "application/json", strings.NewReader("{}"))
		if err != nil {
			return err
		}
		_ = mcpResponse.Body.Close()
		if mcpResponse.StatusCode != http.StatusNoContent {
			t.Fatalf("MCP endpoint status=%d during ACP startup", mcpResponse.StatusCode)
		}
		publicResponse, err := http.Get("http://" + address + "/v1/secretary")
		if err != nil {
			return err
		}
		_ = publicResponse.Body.Close()
		if publicResponse.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("public endpoint status=%d before readiness", publicResponse.StatusCode)
		}
		return nil
	}, nil, func() (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}, func(l net.Listener) {
		address = l.Addr().String()
		go func() { _ = server.Serve(l) }()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready.Store(true)
	response, err := http.Get("http://" + address + "/v1/secretary")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Secretary public endpoint status=%d after readiness", response.StatusCode)
	}
}

func TestListenFailureNeverStartsSecretary(t *testing.T) {
	expected := errors.New("address already in use")
	started, served := false, false
	listener, err := listenWhenSecretaryReady(func() error {
		started = true
		return nil
	}, nil, func() (net.Listener, error) {
		return nil, expected
	}, func(net.Listener) {
		served = true
	})
	if listener != nil || !errors.Is(err, expected) || started || served {
		t.Fatalf("HTTP bind failure: listener=%v err=%v started=%v served=%v", listener, err, started, served)
	}
}

func TestSelectedCodexMissingInventoryClosesHTTPListener(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	snapshot := config.Snapshot{Config: config.Config{
		Secretary: config.SecretaryPolicy{Harness: "codex"},
		WorkerPolicy: config.WorkerPolicy{DefaultHarness: "codex"},
	}}
	served := false
	listener, err := listenWhenSecretaryReady(func() error {
		return prepareSelectedCodexRuntime(context.Background(), snapshot)
	}, nil, func() (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}, func(net.Listener) {
		served = true
	})
	if err == nil || !strings.Contains(err.Error(), "inventory missing") || !served || listener != nil {
		t.Fatalf("missing Codex inventory permitted readiness: err=%v listener=%v served=%v", err, listener, served)
	}
}

func TestWorkerCodexSelectionDoesNotBlockIndependentClaudeSecretary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	snapshot := config.Snapshot{Config: config.Config{
		Secretary: config.SecretaryPolicy{Harness: "claude_code"},
		WorkerPolicy: config.WorkerPolicy{DefaultHarness: "codex"},
	}}
	if err := prepareSelectedCodexRuntime(context.Background(), snapshot); err != nil {
		t.Fatalf("offline Worker Codex inventory prevented persistent Secretary startup: %v", err)
	}
}

func TestSecretaryMCPReadinessRequiresCurrentGenerationAndTools(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.BeginSecretaryRuntime(ctx, identity.ID, capability, "codex", "", "")
	if err != nil {
		t.Fatal(err)
	}
	observation := func(token string, event core.SecretaryMCPObservation) {
		t.Helper()
		if err := store.RecordSecretaryMCPObservation(ctx, person.ID, token, event); err != nil {
			t.Fatal(err)
		}
	}
	observation(first.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationStartup, Success: true})
	observation(first.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationInitialize, Success: true})
	observation(first.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationToolsList, Success: true, ToolCount: 11, HasSpawnWorker: true, HasReplyToUser: true})
	identity.RuntimeGeneration = first.Generation
	if err := waitForSecretaryMCPReadiness(ctx, store, identity); err != nil {
		t.Fatalf("current MCP generation was rejected: %v", err)
	}
	second, err := store.BeginSecretaryRuntime(ctx, identity.ID, capability, "codex", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForSecretaryMCPReadiness(ctx, store, identity); err == nil {
		t.Fatal("revoked prior MCP generation was accepted")
	}
	identity.RuntimeGeneration = second.Generation
	expired, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := waitForSecretaryMCPReadiness(expired, store, identity); err == nil {
		t.Fatal("current ACP session/new without MCP observation was accepted")
	}
	observation(second.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationStartup, Success: true})
	observation(second.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationInitialize, Success: true})
	expiredAgain, cancelAgain := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancelAgain()
	if err := waitForSecretaryMCPReadiness(expiredAgain, store, identity); err == nil {
		t.Fatal("MCP initialize without tools/list was accepted")
	}
	observation(second.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationToolsList, Success: true, ToolCount: 10, HasSpawnWorker: true})
	if err := waitForSecretaryMCPReadiness(ctx, store, identity); err == nil {
		t.Fatal("MCP catalogue missing reply_to_user was accepted")
	}
	third, err := store.BeginSecretaryRuntime(ctx, identity.ID, capability, "codex", "", "")
	if err != nil {
		t.Fatal(err)
	}
	identity.RuntimeGeneration = third.Generation
	observation(third.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationStartup, Success: true})
	observation(third.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationInitialize, Success: true})
	observation(third.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationToolsList, Success: true, ToolCount: 11, HasSpawnWorker: false, HasReplyToUser: true})
	if err := waitForSecretaryMCPReadiness(ctx, store, identity); err == nil {
		t.Fatal("MCP catalogue missing spawn_worker was accepted")
	}
	fourth, err := store.BeginSecretaryRuntime(ctx, identity.ID, capability, "codex", "", "")
	if err != nil {
		t.Fatal(err)
	}
	identity.RuntimeGeneration = fourth.Generation
	observation(fourth.ObserverCapability, core.SecretaryMCPObservation{Phase: core.SecretaryMCPObservationStartup, Success: false})
	if err := waitForSecretaryMCPReadiness(ctx, store, identity); err == nil {
		t.Fatal("failed MCP startup was accepted")
	}
}
