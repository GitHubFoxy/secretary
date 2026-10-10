package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSecretaryContextRetainsUnavailableHistoricalWorkerHarness(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(t.TempDir(), "user.md")
	if _, err := store.SaveUserDocument(ctx, userPath, "Owner context"); err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "acceptance", Name: "Acceptance", Mappings: []ProjectPathMapping{{Node: "omarchy", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	fx := resolverInstance("omarchy/fx", "omarchy", HarnessFX)
	resolverEnroll(t, store, "omarchy", resolverInventory("omarchy", []HarnessInstance{fx}), 1, nil)
	worker, _, attempt, _, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "historical task", DispatchResolutionRequest{ProjectID: project.ID, HarnessInstanceID: fx.ID}, "historical-worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "historical result"}); err != nil {
		t.Fatal(err)
	}
	before, err := store.Worker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	codex := resolverInstance("omarchy/codex", "omarchy", HarnessCodex)
	current := resolverInventory("omarchy", []HarnessInstance{codex})
	if err := store.UpdateNodeInventory(ctx, "omarchy", current); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SecretaryContextPrompt(canonical, "continue with Codex"); err != nil {
		t.Fatalf("historical Worker prevented Secretary prompt: %v", err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "continue with Codex")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.ReconstructSecretaryContextForTurn(ctx, turn.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SecretaryContextPrompt(persisted, turn.Input); err != nil {
		t.Fatalf("persisted historical context prevented Secretary prompt: %v", err)
	}
	if _, err := store.ResolveDispatch(ctx, DispatchResolutionRequest{ProjectID: project.ID, HarnessInstanceID: fx.ID}); err == nil {
		t.Fatal("historical context made removed harness dispatchable")
	}
	if len(canonical.HarnessInstances) != 2 {
		t.Fatalf("historical HarnessInstance missing: %#v", canonical.HarnessInstances)
	}
	for _, instance := range canonical.HarnessInstances {
		if instance.ID == fx.ID && (instance.Status != HarnessUnavailable || instance.Authentication.Authenticated || len(instance.Capabilities.Execution) != 0 || len(instance.Capabilities.Activity) != 0 || len(instance.ModelIDs) != 0 || len(instance.ReasoningLevels) != 0) {
			t.Fatalf("historical HarnessInstance advertises current availability: %#v", instance)
		}
	}
	record, err := store.NodeRecord(ctx, "omarchy")
	if err != nil || !reflect.DeepEqual(record.Inventory, current) || !reflect.DeepEqual(canonical.Nodes[0].Inventory, current) {
		t.Fatalf("observed inventory changed: %v", err)
	}
	after, err := store.Worker(ctx, worker.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("historical Worker binding changed: %v", err)
	}
	canonical.OpenWorkers[0].HarnessInstanceID = "omarchy/unknown"
	if err := canonical.Validate(); err == nil {
		t.Fatal("unknown Worker HarnessInstance accepted")
	}
	canonical.OpenWorkers[0] = before
	var snapshot ProjectSnapshot
	if err := json.Unmarshal([]byte(before.ProjectSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.HarnessInstance.ID = "omarchy/unrelated"
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	canonical.OpenWorkers[0].ProjectSnapshot = string(encoded)
	if err := canonical.Validate(); err == nil {
		t.Fatal("mismatched immutable Worker snapshot accepted")
	}
}
