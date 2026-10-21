package node

import (
	"context"
	"bytes"
	"encoding/base64"
	"time"
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
		t.Fatalf("Node did not allocate a persistent Node-owned workspace: %q err=%v", mapping.Workspace, err)
	}
}

 
// Simulates an OS purge of the temporary directory between separate Node
// processes. The initial session mapping and its workspace must survive.
func TestProjectlessNativeResumeAfterRestartAndTempPurge(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil { t.Fatal(err) }
	statePath := filepath.Join(stateDir, "node.json")
	volatile := filepath.Join(root, "os-temp")
	if err := os.MkdirAll(volatile, 0o700); err != nil { t.Fatal(err) }
	t.Setenv("TMPDIR", volatile)
	t.Setenv("TMP", volatile)
	t.Setenv("TEMP", volatile)

	firstStore, err := OpenLocalStore(statePath)
	if err != nil { t.Fatal(err) }
	runtime := &t12AttemptBoundRuntime{}
	firstNode := NewExecutionNode("macbook", runtime, firstStore)
	cmd := dispatchFixture("durable-workspace-first")
	out, err := firstNode.HandleCommand(context.Background(), cmd)
	if err != nil || out.State != CommandAccepted { t.Fatalf("initial dispatch: %+v %v", out, err) }
	mapping, ok := firstStore.SessionMapping(cmd.Metadata().AttemptID)
	if !ok { t.Fatal("initial native mapping missing") }
	stableRoot := filepath.Join(stateDir, "workspaces")
	rel, err := filepath.Rel(stableRoot, mapping.Workspace)
	if err != nil || !filepath.IsLocal(rel) { t.Fatalf("workspace %q is outside durable state %q: %v", mapping.Workspace, stableRoot, err) }
	if err := os.WriteFile(filepath.Join(mapping.Workspace, "prior.txt"), []byte("saved"), 0o600); err != nil { t.Fatal(err) }
	if err := firstStore.Close(); err != nil { t.Fatal(err) }
	if err := os.RemoveAll(volatile); err != nil { t.Fatal(err) }

	restoredStore, err := OpenLocalStore(statePath)
	if err != nil { t.Fatal(err) }
	defer restoredStore.Close()
	restarted := NewExecutionNode("macbook", runtime, restoredStore)
	// Activating the same real artifact broker code must be able to pin the
	// original directory before native Resume begins.
	identity := NodeIdentity{Node:"macbook", Credential:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)), ConnectURL:"ws://127.0.0.1/v1/nodes/connect"}
	if err := restarted.SetArtifactPublisher(identity, os.Args[0]); err != nil { t.Fatal(err) }
	defer restarted.CloseArtifactPublisher()
	meta := cmd.Metadata()
	meta.CommandID = "durable-workspace-resume"
	meta.IssuedAt = time.Now().UTC()
	resume := Command{Kind:CommandResume, Resume:&ResumeCommand{Metadata:meta, Envelope:cmd.Dispatch.Envelope}}
	out, err = restarted.HandleCommand(context.Background(), resume)
	if err != nil || out.State != CommandAccepted { t.Fatalf("restart native Resume failed: %+v %v", out, err) }
	if runtime.resumes.Load() != 1 { t.Fatalf("native Resume calls = %d, want 1", runtime.resumes.Load()) }
	if _, err := os.Stat(filepath.Join(mapping.Workspace,"prior.txt")); err != nil { t.Fatalf("original workspace lost after OS temp purge: %v", err) }
}
