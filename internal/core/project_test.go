package core

import (
	"context"
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
	if duplicate, err := store.CreateProject(ctx, projectFixture("/other/mac", "/other/home"), "create-key"); err != nil || duplicate.ID != project.ID {
		t.Fatalf("idempotent create=%#v err=%v", duplicate, err)
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
