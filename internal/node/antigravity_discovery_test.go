package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

type mockAntigravityObserver struct {
	observeCalls   atomic.Int32
	version        string
	authenticated  bool
	authMethod     string
	modelIDs       []core.ObservedModelID
	reasoning      []core.ObservedReasoningLevel
	err            error
	diagnosticCode string
}

func (m *mockAntigravityObserver) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	return CommandResult{Stdout: m.version, ExitCode: 0}, nil
}

func (m *mockAntigravityObserver) ObserveAntigravity(ctx context.Context, command string) (string, bool, string, []core.ObservedModelID, []core.ObservedReasoningLevel, string, error) {
	m.observeCalls.Add(1)
	return m.version, m.authenticated, m.authMethod, m.modelIDs, m.reasoning, m.diagnosticCode, m.err
}

func createFakeAntigravityInstall(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if eval, err := filepath.EvalSymlinks(dir); err == nil {
		dir = eval
	}
	companionName := "localharness_external"
	execName := "agy_acp_server"
	if runtime.GOOS == "windows" {
		companionName = "localharness_external.exe"
		execName = "agy_acp_server.exe"
	}
	execPath := filepath.Join(dir, execName)
	companionPath := filepath.Join(dir, companionName)

	if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(companionPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return execPath, companionPath
}

func TestAntigravityProbe_MissingBinary(t *testing.T) {
	ResetAntigravityProbeCache()
	spec := DefaultAntigravityProbeSpec()
	spec.Binary = "/nonexistent/agy_acp_server"

	probe := HarnessProbe{Node: "local", Spec: spec, Runner: &mockAntigravityObserver{}}
	result := probe.Probe(context.Background())

	if result.Available() {
		t.Fatal("expected unavailable for missing binary")
	}
	if result.ErrorCode != "missing" {
		t.Fatalf("expected ErrorCode 'missing', got %q", result.ErrorCode)
	}
	if result.Instance.Status != core.HarnessUnavailable {
		t.Fatalf("expected status HarnessUnavailable, got %q", result.Instance.Status)
	}
	if result.Instance.DiagnosticReason == "" {
		t.Fatal("expected non-empty DiagnosticReason")
	}
	if result.Instance.ErrorCode != "missing" {
		t.Fatalf("expected instance ErrorCode 'missing', got %q", result.Instance.ErrorCode)
	}
}

func TestAntigravityProbe_MissingCompanion(t *testing.T) {
	ResetAntigravityProbeCache()
	dir := t.TempDir()
	execName := "agy_acp_server"
	if runtime.GOOS == "windows" {
		execName = "agy_acp_server.exe"
	}
	execPath := filepath.Join(dir, execName)
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := DefaultAntigravityProbeSpec()
	spec.Binary = execPath
	probe := HarnessProbe{Node: "local", Spec: spec, Runner: &mockAntigravityObserver{}}
	result := probe.Probe(context.Background())

	if result.Available() {
		t.Fatal("expected unavailable for missing companion")
	}
	if result.ErrorCode != "companion_missing" {
		t.Fatalf("expected ErrorCode 'companion_missing', got %q", result.ErrorCode)
	}
	if result.Instance.Status != core.HarnessUnavailable {
		t.Fatalf("expected status HarnessUnavailable, got %q", result.Instance.Status)
	}
	if result.Instance.DiagnosticReason == "" {
		t.Fatal("expected non-empty DiagnosticReason")
	}
}

func TestAntigravityProbe_Unauthenticated(t *testing.T) {
	ResetAntigravityProbeCache()
	execPath, _ := createFakeAntigravityInstall(t)
	spec := DefaultAntigravityProbeSpec()
	spec.Binary = execPath

	observer := &mockAntigravityObserver{
		version:        "1.3.0",
		authenticated:  false,
		diagnosticCode: "unauthenticated",
		err:            errors.New("antigravity: sign-in required"),
	}

	probe := HarnessProbe{Node: "local", Spec: spec, Runner: observer}
	result := probe.Probe(context.Background())

	if result.Available() {
		t.Fatal("expected unavailable when unauthenticated")
	}
	if result.ErrorCode != "unauthenticated" {
		t.Fatalf("expected ErrorCode 'unauthenticated', got %q", result.ErrorCode)
	}
	if result.Instance.Status != core.HarnessUnavailable {
		t.Fatalf("expected status HarnessUnavailable, got %q", result.Instance.Status)
	}
	if result.Instance.Authentication.Authenticated {
		t.Fatal("expected instance.Authentication.Authenticated == false")
	}
	if result.Instance.DiagnosticReason == "" {
		t.Fatal("expected non-empty DiagnosticReason")
	}
}

func TestAntigravityProbe_ProtocolMismatch(t *testing.T) {
	ResetAntigravityProbeCache()
	execPath, _ := createFakeAntigravityInstall(t)
	spec := DefaultAntigravityProbeSpec()
	spec.Binary = execPath

	observer := &mockAntigravityObserver{
		diagnosticCode: "protocol_mismatch",
		err:            errors.New("antigravity: unsupported protocol version 2"),
	}

	probe := HarnessProbe{Node: "local", Spec: spec, Runner: observer}
	result := probe.Probe(context.Background())

	if result.Available() {
		t.Fatal("expected unavailable on protocol mismatch")
	}
	if result.ErrorCode != "protocol_mismatch" {
		t.Fatalf("expected ErrorCode 'protocol_mismatch', got %q", result.ErrorCode)
	}
	if result.Instance.Status != core.HarnessUnavailable {
		t.Fatalf("expected status HarnessUnavailable, got %q", result.Instance.Status)
	}
}

func TestAntigravityProbe_Ready(t *testing.T) {
	ResetAntigravityProbeCache()
	execPath, _ := createFakeAntigravityInstall(t)
	spec := DefaultAntigravityProbeSpec()
	spec.Binary = execPath

	observer := &mockAntigravityObserver{
		version:       "1.3.0",
		authenticated: true,
		authMethod:    "oauth-personal",
		modelIDs:      []core.ObservedModelID{"gemini-3.8-flash-high", "gemini-3.8-pro"},
		reasoning:     []core.ObservedReasoningLevel{"default", "high"},
	}

	probe := HarnessProbe{Node: "local", Spec: spec, Runner: observer}
	result := probe.Probe(context.Background())

	if !result.Available() {
		t.Fatalf("expected available, got err=%v code=%s", result.Err, result.ErrorCode)
	}
	if result.Instance.Status != core.HarnessReady {
		t.Fatalf("expected status HarnessReady, got %q", result.Instance.Status)
	}
	if result.Instance.Version != "1.3.0" {
		t.Fatalf("expected version 1.3.0, got %q", result.Instance.Version)
	}
	if !result.Instance.Authentication.Authenticated || result.Instance.Authentication.Method != "oauth-personal" {
		t.Fatalf("unexpected authentication: %#v", result.Instance.Authentication)
	}
	if len(result.Instance.ModelIDs) != 2 || result.Instance.ModelIDs[0] != "gemini-3.8-flash-high" {
		t.Fatalf("unexpected models: %v", result.Instance.ModelIDs)
	}
	// Capabilities checks: Steering, Approvals, multimodal must NOT be present
	if result.Instance.Capabilities.SupportsExecution(core.CapabilitySteering) {
		t.Fatal("Antigravity must not claim CapabilitySteering without verified contract")
	}
	if result.Instance.Capabilities.SupportsExecution(core.CapabilityApprovals) {
		t.Fatal("Antigravity must not claim CapabilityApprovals")
	}
}

func TestAntigravityProbe_CachingPreventsHeavyProcess(t *testing.T) {
	ResetAntigravityProbeCache()
	execPath, companionPath := createFakeAntigravityInstall(t)
	spec := DefaultAntigravityProbeSpec()
	spec.Binary = execPath

	observer := &mockAntigravityObserver{
		version:       "1.3.0",
		authenticated: true,
		authMethod:    "oauth-personal",
		modelIDs:      []core.ObservedModelID{"gemini-3.8-flash-high"},
		reasoning:     []core.ObservedReasoningLevel{"default"},
	}

	probe := HarnessProbe{Node: "local", Spec: spec, Runner: observer}

	// First probe: cache miss, observer invoked
	res1 := probe.Probe(context.Background())
	if !res1.Available() {
		t.Fatalf("res1 failed: %v", res1.Err)
	}
	if observer.observeCalls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", observer.observeCalls.Load())
	}

	// Second probe within TTL: cache hit, observer NOT invoked again
	res2 := probe.Probe(context.Background())
	if !res2.Available() {
		t.Fatalf("res2 failed: %v", res2.Err)
	}
	if observer.observeCalls.Load() != 1 {
		t.Fatalf("expected still 1 call (cache hit), got %d", observer.observeCalls.Load())
	}

	// Touch companion file to modify mtime -> cache invalidated
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(companionPath, future, future); err != nil {
		t.Fatal(err)
	}

	// Third probe: cache miss due to mtime invalidation, observer invoked
	res3 := probe.Probe(context.Background())
	if !res3.Available() {
		t.Fatalf("res3 failed: %v", res3.Err)
	}
	if observer.observeCalls.Load() != 2 {
		t.Fatalf("expected 2 calls after file modification, got %d", observer.observeCalls.Load())
	}
}

