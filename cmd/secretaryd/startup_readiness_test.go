package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/config"
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
