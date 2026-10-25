package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func prepareSelectedCodexRuntime(ctx context.Context, snapshot config.Snapshot) error {
	secretaryHarness := snapshot.Config.EffectiveSecretaryPolicy().Harness
	if secretaryHarness != "codex" {
		node.DisableUnselectedCodexRuntime()
		return nil
	}
	if err := node.CheckCodexStartup(ctx, "server"); err != nil {
		return err
	}
	return node.ApplyCodexPairInventory("server")
}

func waitForSecretaryMCPReadiness(ctx context.Context, store *core.Store, identity core.SecretaryIdentity) error {
	if identity.ID == "" || identity.RuntimeGeneration <= 0 {
		return errors.New("Secretary MCP runtime generation is missing")
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		discovery, err := store.SecretaryRuntimeMCPDiscovery(ctx, identity.ID, identity.RuntimeGeneration)
		if err != nil {
			return fmt.Errorf("read Secretary MCP runtime discovery: %w", err)
		}
		if discovery.Revoked || !discovery.GenerationMatches || discovery.Failed {
			return fmt.Errorf("Secretary MCP runtime generation %d is unavailable: %s", identity.RuntimeGeneration, discovery.Code)
		}
		if discovery.Started && discovery.Initialized && discovery.ToolsListed {
			if !discovery.HasReplyToUser || !discovery.HasSpawnWorker {
				return fmt.Errorf("Secretary MCP runtime generation %d lacks required addressed-reply or Worker tools", identity.RuntimeGeneration)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Secretary MCP initialize/tools/list were not confirmed for runtime generation %d: %w", identity.RuntimeGeneration, ctx.Err())
		case <-ticker.C:
		}
	}
}

func secretaryStartupHandler(ready *atomic.Bool, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() && !(r.Method == http.MethodPost && (r.URL.Path == "/v1/internal/secretary/mcp/observe" || r.URL.Path == "/v1/internal/secretary/tools/call")) {
			http.Error(w, "Secretary is starting", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

func listenWhenSecretaryReady(start func() error, stop func() error, listen func() (net.Listener, error), serve func(net.Listener)) (net.Listener, error) {
	listener, err := listen()
	if err != nil {
		return nil, err
	}
	serve(listener)
	if err := start(); err != nil {
		_ = listener.Close()
		if stop != nil {
			err = errors.Join(err, stop())
		}
		return nil, err
	}
	return listener, nil
}
