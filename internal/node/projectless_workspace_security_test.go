package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectlessWorkspaceOverrideRejectedByNodeBeforeRuntimeStart(t *testing.T) {
	ctx := context.Background()
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime := &profileGateRuntime{}
	execution := NewExecutionNode("macbook", runtime, store)
	base := dispatchFixture("valid-projectless-dispatch")
	if base.Dispatch.Envelope.Workspace != "" {
		t.Fatal("projectless fixture must let Node choose workspace")
	}
	outsideRoot := t.TempDir()
	link := filepath.Join(t.TempDir(), "escaped-workspace")
	if err := os.Symlink(outsideRoot, link); err != nil {
		t.Fatal(err)
	}
	for _, workspace := range []string{outsideRoot, link, "../outside", "/", " ", tWorkspace} {
		command := dispatchFixture("invalid-projectless-" + workspace)
		command.Dispatch.Envelope.Workspace = workspace
		if err := command.Validate("macbook"); err == nil {
			t.Fatalf("projectless Workspace %q passed protocol validation", workspace)
		}
		if outcome, err := execution.HandleCommand(ctx, command); err == nil || outcome.State == CommandAccepted {
			t.Fatalf("untrusted workspace %q reached Execution Node: outcome=%#v err=%v", workspace, outcome, err)
		}
		direct := execution.dispatch(ctx, command.Dispatch)
		if direct.State != CommandFailed || direct.ErrorCode != "workspace_forbidden" {
			t.Fatalf("untrusted workspace %q bypassed dispatch guard: %#v", workspace, direct)
		}
	}
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{
		Metadata: base.Dispatch.Metadata, Envelope: base.Dispatch.Envelope,
	}}
	resume.Resume.Envelope.Workspace = outsideRoot
	if err := resume.Validate("macbook"); err == nil {
		t.Fatal("projectless Resume accepted untrusted workspace")
	}
	if runtime.starts != 0 {
		t.Fatalf("untrusted Workspace started native runtime %d times", runtime.starts)
	}
	outcome, err := execution.HandleCommand(ctx, base)
	if err != nil || outcome.State != CommandAccepted || runtime.starts != 1 {
		t.Fatalf("Node-owned workspace dispatch failed: outcome=%#v err=%v starts=%d", outcome, err, runtime.starts)
	}
	mapping, found := store.SessionMapping(base.Dispatch.Metadata.AttemptID)
	if !found || mapping.Workspace == "" || mapping.Workspace == outsideRoot {
		t.Fatalf("native session did not receive Node-owned workspace: %#v found=%v", mapping, found)
	}
	t.Cleanup(func() { _ = os.RemoveAll(mapping.Workspace) })
	info, err := os.Stat(mapping.Workspace)
	if err != nil || !info.IsDir() {
		t.Fatalf("Node did not allocate a real isolated workspace: %q err=%v", mapping.Workspace, err)
	}
}
