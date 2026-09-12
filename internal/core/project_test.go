package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func projectFixture(rootA, rootB string) ProjectSpec {
	return ProjectSpec{
		ID:          "frontend",
		Name:        "Frontend",
		Description: "Frontend repository",
		Mappings: []ProjectPathMapping{
			{Node: "macbook", Path: rootA},
			{Node: "home-server", Path: rootB},
		},
		Policy: ProjectPolicy{
			DefaultNode:                   "macbook",
			AllowedHarnessKinds:           []HarnessKind{HarnessFX, HarnessClaudeCode},
			RequiredExecutionCapabilities: []ExecutionCapability{CapabilityShell, CapabilityEdit},
			ModelID:                       "model-a",
			Reasoning:                     "high",
			Execution:                     ExecutionPolicy{RequireApproval: true},
		},
	}
}

func projectInventory(node NodeReference, id HarnessInstanceID, kind HarnessKind) HarnessInventorySnapshot {
	return HarnessInventorySnapshot{Node: node, ObservedAt: time.Now().UTC(), Instances: []HarnessInstance{{
		ID: id, Node: node, Kind: kind, Version: "1.0", Status: HarnessReady,
		Authentication: HarnessAuthentication{Authenticated: true},
		Capabilities:   HarnessCapabilities{Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit}, Activity: []ActivityCapability{ActivityStatus}},
		ModelIDs:       []ObservedModelID{"model-a"}, ReasoningLevels: []ObservedReasoningLevel{"high"},
	}}}
}

func TestProjectCRUDPersistsAcrossReloadAndUsesOptimisticRevision(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "projects.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, projectFixture(filepath.Join(t.TempDir(), "mac"), filepath.Join(t.TempDir(), "home")), "create-key")
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != "frontend" || project.Revision != 1 {
		t.Fatalf("project=%#v", project)
	}
	if duplicate, err := store.CreateProject(ctx, projectFixture("/other/mac", "/other/home"), "create-key"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("different idempotency payload was accepted: %#v err=%v", duplicate, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.Project(ctx, project.ID)
	if err != nil || loaded.Name != "Frontend" || loaded.Revision != 1 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	updated, err := store.UpdateProject(ctx, project.ID, ProjectSpec{Name: "Frontend 2", Description: loaded.Description, Mappings: loaded.Mappings, Policy: loaded.Policy}, loaded.Revision, "update-key")
	if err != nil || updated.Revision != 2 {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if _, err := store.UpdateProject(ctx, project.ID, ProjectSpec{Name: "stale", Description: "stale", Mappings: loaded.Mappings, Policy: loaded.Policy}, loaded.Revision, "stale-key"); !errors.Is(err, ErrProjectRevisionConflict) {
		t.Fatalf("stale update err=%v", err)
	}
	if err := store.DeleteProject(ctx, project.ID, updated.Revision, "delete-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Project(ctx, project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted project err=%v", err)
	}
}

func TestProjectCRUDConcurrentUpdatesHaveSingleWinner(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, projectFixture(filepath.Join(t.TempDir(), "mac"), filepath.Join(t.TempDir(), "home")))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, err := store.UpdateProject(ctx, project.ID, ProjectSpec{Name: name, Mappings: project.Mappings, Policy: project.Policy}, project.Revision)
			errs <- err
		}(name)
	}
	wg.Wait()
	close(errs)
	var conflicts int
	for err := range errs {
		if errors.Is(err, ErrProjectRevisionConflict) {
			conflicts++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts=%d", conflicts)
	}
}

