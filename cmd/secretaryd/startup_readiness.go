package main

import (
 "context"
 "net"

 "github.com/beruseruko/secretary/internal/config"
 "github.com/beruseruko/secretary/internal/node"
)

func prepareSelectedCodexRuntime(ctx context.Context, snapshot config.Snapshot) error {
 secretaryHarness := snapshot.Config.EffectiveSecretaryPolicy().Harness
 if secretaryHarness != "codex" {
  node.DisableUnselectedCodexRuntime()
  return nil
 }
 if err := node.CheckCodexStartup(ctx, "server"); err != nil { return err }
 return node.ApplyCodexPairInventory("server")
}

func listenWhenSecretaryReady(start func() error, stop func() error, listen func() (net.Listener, error)) (net.Listener, error) {
 if err := start(); err != nil { return nil, err }
 listener, err := listen()
 if err != nil {
  if stop != nil { _ = stop() }
  return nil, err
 }
 return listener, nil
}
