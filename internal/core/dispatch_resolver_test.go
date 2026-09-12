package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchResolverUsesWorkerDefaultHarnessRatherThanSecretaryHarness(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	root := t.TempDir()
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "frontend", Name: "Frontend", Mappings: []ProjectPathMapping{{Node: "macbook", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	inventory := resolverInventory("macbook", []HarnessInstance{
		resolverInstance("macbook/claude", "macbook", HarnessClaudeCode),
		resolverInstance("macbook/fx", "macbook", HarnessFX),
	})
	resolverEnroll(t, store, "macbook", inventory, 1, nil)

	resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{
		ProjectID:    project.ID,
		WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessFX, PreferredHarnesses: []HarnessKind{HarnessClaudeCode}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.HarnessInstance.ID != "macbook/fx" {
		t.Fatalf("selected %q, want worker default fx", resolved.HarnessInstance.ID)
	}
	if resolved.Workspace != filepath.Clean(root) {
		t.Fatalf("workspace %q, want %q", resolved.Workspace, root)
	}
}

func TestDispatchResolverHonorsExplicitClaudeInstanceAndObservedPins(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "api", Name: "API", Mappings: []ProjectPathMapping{{Node: "mac", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	claude := resolverInstance("mac/claude", "mac", HarnessClaudeCode)
	claude.ModelIDs = []ObservedModelID{"claude-3-7"}
	claude.ReasoningLevels = []ObservedReasoningLevel{"high"}
	resolverEnroll(t, store, "mac", resolverInventory("mac", []HarnessInstance{claude, resolverInstance("mac/fx", "mac", HarnessFX)}), 2, nil)

	resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, HarnessInstanceID: claude.ID, HarnessKind: HarnessClaudeCode, ModelID: "claude-3-7", Reasoning: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.HarnessInstance.ID != claude.ID || resolved.Snapshot.Policy.ModelPin() != "claude-3-7" || resolved.Snapshot.Policy.Reasoning != "high" {
		t.Fatalf("resolution=%#v", resolved)
	}
	conversation := mustConversation(t, store)
	worker, _, _, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "use claude", DispatchResolutionRequest{ProjectID: project.ID, HarnessInstanceID: claude.ID, ModelID: "claude-3-7", Reasoning: "high"}, "claude-pins")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil || bound.Snapshot.Policy.ModelPin() != "claude-3-7" || bound.Snapshot.Policy.Reasoning != "high" {
		t.Fatalf("bound=%#v err=%v", bound, err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, HarnessInstanceID: claude.ID, ModelID: "missing"}); !errors.Is(err, ErrInvalidDispatchPin) || !errors.Is(err, ErrObservedPinUnavailable) {
		t.Fatalf("missing model error=%v", err)
	}
}

func TestDispatchResolverUsesHealthyCapacityAndProjectPolicy(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "restricted", Name: "Restricted", Mappings: []ProjectPathMapping{{Node: "a", Path: t.TempDir()}, {Node: "b", Path: t.TempDir()}}, Policy: ProjectPolicy{AllowedHarnessKinds: []HarnessKind{HarnessClaudeCode}, RequiredExecutionCapabilities: []ExecutionCapability{CapabilityShell}}})
	if err != nil {
		t.Fatal(err)
	}
	fx := resolverInstance("a/fx", "a", HarnessFX)
	claudeA := resolverInstance("a/claude", "a", HarnessClaudeCode)
	claudeB := resolverInstance("b/claude", "b", HarnessClaudeCode)
	resolverEnroll(t, store, "a", resolverInventory("a", []HarnessInstance{fx, claudeA}), 1, []NodeActiveAttempt{{AttemptID: "busy"}})
	resolverEnroll(t, store, "b", resolverInventory("b", []HarnessInstance{claudeB}), 1, nil)

	resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessFX, PreferredHarnesses: []HarnessKind{HarnessClaudeCode}}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Node != "b" || resolved.HarnessInstance.ID != "b/claude" {
		t.Fatalf("resolution=%#v", resolved)
	}
}

func TestDispatchResolverFindsExplicitHarnessInstanceOnItsObservedNode(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "alpha", Path: t.TempDir()}, {Node: "bravo", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	resolverEnroll(t, store, "alpha", resolverInventory("alpha", []HarnessInstance{resolverInstance("alpha/fx", "alpha", HarnessFX)}), 1, nil)
	resolverEnroll(t, store, "bravo", resolverInventory("bravo", []HarnessInstance{resolverInstance("bravo/claude", "bravo", HarnessClaudeCode)}), 1, nil)
	resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, HarnessInstanceID: "bravo/claude"})
	if err != nil || resolved.Node != "bravo" {
		t.Fatalf("resolution=%#v err=%v", resolved, err)
	}
}

