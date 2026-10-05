package ctl

import (
	"github.com/beruseruko/secretary/internal/core"
	"testing"
	"time"
)

func TestWorkerPolicyReloadAffectsNewBindingsOnlyAndNotReplay(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	instance := core.HarnessInstance{ID: "node/opencode", Node: "node", Kind: core.HarnessOpenCode, Version: "2", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, ModelIDs: []core.ObservedModelID{"openai/gpt-6-luna", "openai/gpt-6.1-sol"}, ReasoningLevels: []core.ObservedReasoningLevel{"xhigh", "low"}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}}}
	if err := store.UpdateNodeHeartbeat(ctx, "node", core.HarnessInventorySnapshot{Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	policy := core.HarnessPolicy{DefaultHarness: core.HarnessOpenCode, ModelID: "openai/gpt-6-luna", Reasoning: "xhigh"}
	calls := 0
	service.WorkerPolicySource = func() core.HarnessPolicy { calls++; return policy }
	request := SpawnWorkerRequest{Intent: "private policy fixture", ProjectID: project.ID, IdempotencyKey: "policy-first"}
	first, err := service.SpawnWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ResolveWorkerBinding(ctx, first.Worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Snapshot.Policy.ModelPin() != policy.ModelID || before.Snapshot.Policy.Reasoning != "xhigh" {
		t.Fatalf("first policy=%#v", before.Snapshot.Policy)
	}
	policy.ModelID = "openai/gpt-6.1-sol"
	policy.Reasoning = "low"
	replay, err := service.SpawnWorker(ctx, request)
	if err != nil || replay.Worker.ID != first.Worker.ID || calls != 1 {
		t.Fatalf("replay changed binding or consulted new policy: calls=%d err=%v", calls, err)
	}
	request.IdempotencyKey = "policy-second"
	second, err := service.SpawnWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	newBinding, err := store.ResolveWorkerBinding(ctx, second.Worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldBinding, err := store.ResolveWorkerBinding(ctx, first.Worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || newBinding.Snapshot.Policy.ModelPin() != policy.ModelID || newBinding.Snapshot.Policy.Reasoning != "low" || oldBinding.Snapshot.Policy.ModelPin() != before.Snapshot.Policy.ModelPin() || oldBinding.Snapshot.Policy.Reasoning != "xhigh" {
		t.Fatal("new defaults changed existing binding")
	}
}
