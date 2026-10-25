package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func projectlessWorkerSnapshot(t *testing.T, node NodeReference, instance HarnessInstance) string {
	t.Helper()
	snapshot := ProjectSnapshot{Node: node, HarnessInstance: instance}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func projectlessContextFixture(t *testing.T) (*Store, context.Context, string, Conversation, SecretaryIdentity, HarnessInstance) {
	t.Helper()
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
	if _, err := store.SaveUserDocument(ctx, userPath, "# User\n"); err != nil {
		t.Fatal(err)
	}
	instance := HarnessInstance{
		ID: "node-projectless/fx", Node: "node-projectless", Kind: HarnessFX, Version: "1", Status: HarnessReady,
		Authentication: HarnessAuthentication{Authenticated: true},
	}
	if _, err := store.EnrollNode(ctx, instance.Node); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, instance.Node, HarnessInventorySnapshot{
		Node: instance.Node, ObservedAt: time.Now().UTC(), Instances: []HarnessInstance{instance},
	}); err != nil {
		t.Fatal(err)
	}
	return store, ctx, userPath, conversation, identity, instance
}

func TestProjectlessWorkerKeepsCanonicalSecretaryContextValid(t *testing.T) {
	store, ctx, userPath, conversation, identity, instance := projectlessContextFixture(t)
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "projectless-worker", Intent: "inspect the repository",
		ProjectID: "", NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: projectlessWorkerSnapshot(t, instance.Node, instance),
	}, TurnSpec{Input: "inspect the repository"})
	if err != nil {
		t.Fatal(err)
	}
	if worker.ProjectID != "" {
		t.Fatalf("projectless Worker gained a Project: %q", worker.ProjectID)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("projectless Worker invalidated the canonical context: %v", err)
	}
	if len(canonical.OpenWorkers) != 1 || canonical.OpenWorkers[0].WorkerRef != worker.WorkerRef {
		t.Fatalf("open workers=%#v", canonical.OpenWorkers)
	}
	if _, err := SecretaryContextPrompt(canonical, "next question"); err != nil {
		t.Fatalf("projectless Worker broke the Secretary prompt: %v", err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "next question")
	if err != nil {
		t.Fatal(err)
	}
	turnContext, err := store.ReconstructSecretaryContextForTurn(ctx, turn.ID, userPath, 10)
	if err != nil {
		t.Fatalf("next user turn could not restore the canonical context: %v", err)
	}
	if len(turnContext.OpenWorkers) != 1 || turnContext.OpenWorkers[0].WorkerRef != worker.WorkerRef {
		t.Fatalf("turn context workers=%#v", turnContext.OpenWorkers)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "What should I inspect first?",
	}); err != nil {
		t.Fatal(err)
	}
	withResult, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("context after the projectless Worker Result failed: %v", err)
	}
	if len(withResult.UnseenWorkerResults) != 1 || withResult.UnseenWorkerResults[0].Summary != "What should I inspect first?" {
		t.Fatalf("unseen results=%#v", withResult.UnseenWorkerResults)
	}
	if _, err := SecretaryContextPrompt(withResult, "answer the question"); err != nil {
		t.Fatalf("projectless Worker Result broke the Secretary prompt: %v", err)
	}
}

func TestProjectlessWorkerApprovalKeepsCanonicalSecretaryContextValid(t *testing.T) {
	store, ctx, userPath, conversation, identity, instance := projectlessContextFixture(t)
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "projectless-approval-worker", Intent: "await approval", ProjectID: "",
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: projectlessWorkerSnapshot(t, instance.Node, instance),
	}, TurnSpec{Input: "await approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, Activity{
		Metadata: ActivityMetadata{
			EventID: "projectless-approval", Node: instance.Node, HarnessInstanceID: instance.ID,
			WorkerRef: worker.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
		},
		Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: "projectless-approval", Summary: "write file"},
	}); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("projectless Approval broke the context reconstruction: %v", err)
	}
	if len(canonical.ActiveApprovals) != 1 || canonical.ActiveApprovals[0].ProjectID != "" {
		t.Fatalf("approvals=%#v", canonical.ActiveApprovals)
	}
	if _, err := SecretaryContextPrompt(canonical, "approve"); err != nil {
		t.Fatalf("projectless Approval broke the Secretary prompt: %v", err)
	}
}

