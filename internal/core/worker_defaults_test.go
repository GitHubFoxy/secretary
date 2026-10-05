package core

import (
	"context"
	"errors"
	"testing"
)

func TestEffectiveDispatchDefaultsYieldToProjectAndExplicitPreferences(t *testing.T) {
	defaults := HarnessPolicy{ModelID: "openai/gpt-6-luna", Reasoning: "xhigh"}
	tests := []struct {
		name             string
		policy           ProjectPolicy
		request          DispatchResolutionRequest
		model, reasoning string
		invalid          bool
	}{
		{"defaults", ProjectPolicy{}, DispatchResolutionRequest{WorkerPolicy: defaults}, defaults.ModelID, defaults.Reasoning, false},
		{"project", ProjectPolicy{ModelID: "project-model", Reasoning: "low"}, DispatchResolutionRequest{WorkerPolicy: defaults}, "project-model", "low", false},
		{"explicit", ProjectPolicy{}, DispatchResolutionRequest{WorkerPolicy: defaults, ModelID: "requested", Reasoning: "medium"}, "requested", "medium", false},
		{"conflict", ProjectPolicy{ModelID: "project-model"}, DispatchResolutionRequest{WorkerPolicy: defaults, ModelID: "requested"}, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, reasoning, err := effectiveDispatchPins(tt.policy, tt.request)
			if tt.invalid {
				if !errors.Is(err, ErrInvalidDispatchPin) {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || model != tt.model || reasoning != tt.reasoning {
				t.Fatalf("model=%q reasoning=%q err=%v", model, reasoning, err)
			}
		})
	}
}

func TestWorkerDefaultModelMustBeObservedWithoutFallback(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "defaults", Name: "Defaults", Mappings: []ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	instance := resolverInstance("node/opencode", "node", HarnessOpenCode)
	instance.ModelIDs = []ObservedModelID{"openai/gpt-6-luna"}
	instance.ReasoningLevels = []ObservedReasoningLevel{"xhigh"}
	resolverEnroll(t, store, "node", resolverInventory("node", []HarnessInstance{instance}), 1, nil)
	request := DispatchResolutionRequest{ProjectID: project.ID, WorkerPolicy: HarnessPolicy{DefaultHarness: HarnessOpenCode, ModelID: "openai/gpt-6-luna", Reasoning: "xhigh"}}
	resolved, err := store.ResolveDispatch(ctx, request)
	if err != nil || resolved.Snapshot.Policy.ModelPin() != request.WorkerPolicy.ModelID || resolved.Snapshot.Policy.Reasoning != "xhigh" {
		t.Fatalf("resolution=%#v err=%v", resolved, err)
	}
	request.WorkerPolicy.ModelID = "unobserved"
	if _, err := store.ResolveDispatch(ctx, request); !errors.Is(err, ErrObservedPinUnavailable) {
		t.Fatalf("unobserved model error=%v", err)
	}
}