func TestProjectMappingsPolicyAndResolverUseTwoNodesWithoutScanning(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	macRoot, homeRoot := t.TempDir(), t.TempDir()
	project, err := store.CreateProject(ctx, projectFixture(macRoot, homeRoot))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "macbook"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "home-server"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, "macbook", projectInventory("macbook", "macbook/fx", HarnessFX)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, "home-server", projectInventory("home-server", "home-server/claude", HarnessClaudeCode)); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ResolveProjectDispatch(ctx, ProjectDispatchRequest{ProjectID: project.ID, NodeID: "home-server", HarnessInstanceID: "home-server/claude"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != homeRoot || resolved.Node != "home-server" {
		t.Fatalf("resolved=%#v", resolved)
	}
	if _, err := store.ResolveProjectDispatch(ctx, ProjectDispatchRequest{ProjectID: project.ID, NodeID: "home-server", HarnessInstanceID: "home-server/fx"}); !errors.Is(err, ErrProjectPolicyDenied) && !errors.Is(err, ErrHarnessUnavailable) {
		t.Fatalf("forbidden harness err=%v", err)
	}
	if _, err := store.ResolveProjectDispatch(ctx, ProjectDispatchRequest{ProjectID: project.ID, NodeID: "macbook", HarnessInstanceID: "macbook/fx", ModelID: "wrong"}); !errors.Is(err, ErrProjectPolicyDenied) {
		t.Fatalf("pin override err=%v", err)
	}
}

func TestProjectRejectsTraversalAndNodeRejectsMissingWorkspace(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	bad := projectFixture("/tmp/project/../escape", t.TempDir())
	if _, err := store.CreateProject(ctx, bad); !errors.Is(err, ErrProjectValidation) {
		t.Fatalf("traversal err=%v", err)
	}
	project, err := store.CreateProject(ctx, projectFixture(t.TempDir(), filepath.Join(t.TempDir(), "missing")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.ValidateDispatch("macbook", HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: HarnessFX, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}}, filepath.Join(t.TempDir(), "outside")); !errors.Is(err, ErrWorkspaceOutsideRoot) {
		t.Fatalf("outside err=%v", err)
	}
	if project.ID == "" {
		t.Fatal("project id missing")
	}
}

func TestProjectRegistryDoesNotScanDiskOrCreateImplicitProjects(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("unexpected implicit projects=%#v", projects)
	}
	root := filepath.Join(t.TempDir(), "not-created")
	if _, err := store.ResolveProjectDispatch(ctx, ProjectDispatchRequest{ProjectID: "directory-name", NodeID: "macbook", Workspace: root}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("implicit project resolution err=%v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("server touched filesystem: stat err=%v", err)
	}
}

func TestProjectExecutionPolicyAllowDenyAndRequiredSemantics(t *testing.T) {
	instance := HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: HarnessFX, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}, Capabilities: HarnessCapabilities{Execution: []ExecutionCapability{CapabilityShell}}}
	base := Project{ID: "p", Name: "P", Mappings: []ProjectPathMapping{{Node: "macbook", Path: t.TempDir()}}}
	base.Policy.Execution.AllowedCapabilities = []ExecutionCapability{CapabilityShell}
	base.Policy.RequiredCapabilities = []ExecutionCapability{CapabilityShell}
	if _, err := base.ValidateDispatch("macbook", instance, ""); err != nil {
		t.Fatalf("allowlist should permit required shell: %v", err)
	}
	base.Policy.RequiredCapabilities = []ExecutionCapability{CapabilityEdit}
	if _, err := base.ValidateDispatch("macbook", instance, ""); !errors.Is(err, ErrProjectPolicyDenied) {
		t.Fatalf("required capability outside allowlist err=%v", err)
	}
	base.Policy.RequiredCapabilities = nil
	base.Policy.Execution.AllowedCapabilities = nil
	base.Policy.Execution.DeniedCapabilities = []ExecutionCapability{CapabilityShell}
	if _, err := base.ValidateDispatch("macbook", instance, ""); !errors.Is(err, ErrProjectPolicyDenied) {
		t.Fatalf("denied capability err=%v", err)
	}
}

func TestProjectRejectsSlashIDsUnknownCapabilitiesAndConflictingAliases(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	base := projectFixture(t.TempDir(), t.TempDir())
	base.ID = "bad/project"
	if _, err := store.CreateProject(ctx, base); !errors.Is(err, ErrProjectValidation) {
		t.Fatalf("slash id err=%v", err)
	}
	base.ID = "frontend"
	base.Policy.Execution.AllowedCapabilities = []ExecutionCapability{"future_capability"}
	if _, err := store.CreateProject(ctx, base); !errors.Is(err, ErrProjectValidation) {
		t.Fatalf("unknown capability err=%v", err)
	}
	base.Policy.Execution.AllowedCapabilities = nil
	base.Policy.ModelID, base.Policy.Model = "model-a", "model-b"
	if _, err := store.CreateProject(ctx, base); !errors.Is(err, ErrProjectValidation) {
		t.Fatalf("conflicting model aliases err=%v", err)
	}
}