func TestDispatchResolverRanksEquivalentNodesDeterministically(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "zeta", Path: t.TempDir()}, {Node: "alpha", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	resolverEnroll(t, store, "zeta", resolverInventory("zeta", []HarnessInstance{resolverInstance("zeta/fx", "zeta", HarnessFX)}), 1, nil)
	resolverEnroll(t, store, "alpha", resolverInventory("alpha", []HarnessInstance{resolverInstance("alpha/fx", "alpha", HarnessFX)}), 1, nil)
	for range 5 {
		resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessFX}})
		if err != nil || resolved.Node != "alpha" || resolved.HarnessInstance.ID != "alpha/fx" {
			t.Fatalf("resolution=%#v err=%v", resolved, err)
		}
	}
}

func TestDispatchResolverQueuesExplicitUnavailableNodeAndKeepsImmutableBinding(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resolver.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "offline", Path: t.TempDir()}, {Node: "other", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	offline := resolverInstance("offline/fx", "offline", HarnessFX)
	resolverEnroll(t, store, "offline", resolverInventory("offline", []HarnessInstance{offline}), 1, nil)
	if err := store.MarkNodeDisconnected(ctx, "offline"); err != nil {
		t.Fatal(err)
	}
	resolverEnroll(t, store, "other", resolverInventory("other", []HarnessInstance{resolverInstance("other/fx", "other", HarnessFX)}), 1, nil)
	conversation := mustConversation(t, store)
	worker, _, _, resolved, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "fix it", DispatchResolutionRequest{ProjectID: project.ID, NodeID: "offline", HarnessInstanceID: offline.ID}, "same-request")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Queued || worker.Status != WorkerQueued || worker.NodeID != "offline" || worker.HarnessInstanceID != "offline/fx" {
		t.Fatalf("queued immutable binding=%#v resolved=%#v", worker, resolved)
	}
	repeated, _, _, repeatedResolution, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "fix it", DispatchResolutionRequest{ProjectID: project.ID, NodeID: "offline", HarnessInstanceID: offline.ID}, "same-request")
	if err != nil || repeated.ID != worker.ID || repeated.NodeID != worker.NodeID || repeatedResolution.HarnessInstance.ID != offline.ID {
		t.Fatalf("repeat=%#v resolution=%#v err=%v", repeated, repeatedResolution, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bound, err := store.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil || bound.Node != "offline" || bound.HarnessInstance.ID != "offline/fx" {
		t.Fatalf("reloaded binding=%#v err=%v", bound, err)
	}
	alternate, _, _, alternateResolved, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "fix it", DispatchResolutionRequest{ProjectID: project.ID, NodeID: "other", HarnessInstanceID: "other/fx"}, "other-request")
	if err != nil || alternate.ID == worker.ID || alternateResolved.Node != "other" {
		t.Fatalf("alternate worker=%#v resolved=%#v err=%v", alternate, alternateResolved, err)
	}
}

func TestDispatchResolverRejectsNoProjectMissingInventoryAndRevokedNodes(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{}); !errors.Is(err, ErrNoProject) {
		t.Fatalf("no project error=%v", err)
	}
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, NodeID: "node"}); !errors.Is(err, ErrMissingHarnessInventory) {
		t.Fatalf("missing inventory error=%v", err)
	}
	resolverEnroll(t, store, "node", resolverInventory("node", []HarnessInstance{resolverInstance("node/fx", "node", HarnessFX)}), 1, nil)
	if _, err := store.SetNodeDraining(ctx, "node", true); err != nil {
		t.Fatal(err)
	}
	conversation := mustConversation(t, store)
	worker, _, _, resolved, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "wait for node", DispatchResolutionRequest{ProjectID: project.ID, NodeID: "node"}, "draining-node")
	if err != nil || !resolved.Queued || worker.Status != WorkerQueued || worker.NodeID != "node" {
		t.Fatalf("draining binding worker=%#v resolution=%#v err=%v", worker, resolved, err)
	}
	if _, err := store.RevokeNode(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, NodeID: "node"}); !errors.Is(err, ErrNodeRevoked) {
		t.Fatalf("revoked error=%v", err)
	}
}

func TestDispatchResolverTriesEveryNodeForExplicitModelPin(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "alpha", Path: t.TempDir()}, {Node: "bravo", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	alpha := resolverInstance("alpha/claude", "alpha", HarnessClaudeCode)
	alpha.ModelIDs = []ObservedModelID{"claude-old"}
	bravo := resolverInstance("bravo/claude", "bravo", HarnessClaudeCode)
	bravo.ModelIDs = []ObservedModelID{"claude-new"}
	resolverEnroll(t, store, "alpha", resolverInventory("alpha", []HarnessInstance{alpha, resolverInstance("alpha/fx", "alpha", HarnessFX)}), 1, nil)
	resolverEnroll(t, store, "bravo", resolverInventory("bravo", []HarnessInstance{bravo, resolverInstance("bravo/fx", "bravo", HarnessFX)}), 1, nil)

	resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, ModelID: "claude-new"})
	if err != nil || resolved.Node != "bravo" || resolved.HarnessInstance.ID != bravo.ID {
		t.Fatalf("resolution=%#v err=%v", resolved, err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, ModelID: "missing"}); !errors.Is(err, ErrInvalidDispatchPin) {
		t.Fatalf("missing model error=%v", err)
	}
}

