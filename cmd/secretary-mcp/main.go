package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/mcp"
)

func main() {
	log.SetOutput(os.Stderr)
	dataDefault := os.Getenv("SECRETARY_MCP_DATA_DIR")
	if dataDefault == "" {
		dataDefault = defaultDataDir()
	}
	dataDir := flag.String("data-dir", dataDefault, "directory containing Secretary durable state")
	role := flag.String("role", os.Getenv("SECRETARY_MCP_ROLE"), "managed role: secretary")
	flag.Parse()
	if *role == "" {
		*role = "secretary"
	}
	if *role != "secretary" {
		log.Fatalf("unknown MCP role %q", *role)
	}
	capability := os.Getenv("SECRETARY_MCP_CAPABILITY")
	if capability == "" {
		log.Fatal("SECRETARY_MCP_CAPABILITY is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := core.Open(ctx, filepath.Join(*dataDir, "secretary.db"))
	if err != nil {
		log.Fatalf("open Secretary state: %v", err)
	}
	defer store.Close()
	owner, _, err := store.EnsureOwner(ctx)
	if err != nil {
		log.Fatalf("find owner: %v", err)
	}
	var secretaryHandler mcp.Handler
	if serverURL := os.Getenv("SECRETARY_MCP_SERVER_URL"); serverURL != "" {
		secretaryHandler = mcp.RemoteSecretary{BaseURL: serverURL, Capability: capability}
	} else {
		manager, err := config.Open(filepath.Join(*dataDir, "config.toml"))
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		policy := manager.Snapshot().Config.EffectiveWorkerPolicy()
		preferred := make([]core.HarnessKind, len(policy.PreferredHarnesses))
		for i, kind := range policy.PreferredHarnesses {
			preferred[i] = core.HarnessKind(kind)
		}
		secretaryHandler = mcp.Secretary{Workers: ctl.WorkerService{Store: store, PersonID: owner.ID, Capability: capability, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessKind(policy.DefaultHarness), PreferredHarnesses: preferred}}}
	}
	handler := mcp.AuditedHandler{
		Handler: secretaryHandler,
		Store:   store,
		Role:    "secretary",
	}
	if err := (mcp.Server{Handler: handler}).Serve(ctx, os.Stdin, os.Stdout); err != nil {
		if ctx.Err() == nil {
			log.Fatal(fmt.Errorf("serve MCP: %w", err))
		}
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
