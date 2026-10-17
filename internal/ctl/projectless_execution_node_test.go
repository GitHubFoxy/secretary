package ctl

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type projectlessExecutionRuntime struct {
	lifecycleRuntime
	executor *node.ExecutionNode
	envelope node.WorkerEnvelope
	calls    int
}

func (r *projectlessExecutionRuntime) Dispatch(ctx context.Context, commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, resolved core.DispatchResolution) error {
	envelope, err := (NodeRuntime{}).envelope(worker, turn, attempt, resolved.ProjectDispatch)
	if err != nil {
		return err
	}
	metadata := core.CommandMetadata{
		CommandID: commandID, Node: core.NodeReference(worker.NodeID),
		HarnessInstanceID: envelope.HarnessInstance.ID, WorkerRef: worker.WorkerRef,
		TurnID: turn.ID, AttemptID: attempt.ID, CorrelationID: attempt.CorrelationID, IssuedAt: time.Now().UTC(),
	}
	outcome, err := r.executor.HandleCommand(ctx, node.Command{
		Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: metadata, Envelope: envelope},
	})
	if err != nil {
		return err
	}
	if outcome.State != node.CommandAccepted {
		return fmt.Errorf("Execution Node rejected selected Worker binding: %#v", outcome)
	}
	r.calls++
	r.envelope = envelope
	return nil
}

func TestProjectlessServerDispatchPreservesOwnerExecutionNodeOverride(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	localHarness := core.HarnessInstance{
		ID: "local/codex", Node: "local", Kind: core.HarnessCodex, Version: "1", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}},
	}
	remoteHarness := localHarness
	remoteHarness.ID, remoteHarness.Node, remoteHarness.Kind = "remote/claude", "remote", core.HarnessClaudeCode
	for _, fixture := range []struct {
		name     core.NodeReference
		instance core.HarnessInstance
	}{
		{name: "local", instance: localHarness},
		{name: "remote", instance: remoteHarness},
	} {
		if _, err := store.EnrollNode(ctx, fixture.name); err != nil {
			t.Fatal(err)
		}
		if err := store.UpdateNodeHeartbeat(ctx, fixture.name, core.HarnessInventorySnapshot{
			Node: fixture.name, ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{fixture.instance},
		}, core.NodeHeartbeat{Capacity: 1}); err != nil {
			t.Fatal(err)
		}
	}
	native := &projectlessNativeRuntime{}
	localStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	executor := node.NewExecutionNode("remote", native, localStore)
	if err := executor.SetInventory(core.HarnessInventorySnapshot{
		Node: "remote", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{remoteHarness},
	}); err != nil {
		t.Fatal(err)
	}
	runtime := &projectlessExecutionRuntime{executor: executor}
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessCodex, PreferredHarnesses: []core.HarnessKind{core.HarnessClaudeCode}},
		WorkerProfileSource: syntheticWorkerTemplateSource(),
		Runtime: runtime,
	}
	automatic, err := store.ResolveDispatch(ctx, core.DispatchResolutionRequest{WorkerPolicy: service.WorkerPolicy})
	if err != nil || automatic.Node != "local" || automatic.HarnessInstance.ID != localHarness.ID {
		t.Fatalf("automatic Node/harness preference: %#v err=%v", automatic, err)
	}
	created, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "review this repository", IdempotencyKey: "remote-owner-override",
		Preferences: WorkerPreferences{NodeID: "remote", HarnessKind: core.HarnessClaudeCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.calls != 1 || created.Worker.ProjectID != "" || created.Worker.NodeID != "remote" ||
		created.Worker.HarnessInstanceID != "remote/claude" || runtime.envelope.ProjectID != "" ||
		runtime.envelope.HarnessInstance.ID != remoteHarness.ID {
		t.Fatalf("Server to Execution Node override mismatch: worker=%#v envelope=%#v", created.Worker, runtime.envelope)
	}
	native.mu.Lock()
	starts := append([]node.StartRequest(nil), native.starts...)
	native.mu.Unlock()
	if len(starts) != 1 || starts[0].HarnessInstance.ID != remoteHarness.ID || starts[0].Workspace == "" {
		t.Fatalf("Execution Node did not start selected harness in its own workspace: %#v", starts)
	}
	bound, err := store.ResolveWorkerBinding(ctx, created.Worker.ID)
	if err != nil || bound.Node != "remote" || bound.HarnessInstance.ID != remoteHarness.ID {
		t.Fatalf("Server did not persist actual selected binding: %#v err=%v", bound, err)
	}
	if _, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "different harness", IdempotencyKey: "bad-override",
		Preferences: WorkerPreferences{NodeID: "remote", HarnessKind: core.HarnessCodex},
	}); !errors.Is(err, core.ErrInvalidDispatchPin) {
		t.Fatalf("incompatible Node/harness override should fail visibly: %v", err)
	}
	if runtime.calls != 1 {
		t.Fatal("incompatible override was sent to Execution Node")
	}
}
