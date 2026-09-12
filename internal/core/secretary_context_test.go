package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSecretaryContextValidateRejectsIncompleteCanonicalSnapshots(t *testing.T) {
	cases := []struct {
		name    string
		encoded string
		reason  string
	}{
		{
			name:    "identity only",
			encoded: `{"identity":{"id":"sec-1","conversation_id":"conv-1"}}`,
			reason:  "identity-only snapshot must not be accepted",
		},
		{
			name:    "missing user and policy",
			encoded: `{"identity":{"id":"sec-1","person_id":"person-1","conversation_id":"conv-1","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},"recent_entries":[],"unseen_worker_results":[],"open_workers":[],"projects":[],"nodes":[],"harness_instances":[],"active_approvals":[]}`,
			reason:  "snapshot without user document and policy must not be accepted",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var snapshot SecretaryContext
			if err := json.Unmarshal([]byte(tc.encoded), &snapshot); err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Validate(); err == nil {
				t.Fatal(tc.reason)
			}
		})
	}
}

func TestSecretaryContextSnapshotRejectsRuntimeAndCredentialFields(t *testing.T) {
	for _, field := range []string{"runtime_session_id", "credential_hash", "callback_capability", "capability_secret"} {
		t.Run(field, func(t *testing.T) {
			var snapshot SecretaryContext
			encoded := `{"identity":{"id":"sec-1"},"` + field + `":"secret"}`
			if err := json.Unmarshal([]byte(encoded), &snapshot); err == nil {
				t.Fatalf("snapshot accepted forbidden field %q", field)
			}
		})
	}
}

func TestReconstructSecretaryContextMissingPolicySnapshotFailsClosed(t *testing.T) {
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
	if _, err := store.SaveUserDocument(ctx, userPath, "policy is required"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM secretary_policy_snapshots`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing policy snapshot error=%v", err)
	}
}

func TestSecretaryResultClaimReturnsAfterRestartRecovery(t *testing.T) {
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
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "restart-result", Intent: "result", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "worker-policy"}, TurnSpec{Input: "result"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	_, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "must survive restart"})
	if err != nil || result == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverSecretaryTurn(ctx, identity.ID, "runtime restarted before prompt"); err != nil {
		t.Fatal(err)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.StartSecretaryTurn(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot SecretaryContext
	if err := json.Unmarshal([]byte(started.ContextSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.UnseenWorkerResults) != 1 || snapshot.UnseenWorkerResults[0].ID != result.ID {
		t.Fatalf("recovered result=%#v", snapshot.UnseenWorkerResults)
	}
}

func TestSecretaryPromptAcceptedClaimDoesNotReappearAfterRecovery(t *testing.T) {
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
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "accepted prompt"); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "accepted-result", Intent: "result", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "worker-policy"}, TurnSpec{Input: "result"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "consumed once"}); err != nil || result == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginSecretaryPrompt(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptSecretaryPrompt(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverSecretaryTurn(ctx, identity.ID, "runtime restarted after accepted prompt"); err != nil {
		t.Fatal(err)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.StartSecretaryTurn(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot SecretaryContext
	if err := json.Unmarshal([]byte(started.ContextSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.UnseenWorkerResults) != 0 {
		t.Fatalf("accepted result reappeared=%#v", snapshot.UnseenWorkerResults)
	}
}

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
	if err := canonical.Validate(); err != nil {
		t.Fatalf("complete canonical context rejected: %v", err)
	}
	canonical.ConversationSummary = ""
	canonical.RecentEntries = nil
	if err := canonical.Validate(); err != nil {
		t.Fatalf("canonical context without optional recent content rejected: %v", err)
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
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
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

func TestConcurrentSecretaryStartsClaimResultForOnlyNextEligibleTurn(t *testing.T) {
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
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "race-worker", Intent: "race", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "race"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "one result"}); err != nil || result == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan struct {
		turn SecretaryTurn
		err  error
	}, 2)
	var wg sync.WaitGroup
	for _, queued := range []SecretaryTurn{first, second} {
		wg.Add(1)
		go func(queued SecretaryTurn) {
			defer wg.Done()
			<-start
			turn, startErr := store.StartSecretaryTurn(ctx, queued.ID)
			results <- struct {
				turn SecretaryTurn
				err  error
			}{turn: turn, err: startErr}
		}(queued)
	}
	close(start)
	wg.Wait()
	close(results)

	var owner SecretaryTurn
	for result := range results {
		if result.err == nil {
			if owner.ID != "" {
				t.Fatalf("two turns started: %s and %s", owner.ID, result.turn.ID)
			}
			owner = result.turn
		} else if !errors.Is(result.err, ErrInvalidTransition) {
			t.Fatalf("concurrent start error: %v", result.err)
		}
	}
	if owner.ID == "" {
		t.Fatal("no queued turn started")
	}
	var snapshot SecretaryContext
	if err := json.Unmarshal([]byte(owner.ContextSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.UnseenWorkerResults) != 1 {
		t.Fatalf("owner snapshot=%#v", snapshot.UnseenWorkerResults)
	}
	if _, err := store.FinishSecretaryTurn(ctx, owner.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	other := second
	if owner.ID == second.ID {
		other = first
	}
	startedOther, err := store.StartSecretaryTurn(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	var later SecretaryContext
	if err := json.Unmarshal([]byte(startedOther.ContextSnapshot), &later); err != nil {
		t.Fatal(err)
	}
	if len(later.UnseenWorkerResults) != 0 {
		t.Fatalf("later turn duplicated result: %#v", later.UnseenWorkerResults)
	}
}

func TestIneligibleSecretaryStartDoesNotConsumeResult(t *testing.T) {
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
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "failed-start-worker", Intent: "result", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "result"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "must survive"}); err != nil || result == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, second.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ineligible start error=%v", err)
	}
	failedTurn, err := store.SecretaryTurn(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failedTurn.ContextSnapshot != "" {
		t.Fatalf("ineligible start published snapshot: %q", failedTurn.ContextSnapshot)
	}
	remaining, err := store.ReconstructSecretaryContext(ctx, identity.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.UnseenWorkerResults) != 1 {
		t.Fatalf("ineligible start consumed result: %#v", remaining.UnseenWorkerResults)
	}
	if _, err := store.FinishSecretaryTurn(ctx, first.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	started, err := store.StartSecretaryTurn(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot SecretaryContext
	if err := json.Unmarshal([]byte(started.ContextSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.UnseenWorkerResults) != 1 || snapshot.UnseenWorkerResults[0].Summary != "must survive" {
		t.Fatalf("result consumed by failed start: %#v", snapshot.UnseenWorkerResults)
	}
}

func TestSecretaryContextReconstructionIsPureAcrossRepeats(t *testing.T) {
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
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "pure-worker", Intent: "pure", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "snapshot"}, TurnSpec{Input: "pure"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "pure result"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		canonical, err := store.ReconstructSecretaryContext(ctx, identity.ID, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(canonical.UnseenWorkerResults) != 1 {
			t.Fatalf("pure reconstruction=%#v", canonical.UnseenWorkerResults)
		}
	}
	var seen int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_context_seen_results`).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatalf("pure reconstruction mutated seen claims: %d", seen)
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
