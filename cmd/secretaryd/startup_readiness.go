package main

import (
 "context"
 "net"

 "github.com/beruseruko/secretary/internal/config"
 "github.com/beruseruko/secretary/internal/node"
)

func prepareSelectedCodexRuntime(ctx context.Context, snapshot config.Snapshot) error {
 secretaryHarness := snapshot.Config.EffectiveSecretaryPolicy().Harness
 workerHarness := snapshot.Config.EffectiveWorkerPolicy().DefaultHarness
 if secretaryHarness != "codex" && workerHarness != "codex" {
  node.DisableUnselectedCodexRuntime()
  return nil
 }
 if err := node.CheckCodexStartup(ctx, "server"); err != nil { return err }
 return node.ApplyCodexPairInventory("server")
}

func listenWhenSecretaryReady(start func() error, listen func() (net.Listener, error)) (net.Listener, error) {
 if err := start(); err != nil { return nil, err }
 return listen()
}