func TestDispatchResolverDefaultsEmptyWorkerPolicyToFX(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	resolverEnroll(t, store, "node", resolverInventory("node", []HarnessInstance{resolverInstance("node/claude", "node", HarnessClaudeCode), resolverInstance("node/fx", "node", HarnessFX)}), 1, nil)
	resolved, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID})
	if err != nil || resolved.HarnessInstance.Kind != HarnessFX {
		t.Fatalf("resolution=%#v err=%v", resolved, err)
	}
}

func TestResolveAndCreateWorkerReplaysBeforeCanonicalReads(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resolver.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	inventory := resolverInventory("node", []HarnessInstance{resolverInstance("node/fx", "node", HarnessFX)})
	resolverEnroll(t, store, "node", inventory, 1, nil)
	conversation := mustConversation(t, store)
	worker, turn, attempt, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "durable", DispatchResolutionRequest{ProjectID: project.ID}, "durable-key")
	if err != nil {
		t.Fatal(err)
	}
	mutated := resolverInventory("node", []HarnessInstance{resolverInstance("node/claude", "node", HarnessClaudeCode)})
	if err := store.UpdateNodeInventory(ctx, "node", mutated); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProject(ctx, project.ID, project.Revision); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	replayedWorker, replayedTurn, replayedAttempt, resolved, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "changed request is ignored", DispatchResolutionRequest{ProjectID: "deleted", ModelID: "missing"}, "durable-key")
	if err != nil || replayedWorker.ID != worker.ID || replayedTurn.ID != turn.ID || replayedAttempt.ID != attempt.ID || resolved.HarnessInstance.ID != "node/fx" {
		t.Fatalf("replay worker=%#v turn=%#v attempt=%#v resolution=%#v err=%v", replayedWorker, replayedTurn, replayedAttempt, resolved, err)
	}
}

func TestResolveAndCreateWorkerRejectsNodeMutationAfterResolution(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resolver.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	other, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	resolverEnroll(t, store, "node", resolverInventory("node", []HarnessInstance{resolverInstance("node/fx", "node", HarnessFX)}), 1, nil)
	conversation := mustConversation(t, store)
	store.beforeResolvedWorkerCreate = func() {
		if _, err := other.RevokeNode(ctx, "node"); err != nil {
			t.Fatalf("revoke during resolution: %v", err)
		}
	}
	defer func() { store.beforeResolvedWorkerCreate = nil }()
	if _, _, _, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "race", DispatchResolutionRequest{ProjectID: project.ID}, "race-key"); !errors.Is(err, ErrSelectedNodeUnavailable) {
		t.Fatalf("creation error=%v", err)
	}
	var workers int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers`).Scan(&workers); err != nil || workers != 0 {
		t.Fatalf("workers=%d err=%v", workers, err)
	}
}

func TestResolveAndCreateWorkerRejectsInventoryMutationAfterResolution(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "resolver.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	other, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "repo", Name: "Repo", Mappings: []ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	resolverEnroll(t, store, "node", resolverInventory("node", []HarnessInstance{resolverInstance("node/fx", "node", HarnessFX)}), 1, nil)
	conversation := mustConversation(t, store)
	store.beforeResolvedWorkerCreate = func() {
		inventory := resolverInventory("node", []HarnessInstance{resolverInstance("node/claude", "node", HarnessClaudeCode)})
		if err := other.UpdateNodeInventory(ctx, "node", inventory); err != nil {
			t.Fatalf("mutate inventory during resolution: %v", err)
		}
	}
	defer func() { store.beforeResolvedWorkerCreate = nil }()
	if _, _, _, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "race", DispatchResolutionRequest{ProjectID: project.ID}, "inventory-race-key"); !errors.Is(err, ErrMissingHarnessInventory) {
		t.Fatalf("creation error=%v", err)
	}
	var workers int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers`).Scan(&workers); err != nil || workers != 0 {
		t.Fatalf("workers=%d err=%v", workers, err)
	}
}

func resolverInstance(id HarnessInstanceID, node NodeReference, kind HarnessKind) HarnessInstance {
	return HarnessInstance{ID: id, Node: node, Kind: kind, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}, Capabilities: HarnessCapabilities{Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit}}}
}

func resolverInventory(node NodeReference, instances []HarnessInstance) HarnessInventorySnapshot {
	return HarnessInventorySnapshot{Node: node, ObservedAt: time.Now().UTC(), Instances: instances}
}

func resolverEnroll(t *testing.T, store *Store, node NodeReference, inventory HarnessInventorySnapshot, capacity int, active []NodeActiveAttempt) {
	t.Helper()
	if _, err := store.EnrollNode(context.Background(), node); err != nil && !errors.Is(err, ErrNodeAlreadyEnrolled) {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(context.Background(), node, inventory, NodeHeartbeat{Capacity: capacity, ActiveAttempts: active}); err != nil {
		t.Fatal(err)
	}
}
