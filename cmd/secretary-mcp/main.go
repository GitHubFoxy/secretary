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
	"time"

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
	role := flag.String("role", os.Getenv("SECRETARY_MCP_ROLE"), "managed role: secretary, worker or child_worker")
	workerRef := flag.String("worker-ref", os.Getenv("SECRETARY_MCP_WORKER_REF"), "Worker reference for a worker role")
	flag.Parse()
	if *role == "" {
		*role = "secretary"
	}
	if *role != "secretary" && *role != "worker" && *role != "child_worker" {
		log.Fatalf("unknown MCP role %q", *role)
	}
	capability := os.Getenv("SECRETARY_MCP_CAPABILITY")
	if capability == "" && *role != "child_worker" {
		log.Fatal("SECRETARY_MCP_CAPABILITY is required")
	}
	if *role == "worker" && *workerRef == "" {
		log.Fatal("SECRETARY_MCP_WORKER_REF is required for worker role")
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
	var handler mcp.Handler
	switch *role {
	case "secretary":
		manager, err := config.Open(filepath.Join(*dataDir, "config.toml"))
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		policy := manager.Snapshot().Config.EffectiveWorkerPolicy()
		preferred := make([]core.HarnessKind, len(policy.PreferredHarnesses))
		for i, kind := range policy.PreferredHarnesses {
			preferred[i] = core.HarnessKind(kind)
		}
		handler = mcp.Secretary{Workers: ctl.WorkerService{Store: store, PersonID: owner.ID, Capability: capability, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessKind(policy.DefaultHarness), PreferredHarnesses: preferred}}}
	case "worker":
		handler = mcp.WorkerHandler{
			WorkerRef: *workerRef,
			Authorize: func(ctx context.Context, ref string) error {
				allowed, err := store.AuthorizeWorkerCapability(ctx, ref, capability)
				if err != nil {
					return err
				}
				if !allowed {
					return ctl.ErrUnauthorized
				}
				return nil
			},
			Spawn: func(ctx context.Context, ref, text string) (any, error) {
				attempt, err := store.ActiveAttemptForWorker(ctx, ref)
				if err != nil {
					return nil, err
				}
				child, err := store.CreateChildTask(ctx, ref, attempt.ID, text)
				if err != nil {
					return nil, err
				}
				return waitForChild(ctx, store, child.ID)
			},
		}
	case "child_worker":
		handler = mcp.EmptyHandler{}
	}
	handler = mcp.AuditedHandler{Handler: handler, Store: store, Role: *role, WorkerRef: *workerRef}
	if err := (mcp.Server{Handler: handler}).Serve(ctx, os.Stdin, os.Stdout); err != nil {
		if ctx.Err() == nil {
			log.Fatal(fmt.Errorf("serve MCP: %w", err))
		}
	}
}

func waitForChild(parent context.Context, store *core.Store, taskID string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		details, err := store.TaskDetails(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if len(details.Results) > 0 {
			result := details.Results[len(details.Results)-1]
			return map[string]any{"task_id": taskID, "status": result.Status, "summary": result.Summary}, nil
		}
		if details.Task.State == core.TaskDispatchFailed {
			return nil, fmt.Errorf("child task %s dispatch failed", taskID)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for child %s: %w", taskID, ctx.Err())
		case <-ticker.C:
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