func TestCreateWorkerFromDispatchUsesCanonicalProjectSnapshotAndBinding(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	root := t.TempDir()
	project, err := store.CreateProject(ctx, projectFixture(root, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	conversation := mustConversation(t, store)
	instance := HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: HarnessFX, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}, Capabilities: HarnessCapabilities{Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit}}, ModelIDs: []ObservedModelID{"model-a"}, ReasoningLevels: []ObservedReasoningLevel{"high"}}
	if _, err := store.EnrollNode(ctx, "macbook"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, "macbook", projectInventory("macbook", instance.ID, HarnessFX)); err != nil {
		t.Fatal(err)
	}
	forged := project.Snapshot("macbook", root, instance)
	forged.Name = "forged"
	forged.Policy.ModelID = "other"
	worker, _, _, err := store.CreateWorkerFromDispatch(ctx, conversation.ID, "intent", ProjectDispatch{Project: project, Node: "macbook", Workspace: root, HarnessInstance: instance, Snapshot: forged}, "canonical-worker")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot ProjectSnapshot
	if err := json.Unmarshal([]byte(worker.ProjectSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Name != project.Name || snapshot.Policy.ModelPin() != project.Policy.ModelPin() {
		t.Fatalf("forged snapshot persisted: %#v", snapshot)
	}
	stale := project
	stale.Revision = 0
	if _, _, _, err := store.CreateWorkerFromDispatch(ctx, conversation.ID, "stale", ProjectDispatch{Project: stale, Node: "macbook", Workspace: root, HarnessInstance: instance}, "stale-worker"); !errors.Is(err, ErrProjectRevisionConflict) {
		t.Fatalf("revision zero err=%v", err)
	}
	wrongNode := instance
	wrongNode.Node = "home-server"
	if _, _, _, err := store.CreateWorkerFromDispatch(ctx, conversation.ID, "wrong", ProjectDispatch{Project: project, Node: "macbook", Workspace: root, HarnessInstance: wrongNode}, "wrong-node"); !errors.Is(err, ErrProjectPolicyDenied) {
		t.Fatalf("mismatched HarnessInstance Node err=%v", err)
	}
}

func TestWorkerSnapshotSurvivesStoreRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	project, err := store.CreateProject(ctx, projectFixture(root, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	conversation := mustConversation(t, store)
	instance := HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: HarnessFX, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}, Capabilities: HarnessCapabilities{Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit}}, ModelIDs: []ObservedModelID{"model-a"}, ReasoningLevels: []ObservedReasoningLevel{"high"}}
	worker, _, _, err := store.CreateWorkerFromDispatch(ctx, conversation.ID, "restart", ProjectDispatch{Project: project, Node: "macbook", Workspace: root, HarnessInstance: instance}, "restart-worker")
	if err != nil {
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
	loaded, err := store.Worker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProjectSnapshot == "" || loaded.PolicySnapshot == "" || loaded.Workspace != root {
		t.Fatalf("snapshot did not survive restart: %#v", loaded)
	}
}

func mustConversation(t *testing.T, store *Store) Conversation {
	t.Helper()
	_, conversation, err := store.CreatePersonWithConversation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return conversation
}

func TestWorkerKeepsProjectSnapshotAfterRegistryChange(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	root := t.TempDir()
	project, err := store.CreateProject(ctx, projectFixture(root, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved := ProjectDispatch{Project: project, Node: "macbook", Workspace: root, HarnessInstance: HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: HarnessFX, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}, Capabilities: HarnessCapabilities{Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit}}, ModelIDs: []ObservedModelID{"model-a"}, ReasoningLevels: []ObservedReasoningLevel{"high"}}}
	worker, _, _, err := store.CreateWorkerFromDispatch(ctx, conversation.ID, "intent", resolved, "worker-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateProject(ctx, project.ID, ProjectSpec{Name: "changed", Mappings: project.Mappings, Policy: project.Policy}, project.Revision); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.Worker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ProjectSnapshot == "" || reloaded.Workspace != root || reloaded.PolicySnapshot == "" {
		t.Fatalf("worker=%#v", reloaded)
	}
	if reloaded.ProjectID != project.ID {
		t.Fatal("binding changed")
	}
}
