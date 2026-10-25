package ctl

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestProjectlessWorkerKeepsSecretaryContextUsableAfterResultAndFollowup(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "codex", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "secretary profile",
	}); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(t.TempDir(), "user.md")
	if _, err := store.SaveUserDocument(ctx, userPath, "# User\n"); err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "local"); err != nil {
		t.Fatal(err)
	}
	codex := core.HarnessInstance{
		ID: "local/codex", Node: "local", Kind: core.HarnessCodex, Version: "1",
		Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}},
	}
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{codex},
	}, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	native := &projectlessNativeRuntime{}
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability,
		WorkerPolicy:        core.HarnessPolicy{DefaultHarness: core.HarnessCodex},
		WorkerProfileSource: syntheticWorkerTemplateSource(),
		Runtime:             legacyLocalTestRuntime{Local: node.NewLocal(native)},
	}
	spawned, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect the parser", IdempotencyKey: "projectless-context"})
	if err != nil {
		t.Fatal(err)
	}
	if spawned.Worker.ProjectID != "" {
		t.Fatalf("projectless Worker gained a Project: %q", spawned.Worker.ProjectID)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("projectless Worker broke the context reconstruction: %v", err)
	}
	if _, err := core.SecretaryContextPrompt(canonical, "first question"); err != nil {
		t.Fatalf("projectless Worker broke the Secretary prompt: %v", err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, spawned.Attempts[0].ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "Which parser should I read first?",
	}); err != nil {
		t.Fatal(err)
	}
	withResult, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("projectless Worker Result broke the context reconstruction: %v", err)
	}
	if len(withResult.UnseenWorkerResults) != 1 {
		t.Fatalf("unseen results=%#v", withResult.UnseenWorkerResults)
	}
	if _, err := core.SecretaryContextPrompt(withResult, "the tokenizer"); err != nil {
		t.Fatalf("projectless Worker Result broke the Secretary prompt: %v", err)
	}
	followup, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef: spawned.Worker.WorkerRef, Text: "the tokenizer", IdempotencyKey: "projectless-context-followup",
	})
	if err != nil {
		t.Fatalf("follow-up to the projectless Worker failed: %v", err)
	}
	if followup.Worker.ID != spawned.Worker.ID || followup.Worker.WorkerRef != spawned.Worker.WorkerRef {
		t.Fatalf("follow-up migrated to another Worker: %#v", followup.Worker)
	}
	afterFollowup, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("context after the follow-up failed: %v", err)
	}
	if _, err := core.SecretaryContextPrompt(afterFollowup, "continue"); err != nil {
		t.Fatalf("follow-up broke the Secretary prompt: %v", err)
	}
	workers, err := store.WorkersForConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 || workers[0].WorkerRef != spawned.Worker.WorkerRef {
		t.Fatalf("follow-up created another Worker: %#v", workers)
	}
}
