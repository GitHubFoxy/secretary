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
 t.Setenv("HOME", t.TempDir())
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
 listener, err := listenWhenSecretaryReady(func() error { return expected }, func() (net.Listener, error) {
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
 }, func() (net.Listener, error) {
  if !started { return nil, errors.New("HTTP listener before runtime") }
  return net.Listen("tcp", "127.0.0.1:0")
 })
 if err != nil { t.Fatal(err) }
 defer listener.Close()
 if listener.Addr() == nil || !strings.Contains(listener.Addr().String(), ":") {
  t.Fatalf("HTTP listener unavailable after Secretary start: %v", listener.Addr())
 }
}