func TestHarnessDiscovery_PreservesAntigravityDiagnostics(t *testing.T) {
	ResetAntigravityProbeCache()
	discovery := HarnessDiscovery{
		Node:               "test-node",
		Runner:             ExecCommandRunner{},
		IncludeAntigravity: true,
		BinaryOverrides: map[core.HarnessKind]string{
			core.HarnessAntigravity: "/nonexistent/agy_acp_server",
		},
	}

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	instance, ok := snapshot.Instance("test-node/antigravity")
	if !ok {
		t.Fatal("expected test-node/antigravity instance in snapshot")
	}
	if instance.Status != core.HarnessUnavailable {
		t.Fatalf("expected HarnessUnavailable, got %q", instance.Status)
	}
	if instance.ErrorCode != "missing" {
		t.Fatalf("expected ErrorCode 'missing', got %q", instance.ErrorCode)
	}
	if instance.DiagnosticReason == "" {
		t.Fatal("expected non-empty DiagnosticReason")
	}
}


func TestAntigravityDiscovery_IncludeAntigravity(t *testing.T) {
	ResetAntigravityProbeCache()
	execPath, _ := createFakeAntigravityInstall(t)

	observer := &mockAntigravityObserver{
		version:       "1.3.0",
		authenticated: true,
		authMethod:    "oauth-personal",
		modelIDs:      []core.ObservedModelID{"gemini-3.8-flash-high"},
		reasoning:     []core.ObservedReasoningLevel{"default"},
	}

	discovery := HarnessDiscovery{
		Node:               "test-node",
		Runner:             observer,
		IncludeAntigravity: true,
		BinaryOverrides: map[core.HarnessKind]string{
			core.HarnessAntigravity: execPath,
		},
	}

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	var found *core.HarnessInstance
	for i := range snapshot.Instances {
		if snapshot.Instances[i].Kind == core.HarnessAntigravity {
			found = &snapshot.Instances[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected Antigravity in discovered instances")
	}
	if found.Status != core.HarnessReady {
		t.Fatalf("expected HarnessReady, got %s", found.Status)
	}
	if found.Version != "1.3.0" {
		t.Fatalf("expected 1.3.0, got %s", found.Version)
	}
}

func TestAntigravityDiscovery_PreservesDiagnosticsInInventory(t *testing.T) {
	ResetAntigravityProbeCache()
	execPath, _ := createFakeAntigravityInstall(t)

	observer := &mockAntigravityObserver{
		version:        "1.3.0",
		authenticated:  false,
		diagnosticCode: "unauthenticated",
		err:            errors.New("antigravity: sign-in required"),
	}

	discovery := HarnessDiscovery{
		Node:               "test-node",
		Runner:             observer,
		IncludeAntigravity: true,
		BinaryOverrides: map[core.HarnessKind]string{
			core.HarnessAntigravity: execPath,
		},
	}

	snapshot, err := discovery.Discover(context.Background())
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	var found *core.HarnessInstance
	for i := range snapshot.Instances {
		if snapshot.Instances[i].Kind == core.HarnessAntigravity {
			found = &snapshot.Instances[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected Antigravity in discovered instances")
	}
	if found.Status != core.HarnessUnavailable {
		t.Fatalf("expected HarnessUnavailable, got %s", found.Status)
	}
	if found.ErrorCode != "unauthenticated" {
		t.Fatalf("expected ErrorCode unauthenticated, got %q", found.ErrorCode)
	}
	if found.DiagnosticReason != "antigravity: sign-in required" {
		t.Fatalf("expected DiagnosticReason, got %q", found.DiagnosticReason)
	}
}

func TestClassifyAntigravityError(t *testing.T) {
	cases := []struct {
		err      error
		wantCode string
	}{
		{
			err:      &acp.RPCError{Code: -32000, Message: "auth_required"},
			wantCode: "unauthenticated",
		},
		{
			err:      &acp.RPCError{Code: -32601, Message: "method not found"},
			wantCode: "protocol_mismatch",
		},
		{
			err:      &acp.RPCError{Code: -32001, Message: "mcp server failed"},
			wantCode: "mcp_setup_failed",
		},
		{
			err:      &acp.RPCError{Code: -32002, Message: "invalid config option"},
			wantCode: "config_failed",
		},
		{
			err:      &acp.RPCError{Code: -32603, Message: "internal server error"},
			wantCode: "session_setup_failed",
		},
		{
			err:      errors.New("connection reset by peer"),
			wantCode: "session_setup_failed",
		},
		{
			err:      errors.New("unauthorized request"),
			wantCode: "unauthenticated",
		},
		{
			err:      errors.New("mcp pipe broken"),
			wantCode: "mcp_setup_failed",
		},
	}

	for _, tc := range cases {
		code, diag := classifyAntigravityError(tc.err)
		if code != tc.wantCode {
			t.Errorf("for error %v, got code %q, want %q", tc.err, code, tc.wantCode)
		}
		if diag == nil {
			t.Errorf("for error %v, expected non-nil diagnostic error", tc.err)
		}
	}
}
