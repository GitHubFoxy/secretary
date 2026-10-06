package ctl

import (
	"errors"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestWorkerTemplateSourceFailuresLeaveNoWorkerBinding(t *testing.T) {
	for _, scenario := range []string{"missing", "unavailable", "project_permission_mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, store, service, project := newWorkerService(t)
			instance := core.HarnessInstance{
				ID: "node/opencode", Node: "node", Kind: core.HarnessOpenCode, Version: "2.0.22",
				Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
				Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}},
				ModelIDs:     []core.ObservedModelID{"fixture/model"}, ReasoningLevels: []core.ObservedReasoningLevel{"xhigh"},
			}
			policy := core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessOpenCode}, ModelID: "fixture/model", Reasoning: "xhigh"}
			if scenario == "project_permission_mismatch" {
				policy.Execution.DeniedCapabilities = []core.ExecutionCapability{core.CapabilityShell}
			}
			updated, err := store.UpdateProject(ctx, project.ID, core.ProjectSpec{
				ID: project.ID, Name: project.Name, Mappings: project.Mappings, Policy: policy,
			}, project.Revision)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpdateNodeHeartbeat(ctx, "node", core.HarnessInventorySnapshot{
				Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance},
			}, core.NodeHeartbeat{Capacity: 2}); err != nil {
				t.Fatal(err)
			}
			service.WorkerPolicy = core.HarnessPolicy{DefaultHarness: core.HarnessOpenCode, ModelID: "fixture/model", Reasoning: "xhigh"}
			service.WorkerProfileSource = func() (node.ManagedProfile, error) {
				switch scenario {
				case "missing":
					return node.ManagedProfile{}, nil
				case "unavailable":
					return node.ManagedProfile{}, errors.New("synthetic unavailable source")
				default:
					return node.ManagedProfile{Version: "synthetic-config-v1", Name: "worker", Content: "Synthetic profile.",
						AllowTools: []string{"bash"}, Hash: "synthetic-source-hash", Runtime: "opencode", Model: "fixture/model", Reasoning: "xhigh"}, nil
				}
			}
			_, err = service.SpawnWorker(ctx, SpawnWorkerRequest{
				Intent: "synthetic profile policy fixture", ProjectID: updated.ID, IdempotencyKey: "profile-fail-closed-" + scenario,
			})
			want := ErrWorkerProfileUnavailable
			if scenario == "project_permission_mismatch" {
				want = ErrWorkerProfileInvalid
			}
			if !errors.Is(err, want) {
				t.Fatalf("Profile source failure category matched=%t", errors.Is(err, want))
			}
			workers, err := service.ListWorkers(ctx)
			if err != nil || len(workers) != 0 {
				t.Fatalf("invalid/unavailable Profile created Worker state: workers=%d err_present=%t", len(workers), err != nil)
			}
		})
	}
}
