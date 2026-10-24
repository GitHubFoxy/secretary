package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/beruseruko/secretary/internal/config"
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
