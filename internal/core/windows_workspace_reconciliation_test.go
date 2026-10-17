package core

import (
    "context"
    "errors"
    "testing"
)

func TestWindowsWorkspaceMappingReconcilesAndDispatchesOnUnixServer(t *testing.T) {
    ctx := context.Background()
    store := newTestStore(t)
    const root = "C:\\src\\project"
    const inner = "C:\\src\\project\\subdir"
    project, err := store.CreateProject(ctx, ProjectSpec{
        ID: "windows", Name: "Windows workspace",
        Mappings: []ProjectPathMapping{{Node: "windows-node", Path: root}},
        Policy: ProjectPolicy{Execution: ExecutionPolicy{WorkspaceRoot: root}},
    })
    if err != nil {
        t.Fatalf("Server rejected a Windows Project path: %v", err)
    }
    if _, err := store.EnrollNode(ctx, "windows-node"); err != nil {
        t.Fatal(err)
    }
    inventory := projectInventory("windows-node", "windows-node/codex", HarnessCodex)
    if err := store.MarkNodeConnected(ctx, "windows-node", inventory); err != nil {
        t.Fatal(err)
    }
    if err := store.ReconcileNodeWorkspaces(ctx, "windows-node", []NodeWorkspaceMapping{{ProjectID: project.ID, Path: root}}); err != nil {
        t.Fatalf("Server did not reconcile Windows mapping: %v", err)
    }
    binding, err := store.ResolveProjectDispatch(ctx, ProjectDispatchRequest{
        ProjectID: project.ID, NodeID: "windows-node", HarnessInstanceID: "windows-node/codex", Workspace: inner,
    })
    if err != nil || binding.Workspace != inner || binding.Snapshot.Workspace != inner {
        t.Fatalf("Windows dispatch binding=%#v err=%v", binding, err)
    }
    _, conversation, err := store.CreatePersonWithConversation(ctx)
    if err != nil {
        t.Fatal(err)
    }
    worker, _, _, err := store.CreateWorkerFromDispatch(ctx, conversation.ID, "run on Windows", binding, "windows-dispatch")
    if err != nil || worker.Workspace != inner || worker.NodeID != "windows-node" {
        t.Fatalf("Windows dispatch could not create Worker: worker=%#v err=%v", worker, err)
    }
    record, err := store.NodeRecord(ctx, "windows-node")
    if err != nil || len(record.Workspaces) != 1 || record.Workspaces[0].Path != root {
        t.Fatalf("Server changed the native Node path: %#v err=%v", record.Workspaces, err)
    }
    for _, bad := range []string{"C:\\src\\projected", "D:\\src\\project\\subdir", "/tmp/project/subdir"} {
        if _, err := store.ResolveProjectDispatch(ctx, ProjectDispatchRequest{
            ProjectID: project.ID, NodeID: "windows-node", HarnessInstanceID: "windows-node/codex", Workspace: bad,
        }); !errors.Is(err, ErrWorkspaceOutsideRoot) {
            t.Fatalf("Server authorized escaped Windows workspace %q: %v", bad, err)
        }
    }
    if err := store.ReconcileNodeWorkspaces(ctx, "windows-node", []NodeWorkspaceMapping{
        {ProjectID: project.ID, Path: "C:\\src\\project\\..\\private"},
    }); !errors.Is(err, ErrNodeWorkspaceMismatch) {
        t.Fatalf("Server accepted Windows traversal from Node: %v", err)
    }
    if err := store.ReconcileNodeWorkspaces(ctx, "windows-node", []NodeWorkspaceMapping{
        {ProjectID: project.ID, Path: "C:\\src\\other"},
    }); !errors.Is(err, ErrNodeWorkspaceMismatch) {
        t.Fatalf("Server accepted a modified Node path: %v", err)
    }
}

func TestCrossPlatformWorkspacePathValidation(t *testing.T) {
    for _, good := range []string{
        "/home/worker/project", "C:\\src\\project", "c:\\Users\\A\\src", "\\\\server\\share\\repo",
    } {
        if err := ValidateWorkspaceRootPath(good); err != nil {
            t.Fatalf("valid absolute workspace %q rejected: %v", good, err)
        }
    }
    for _, bad := range []string{
        "relative", "../relative", "/tmp/../etc", "/tmp//repo", "/",
        "C:relative", "C:\\", "C:\\src\\..\\private", "C:\\src\\\\repo",
        "C:/src/project", "\\\\server\\share",
    } {
        if err := ValidateWorkspaceRootPath(bad); err == nil {
            t.Fatalf("invalid/unclean workspace %q accepted", bad)
        }
    }
    if !pathContained("C:\\src\\project", "c:\\SRC\\PROJECT\\child") {
        t.Fatal("drive path containment failed")
    }
}
