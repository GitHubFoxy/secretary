package core

import (
	"context"
	"errors"
	"testing"
)

func TestProjectlessDispatchChoosesReadyHarnessAndHonorsOwnerOverride(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	resolverEnroll(t, store, "remote", resolverInventory("remote", []HarnessInstance{
		resolverInstance("remote/claude", "remote", HarnessClaudeCode),
	}), 2, nil)
	resolverEnroll(t, store, "local", resolverInventory("local", []HarnessInstance{
		resolverInstance("local/codex", "local", HarnessCodex),
		resolverInstance("local/claude", "local", HarnessClaudeCode),
	}), 2, nil)

	request := DispatchResolutionRequest{WorkerPolicy: HarnessPolicy{
		DefaultHarness: HarnessCodex,
		PreferredHarnesses: []HarnessKind{HarnessClaudeCode},
		RequiredExecutionCapabilities: []ExecutionCapability{CapabilityShell},
	}}
	selected, err := store.ResolveDispatch(ctx, request)
	if err != nil || selected.Node != "local" || selected.HarnessInstance.ID != "local/codex" || selected.Workspace != "" || selected.Project.ID != "" {
		t.Fatalf("automatic selection: %#v err=%v", selected, err)
	}
	conversation := mustConversation(t, store)
	worker, _, _, resolved, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "build a site", request, "projectless-default")
	if err != nil || resolved.Queued || worker.ProjectID != "" {
		t.Fatalf("projectless Worker: %#v selection=%#v err=%v", worker, resolved, err)
	}
	binding, err := store.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil || binding.Node != "local" || binding.HarnessInstance.ID != "local/codex" || binding.Workspace != "" {
		t.Fatalf("immutable Worker binding: %#v err=%v", binding, err)
	}

	override := request
	override.NodeID = "remote"
	override.HarnessKind = HarnessClaudeCode
	override.HarnessInstanceID = "remote/claude"
	other, _, _, selection, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "review the site", override, "projectless-override")
	if err != nil || other.NodeID != "remote" || other.HarnessInstanceID != "remote/claude" || selection.Node != "remote" || selection.Project.ID != "" {
		t.Fatalf("owner override: worker=%#v selection=%#v err=%v", other, selection, err)
	}
	original, err := store.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil || original.Node != binding.Node || original.HarnessInstance.ID != binding.HarnessInstance.ID {
		t.Fatalf("override migrated existing Worker: %#v err=%v", original, err)
	}
	repeated, _, _, replay, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "ignored duplicate", override, "projectless-default")
	if err != nil || repeated.ID != worker.ID || replay.Node != "local" || replay.HarnessInstance.ID != "local/codex" {
		t.Fatalf("replay changed immutable selection: %#v %#v err=%v", repeated, replay, err)
	}
}

func TestProjectlessDispatchExplainsIncompatibleAndOfflineOverrides(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	installed := resolverInstance("node/claude", "node", HarnessClaudeCode)
	resolverEnroll(t, store, "node", resolverInventory("node", []HarnessInstance{installed}), 2, nil)

	auto, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessCodex}})
	if err != nil || auto.HarnessInstance.ID != installed.ID {
		t.Fatalf("fallback to installed supported harness: %#v err=%v", auto, err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{NodeID: "node", HarnessKind: HarnessCodex}); !errors.Is(err, ErrInvalidDispatchPin) {
		t.Fatalf("incompatible harness override: %v", err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{NodeID: "node", HarnessInstanceID: "node/missing"}); !errors.Is(err, ErrInvalidDispatchPin) {
		t.Fatalf("uninstalled harness override: %v", err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{NodeID: "unknown"}); !errors.Is(err, ErrSelectedNodeUnavailable) {
		t.Fatalf("unenrolled Node override: %v", err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{WorkerPolicy: HarnessPolicy{
		DefaultHarness: HarnessClaudeCode,
		RequiredActivityCapabilities: []ActivityCapability{ActivityCapability("nonexistent")},
	}}); !errors.Is(err, ErrInvalidDispatchPin) {
		t.Fatalf("incompatible capability: %v", err)
	}
	if err := store.MarkNodeDisconnected(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{NodeID: "node", HarnessKind: HarnessClaudeCode}); !errors.Is(err, ErrSelectedNodeUnavailable) {
		t.Fatalf("offline Node override: %v", err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{}); !errors.Is(err, ErrNoEligibleNode) {
		t.Fatalf("offline automatic selection: %v", err)
	}
}

func TestProjectlessDispatchPrefersConfiguredHarnessAcrossNodes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	resolverEnroll(t, store, "local", resolverInventory("local", []HarnessInstance{
		resolverInstance("local/claude", "local", HarnessClaudeCode),
	}), 1, nil)
	resolverEnroll(t, store, "remote", resolverInventory("remote", []HarnessInstance{
		resolverInstance("remote/codex", "remote", HarnessCodex),
	}), 1, nil)
	selected, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{
		WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessCodex, PreferredHarnesses: []HarnessKind{HarnessClaudeCode}},
	})
	if err != nil || selected.Node != "remote" || selected.HarnessInstance.ID != "remote/codex" {
		t.Fatalf("configured harness preference lost to Node preference: %#v err=%v", selected, err)
	}
}


func TestProjectlessExplicitBusyNodeFailsVisibly(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	resolverEnroll(t, store, "busy", resolverInventory("busy", []HarnessInstance{
		resolverInstance("busy/codex", "busy", HarnessCodex),
	}), 1, []NodeActiveAttempt{{AttemptID: "other-attempt"}})
	_, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{NodeID: "busy", HarnessKind: HarnessCodex})
	if !errors.Is(err, ErrSelectedNodeUnavailable) {
		t.Fatalf("busy selected Node must not silently queue or migrate: %v", err)
	}
}
