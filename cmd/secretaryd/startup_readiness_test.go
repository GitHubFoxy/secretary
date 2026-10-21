package main

import (
 "context"
 "errors"
 "net"
 "strings"
 "testing"

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

func TestStartupFailureDoesNotExposeHTTPListener(t *testing.T) {
 expected := errors.New("Codex inventory missing")
 called := false
 listener, err := listenWhenSecretaryReady(func() error { return expected }, nil, func() (net.Listener, error) {
  called = true
  return net.Listen("tcp", "127.0.0.1:0")
 })
 if listener != nil || !errors.Is(err, expected) || called {
  t.Fatalf("HTTP listener started before runtime readiness: listener=%v err=%v called=%v", listener, err, called)
 }
}

func TestSuccessfulRuntimeStartsBeforeHTTPListener(t *testing.T) {
 started := false
 listener, err := listenWhenSecretaryReady(func() error {
  started = true
  return nil
 }, nil, func() (net.Listener, error) {
  if !started { return nil, errors.New("HTTP listener before runtime") }
  return net.Listen("tcp", "127.0.0.1:0")
 })
 if err != nil { t.Fatal(err) }
 defer listener.Close()
 if listener.Addr() == nil || !strings.Contains(listener.Addr().String(), ":") {
  t.Fatalf("HTTP listener unavailable after Secretary start: %v", listener.Addr())
 }
}

func TestListenFailureStopsStartedSecretary(t *testing.T) {
 expected := errors.New("address already in use")
 stopped := false
 listener, err := listenWhenSecretaryReady(func() error { return nil }, func() error {
  stopped = true
  return nil
 }, func() (net.Listener, error) {
  return nil, expected
 })
 if listener != nil || !errors.Is(err, expected) {
  t.Fatalf("listen failure was not reported: listener=%v err=%v", listener, err)
 }
 if !stopped {
  t.Fatal("persistent Secretary was left running after the HTTP listener failed")
 }
}

func TestSelectedCodexMissingInventoryDoesNotBindHTTP(t *testing.T) {
 home := t.TempDir()
 t.Setenv("HOME", home)
 t.Setenv("USERPROFILE", home)
 snapshot := config.Snapshot{Config: config.Config{
  Secretary: config.SecretaryPolicy{Harness: "codex"},
  WorkerPolicy: config.WorkerPolicy{DefaultHarness: "codex"},
 }}
 called := false
 listener, err := listenWhenSecretaryReady(func() error {
  return prepareSelectedCodexRuntime(context.Background(), snapshot)
 }, nil, func() (net.Listener, error) {
  called = true
  return net.Listen("tcp", "127.0.0.1:0")
 })
 if err == nil || !strings.Contains(err.Error(), "inventory missing") || called || listener != nil {
  t.Fatalf("missing Codex inventory permitted HTTP readiness: err=%v listener=%v bind=%v", err, listener, called)
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
