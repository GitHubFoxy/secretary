package node

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type projectRuntime struct{}

func (projectRuntime) Start(context.Context, StartRequest) (Session, error) {
	return &projectSession{}, nil
}

type projectSession struct{}

func (*projectSession) ID() string                                  { return "native" }
func (*projectSession) Prompt(context.Context, string) error        { return nil }
func (*projectSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (*projectSession) Cancel(context.Context) error                { return nil }
func (*projectSession) Activity() <-chan Activity                   { c := make(chan Activity); close(c); return c }
func (*projectSession) Result() <-chan Result                       { c := make(chan Result); close(c); return c }
func (*projectSession) Close() error                                { return nil }

func TestExecutionNodeRejectsProjectTraversalAndMissingWorkspaceBeforeRuntime(t *testing.T) {
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("macbook", projectRuntime{}, store)
	instance := core.HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	project := core.Project{ID: "p", Name: "P", Mappings: []core.ProjectPathMapping{{Node: "macbook", Path: filepath.Join(t.TempDir(), "missing")}}, Policy: core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessFX}}}
	_, err = project.ValidateDispatch("macbook", instance, project.Mappings[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	command := Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{Metadata: core.CommandMetadata{CommandID: "missing", Node: "macbook", HarnessInstanceID: instance.ID, WorkerRef: "w", TurnID: "t", AttemptID: "a", IssuedAt: time.Now().UTC()}, Envelope: WorkerEnvelope{WorkerRef: "w", TurnID: "t", AttemptID: "a", OriginalUserIntent: "run", ProjectID: project.ID, ProjectSnapshot: project.Snapshot("macbook", project.Mappings[0].Path, instance), Workspace: project.Mappings[0].Path, HarnessInstance: instance}}}
	outcome, err := execution.HandleCommand(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State != CommandFailed || outcome.ErrorCode != "workspace_missing" {
		t.Fatalf("outcome=%#v", outcome)
	}
	if _, err := execution.HandleCommand(context.Background(), command); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionNodeRejectsWorkspaceOutsideProjectRoot(t *testing.T) {
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	instance := core.HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	root := t.TempDir()
	project := core.Project{ID: "p", Name: "P", Mappings: []core.ProjectPathMapping{{Node: "macbook", Path: root}}, Policy: core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessFX}}}
	workspace := filepath.Join(t.TempDir(), "outside")
	_, err = project.ValidateDispatch("macbook", instance, workspace)
	if !errors.Is(err, core.ErrWorkspaceOutsideRoot) {
		t.Fatalf("validation err=%v", err)
	}
}
