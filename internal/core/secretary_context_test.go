package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSecretaryContextReconstructionUsesOnlyServerOwnedSources(t *testing.T) {
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
	if _, err := store.SaveUserDocument(ctx, userPath, "# User\nPrefer concise answers.\n"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryConversationSummary(ctx, conversation.ID, "Owner prefers durable execution."); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEntry(ctx, conversation.ID, EntryUser, "old message"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEntry(ctx, conversation.ID, EntrySecretary, "old answer"); err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, ProjectSpec{ID: "project-context", Name: "Context project", Mappings: []ProjectPathMapping{{Node: "node-context", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	inventory := HarnessInventorySnapshot{Node: "node-context", ObservedAt: time.Now().UTC(), Instances: []HarnessInstance{{
		ID: "node-context/fx", Node: "node-context", Kind: HarnessFX, Version: "1", Status: HarnessReady,
		Authentication: HarnessAuthentication{Authenticated: true},
	}}}
	if _, err := store.EnrollNode(ctx, "node-context"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkNodeConnected(ctx, "node-context", inventory); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "context-worker", Intent: "inspect context", ProjectID: project.ID, NodeID: "node-context", HarnessInstanceID: "node-context/fx", PolicySnapshot: "worker-policy-v1",
	}, TurnSpec{Input: "inspect context"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "context result"}); err != nil {
		t.Fatal(err)
	}
	approvalWorker, _, approvalAttempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "approval-worker", Intent: "await approval", ProjectID: project.ID, NodeID: "node-context", HarnessInstanceID: "node-context/fx", PolicySnapshot: "worker-policy-v1",
	}, TurnSpec{Input: "await approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordNodeActivityReplay(ctx, Activity{Metadata: ActivityMetadata{EventID: "approval-context", Node: "node-context", HarnessInstanceID: "node-context/fx", WorkerRef: approvalWorker.WorkerRef, TurnID: approvalAttempt.TurnID, AttemptID: approvalAttempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityPermissionRequest, Request: &ActivityRequest{RequestID: "approval-context", Summary: "write file"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{Version: "cfg-v1", Harness: "fx", Model: "secretary-model", Reasoning: "high", ProfileName: "secretary", ProfileVersion: "cfg-v1", ProfileHash: "profile-hash", ProfileContent: "managed profile"}); err != nil {
		t.Fatal(err)
	}

	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Identity.ID != identity.ID || canonical.UserDocument.Content == "" || canonical.UserDocument.Revision < 2 {
		t.Fatalf("identity/user document=%#v", canonical)
	}
	if canonical.ConversationSummary != "Owner prefers durable execution." || len(canonical.RecentEntries) != 3 {
		t.Fatalf("conversation context=%#v", canonical)
	}
	if len(canonical.UnseenWorkerResults) != 1 || canonical.UnseenWorkerResults[0].Summary != "context result" {
		t.Fatalf("unseen results=%#v", canonical.UnseenWorkerResults)
	}
	if len(canonical.OpenWorkers) != 2 || len(canonical.Projects) != 1 || canonical.Projects[0].ID != project.ID {
		t.Fatalf("workers/projects=%#v/%#v", canonical.OpenWorkers, canonical.Projects)
	}
	if len(canonical.Nodes) != 1 || len(canonical.HarnessInstances) != 1 || canonical.HarnessInstances[0].ID != "node-context/fx" {
		t.Fatalf("node inventory=%#v/%#v", canonical.Nodes, canonical.HarnessInstances)
	}
	if len(canonical.ActiveApprovals) != 1 || canonical.ActiveApprovals[0].RequestID != "approval-context" {
		t.Fatalf("approvals=%#v", canonical.ActiveApprovals)
	}
	if canonical.PolicyProfile.Version != "cfg-v1" || canonical.PolicyProfile.ProfileHash != "profile-hash" {
		t.Fatalf("policy profile=%#v", canonical.PolicyProfile)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("canonical context must be serializable: %v", err)
	}
}

func TestSecretaryContextReconstructionSurvivesRestartAndDoesNotTrustNativeSession(t *testing.T) {
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
	userPath := filepath.Join(t.TempDir(), "user.md")
	if _, err := store.SaveUserDocument(ctx, userPath, "new preference"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "next"); err != nil {
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
	if err := os.WriteFile(userPath, []byte("updated preference"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.UserDocument.Content != "updated preference" || canonical.UserDocument.Revision != 3 || canonical.Identity.ID != identity.ID {
		t.Fatalf("reconstructed=%#v", canonical)
	}
}

func TestReconstructSecretaryContextForTurnSnapshotIsImmutableAndDoesNotStealLaterResults(t *testing.T) {
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
	if _, err := store.SaveUserDocument(ctx, userPath, "stable context"); err != nil {
		t.Fatal(err)
	}
	firstWorker, _, firstAttempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "snapshot-first", Intent: "first", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, firstAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, firstAttempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "first result"}); err != nil || result == nil {
		t.Fatalf("first result=%#v err=%v", result, err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "summarize")
	if err != nil {
		t.Fatal(err)
	}
	firstSnapshot, err := store.ReconstructSecretaryContextForTurn(ctx, turn.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	encodedFirst, err := json.Marshal(firstSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	secondWorker, _, secondAttempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "snapshot-second", Intent: "second", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, secondAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, secondAttempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "second result"}); err != nil || result == nil {
		t.Fatalf("second result=%#v err=%v", result, err)
	}
	secondSnapshot, err := store.ReconstructSecretaryContextForTurn(ctx, turn.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	encodedSecond, err := json.Marshal(secondSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedSecond) != string(encodedFirst) {
		t.Fatalf("snapshot changed on repeat: first=%s second=%s", encodedFirst, encodedSecond)
	}
	if len(secondSnapshot.UnseenWorkerResults) != 1 || secondSnapshot.UnseenWorkerResults[0].WorkerID != firstWorker.ID {
		t.Fatalf("repeat returned rebuilt context=%#v", secondSnapshot.UnseenWorkerResults)
	}
	remaining, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.UnseenWorkerResults) != 1 || remaining.UnseenWorkerResults[0].WorkerID != secondWorker.ID {
		t.Fatalf("later Result was stolen: %#v", remaining.UnseenWorkerResults)
	}
}

func TestReconstructSecretaryContextForTurnConcurrentCallsMaterializeOnce(t *testing.T) {
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
	if _, err := store.SaveUserDocument(ctx, userPath, "concurrent context"); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "snapshot-concurrent", Intent: "concurrent", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "concurrent result"}); err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "summarize concurrently")
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		context SecretaryContext
		err     error
	}
	responses := make(chan response, 8)
	for i := 0; i < 8; i++ {
		go func() {
			value, callErr := store.ReconstructSecretaryContextForTurn(ctx, turn.ID, userPath, 20)
			responses <- response{context: value, err: callErr}
		}()
	}
	var first string
	for i := 0; i < 8; i++ {
		value := <-responses
		if value.err != nil {
			t.Fatal(value.err)
		}
		encoded, marshalErr := json.Marshal(value.context)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if first == "" {
			first = string(encoded)
		} else if string(encoded) != first {
			t.Fatalf("concurrent snapshot mismatch: first=%s got=%s", first, encoded)
		}
	}
	remaining, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.UnseenWorkerResults) != 0 {
		t.Fatalf("concurrent materialization did not acknowledge exactly once: %#v", remaining.UnseenWorkerResults)
	}
}

func TestTerminalWorkerResultIsImmediateAndUnseenForNextSecretaryContext(t *testing.T) {
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
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "result-worker", Intent: "finish", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "finish"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "finished directly"}); err != nil || result == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != EntryWorkerResult {
		t.Fatalf("entries=%#v", entries)
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("worker result created Secretary turn: %#v", turns)
	}
	canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, filepath.Join(t.TempDir(), "user.md"), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical.UnseenWorkerResults) != 1 || canonical.UnseenWorkerResults[0].WorkerID != worker.ID {
		t.Fatalf("unseen=%#v", canonical.UnseenWorkerResults)
	}
	queued, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "tell me the result")
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.StartSecretaryTurn(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snap SecretaryContext
	if err := json.Unmarshal([]byte(started.ContextSnapshot), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.UnseenWorkerResults) != 1 || snap.UnseenWorkerResults[0].ID == "" {
		t.Fatalf("started context snapshot=%#v", snap)
	}
	remaining, err := store.ReconstructSecretaryContext(ctx, identity.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.UnseenWorkerResults) != 0 {
		t.Fatalf("result was not acknowledged by started turn: %#v", remaining.UnseenWorkerResults)
	}
}