func TestSecretaryContextAcceptsProjectAndProjectlessWorkersTogether(t *testing.T) {
	store, ctx, userPath, conversation, identity, instance := projectlessContextFixture(t)
	root := t.TempDir()
	project, err := store.CreateProject(ctx, ProjectSpec{
		ID: "project-mixed", Name: "Mixed project", Mappings: []ProjectPathMapping{{Node: instance.Node, Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	projectSnapshot := project.Snapshot(instance.Node, root, instance)
	encodedProject, err := json.Marshal(projectSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "bound-worker", Intent: "bound work", ProjectID: project.ID,
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: string(encodedProject),
	}, TurnSpec{Input: "bound work"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "projectless-worker", Intent: "projectless work", ProjectID: "",
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: projectlessWorkerSnapshot(t, instance.Node, instance),
	}, TurnSpec{Input: "projectless work"}); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("mixed context rejected: %v", err)
	}
	if len(canonical.OpenWorkers) != 2 || len(canonical.Projects) != 1 {
		t.Fatalf("mixed context workers=%#v projects=%#v", canonical.OpenWorkers, canonical.Projects)
	}
	if _, err := SecretaryContextPrompt(canonical, "continue"); err != nil {
		t.Fatalf("mixed canonical context broke the Secretary prompt: %v", err)
	}
}

func TestSecretaryContextRejectsInvalidProjectReferences(t *testing.T) {
	store, ctx, userPath, conversation, identity, instance := projectlessContextFixture(t)
	root := t.TempDir()
	project, err := store.CreateProject(ctx, ProjectSpec{
		ID: "project-strict", Name: "Strict project", Mappings: []ProjectPathMapping{{Node: instance.Node, Path: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	projectSnapshot := project.Snapshot(instance.Node, root, instance)
	encodedProject, err := json.Marshal(projectSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "unknown-project-worker", Intent: "unknown project", ProjectID: "project-missing",
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: string(encodedProject),
	}, TurnSpec{Input: "unknown project"}); err != nil {
		t.Fatal(err)
	}
	unknown, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SecretaryContextPrompt(unknown, "continue"); err == nil {
		t.Fatal("Worker referencing an unknown Project was accepted")
	}
	if _, _, _, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "claiming-project-worker", Intent: "claiming project", ProjectID: "",
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: string(encodedProject),
	}, TurnSpec{Input: "claiming project"}); err != nil {
		t.Fatal(err)
	}
	claiming, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SecretaryContextPrompt(claiming, "continue"); err == nil {
		t.Fatal("projectless Worker that claims a Project snapshot was accepted")
	}
	foreignSnapshot := projectSnapshot
	foreignSnapshot.Node = "node-other"
	encodedForeign, err := json.Marshal(foreignSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "foreign-node-worker", Intent: "foreign node", ProjectID: "",
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: string(encodedForeign),
	}, TurnSpec{Input: "foreign node"}); err != nil {
		t.Fatal(err)
	}
	foreign, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SecretaryContextPrompt(foreign, "continue"); err == nil {
		t.Fatal("projectless Worker with a foreign Node snapshot was accepted")
	}
}

func TestProjectlessWorkerContextSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(t.TempDir(), "user.md")
	if _, err := store.SaveUserDocument(ctx, userPath, "# User\n"); err != nil {
		t.Fatal(err)
	}
	instance := HarnessInstance{ID: "node-restart/fx", Node: "node-restart", Kind: HarnessFX, Version: "1", Status: HarnessReady, Authentication: HarnessAuthentication{Authenticated: true}}
	if _, err := store.EnrollNode(ctx, instance.Node); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, instance.Node, HarnessInventorySnapshot{
		Node: instance.Node, ObservedAt: time.Now().UTC(), Instances: []HarnessInstance{instance},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "restart-projectless", Intent: "restart work", ProjectID: "",
		NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID),
		PolicySnapshot: "worker-policy-v1", ProjectSnapshot: projectlessWorkerSnapshot(t, instance.Node, instance),
	}, TurnSpec{Input: "restart work"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatalf("restart lost the projectless Worker context: %v", err)
	}
	if len(canonical.OpenWorkers) != 1 || canonical.OpenWorkers[0].ProjectID != "" {
		t.Fatalf("restart changed the projectless Worker: %#v", canonical.OpenWorkers)
	}
	if _, err := SecretaryContextPrompt(canonical, "continue"); err != nil {
		t.Fatalf("canonical context after restart broke the Secretary prompt: %v", err)
	}
}
