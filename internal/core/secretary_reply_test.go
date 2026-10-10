package core

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestLegacySecretaryTurnsReceiveServerInputIdentityOnMigration(t *testing.T) {
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
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "legacy queued input")
	if err != nil {
		t.Fatal(err)
	}
	beforeEntries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE secretary_turns SET input_id = '' WHERE id = ?`, turn.ID); err != nil {
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
	migrated, err := store.SecretaryTurn(ctx, turn.ID)
	if err != nil || migrated.InputID == "" {
		t.Fatalf("legacy turn was not assigned a server input identity: turn=%#v err=%v", migrated, err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil || len(entries) != len(beforeEntries) {
		t.Fatalf("migration changed conversation history: before=%#v after=%#v err=%v", beforeEntries, entries, err)
	}
}

func TestAddressedSecretaryReplyUsesServerOriginAndExactWorkerResults(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
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
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "dispatch two independent tasks and answer me")
	if err != nil {
		t.Fatal(err)
	}
	if origin.InputID == "" {
		t.Fatal("queued Secretary turn has no server-issued input identity")
	}
	if _, err := store.StartSecretaryTurn(ctx, origin.ID); err != nil {
		t.Fatal(err)
	}

	createResult := func(ref, summary string) Phase4Result {
		t.Helper()
		_, _, attempt, createErr := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
			WorkerRef: ref, Intent: "synthetic task", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic",
		}, TurnSpec{Input: "synthetic task"})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, setErr := store.SetPhase4AttemptActive(ctx, attempt.ID); setErr != nil {
			t.Fatal(setErr)
		}
		_, result, _, outcomeErr := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: summary})
		if outcomeErr != nil || result == nil {
			t.Fatalf("record Worker Result: result=%#v err=%v", result, outcomeErr)
		}
		return *result
	}

	// The first Result is durable before its MCP origin link; the second follows
	// the link. Both delivery orders must produce the same exact Result relation.
	first := createResult("worker-fast", "fast result")
	if _, err := store.LinkSecretaryWorkerTurn(ctx, person.ID, capability, origin.ID, origin.InputID, first.TurnID); err != nil {
		t.Fatal(err)
	}
	_, _, secondAttempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "worker-slow", Intent: "synthetic task", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic",
	}, TurnSpec{Input: "synthetic task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, secondAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LinkSecretaryWorkerTurn(ctx, person.ID, capability, origin.ID, origin.InputID, secondAttempt.TurnID); err != nil {
		t.Fatal(err)
	}
	_, second, _, err := store.RecordAttemptOutcome(ctx, secondAttempt.ID, AttemptOutcomeInput{Status: OutcomeFailed, Classification: OutcomeFinal, Summary: "slow failure"})
	if err != nil || second == nil {
		t.Fatalf("record second Worker Result: result=%#v err=%v", second, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID); err != nil || !related {
		t.Fatalf("origin did not resolve its exact Results: related=%v err=%v", related, err)
	}
	if unrelated, err := store.SecretaryOriginHasResult(ctx, origin.ID, "stale-input"); err == nil && unrelated {
		t.Fatal("stale input identity inherited an origin Result")
	}

	entry, duplicate, err := store.RecordSecretaryReply(ctx, person.ID, capability, origin.ID, origin.InputID, "The independent answer is 42.")
	if err != nil || duplicate || entry.Kind != EntrySecretary {
		t.Fatalf("reply=%#v duplicate=%v err=%v", entry, duplicate, err)
	}
	replayed, duplicate, err := store.RecordSecretaryReply(ctx, person.ID, capability, origin.ID, origin.InputID, "The independent answer is 42.")
	if err != nil || !duplicate || replayed.ID != entry.ID {
		t.Fatalf("replay=%#v duplicate=%v err=%v", replayed, duplicate, err)
	}
	if _, _, err := store.RecordSecretaryReply(ctx, person.ID, capability, origin.ID, origin.InputID, "different replay body"); err == nil {
		t.Fatal("same reply identity accepted conflicting text")
	}
	if _, recorded, err := store.RecordAddressedSecretaryTextDelta(ctx, origin.ID, origin.InputID, "post-reply echo"); err != nil || recorded {
		t.Fatalf("unaddressed delta escaped after typed reply: recorded=%v err=%v", recorded, err)
	}
	if _, _, err := store.RecordSecretaryReply(ctx, person.ID, capability, origin.ID, "stale-input", "stale reply"); err == nil {
		t.Fatal("stale input identity was accepted")
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("reply/replay created Secretary turns: turns=%#v err=%v", turns, err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secretaryReplies, workerResults := 0, 0
	for _, candidate := range entries {
		if candidate.Kind == EntrySecretary {
			secretaryReplies++
		}
		if candidate.Kind == EntryWorkerResult {
			workerResults++
		}
	}
	if secretaryReplies != 1 || workerResults != 2 {
		t.Fatalf("reply replay duplicated a conversation entry: secretary=%d worker_results=%d", secretaryReplies, workerResults)
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, origin.ID, origin.InputID, SecretaryTurnSucceeded, "", "untyped echo", []string{"untyped delta"}); err != nil {
		t.Fatal(err)
	}
	entries, err = store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secretaryReplies = 0
	for _, candidate := range entries {
		if candidate.Kind == EntrySecretary {
			secretaryReplies++
		}
	}
	if secretaryReplies != 1 {
		t.Fatalf("completion Summary duplicated the typed reply: entries=%#v", entries)
	}
	turnEvents, err := store.SecretaryEvents(ctx, origin.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range turnEvents {
		if event.Kind == SecretaryTextDeltaEvent {
			t.Fatalf("buffered unaddressed delta duplicated the typed reply: %#v", event)
		}
	}
}

func TestPendingCommandOriginJoinsExactResultBeforeReceiptAndFailedDispatchDoesNotLink(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "test profile"}); err != nil {
		t.Fatal(err)
	}
	createOrigin := func(text string) SecretaryTurn {
		t.Helper()
		turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	createWorker := func(ref string) (Worker, Phase4Attempt) {
		t.Helper()
		worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: ref, Intent: "work", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "test"}, TurnSpec{Input: "work"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
			t.Fatal(err)
		}
		return worker, attempt
	}

	acceptedOrigin := createOrigin("dispatch worker")
	if _, _, err := store.RecordSecretaryAcknowledgement(ctx, person.ID, capability, acceptedOrigin.ID, acceptedOrigin.InputID, "Сейчас проверю"); err != nil {
		t.Fatal(err)
	}
	worker, previousAttempt := createWorker("accepted-worker")
	_, previousResult, _, err := store.RecordAttemptOutcome(ctx, previousAttempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "previous turn result"})
	if err != nil || previousResult == nil {
		t.Fatalf("record previous Worker Result=%#v err=%v", previousResult, err)
	}
	currentTurn, currentAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "follow up"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, currentAttempt.ID); err != nil {
		t.Fatal(err)
	}
	binding := SecretaryOriginIdentity{PersonID: person.ID, Capability: capability, SecretaryTurnID: acceptedOrigin.ID, InputID: acceptedOrigin.InputID}
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", currentAttempt.WorkerID, currentAttempt.ID, binding)
	if err != nil || duplicate {
		t.Fatalf("claim accepted command=%#v duplicate=%v err=%v", command, duplicate, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	replayedCommand, duplicate, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", currentAttempt.WorkerID, currentAttempt.ID, binding)
	if err != nil || !duplicate || replayedCommand.ID != command.ID {
		t.Fatalf("replayed pending command=%#v duplicate=%v err=%v", replayedCommand, duplicate, err)
	}
	command = replayedCommand
	if related, err := store.SecretaryOriginHasResult(ctx, acceptedOrigin.ID, acceptedOrigin.InputID); err != nil || related {
		t.Fatalf("pending command linked a prior Worker Turn Result: related=%v err=%v", related, err)
	}
	_, currentResult, _, err := store.RecordAttemptOutcome(ctx, currentAttempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "Result before command receipt"})
	if err != nil || currentResult == nil {
		t.Fatalf("record pre-receipt Result=%#v err=%v", currentResult, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, acceptedOrigin.ID, acceptedOrigin.InputID); err != nil || !related {
		t.Fatalf("exact Result was not joined before receipt commit: related=%v err=%v", related, err)
	}
	var linkedTurnID, linkedResultID string
	if err := store.db.QueryRowContext(ctx, `SELECT worker_turn_id, result_id FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?`, acceptedOrigin.ID, acceptedOrigin.InputID).Scan(&linkedTurnID, &linkedResultID); err != nil || linkedTurnID != currentTurn.ID || linkedResultID != currentResult.ID || linkedResultID == previousResult.ID {
		t.Fatalf("origin joined wrong Worker Turn/Result: turn=%q result=%q previous=%q err=%v", linkedTurnID, linkedResultID, previousResult.ID, err)
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, acceptedOrigin.ID, acceptedOrigin.InputID, SecretaryTurnSucceeded, "", "duplicate terminal Summary", nil); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Kind == EntrySecretary && entry.Body == "duplicate terminal Summary" {
			t.Fatalf("Summary escaped before command receipt replay: %#v", entries)
		}
	}
	if _, err := store.MarkWorkerCommandDelivered(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	var linkedCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_origin_results WHERE secretary_turn_id = ? AND input_id = ?`, acceptedOrigin.ID, acceptedOrigin.InputID).Scan(&linkedCount); err != nil || linkedCount != 1 {
		t.Fatalf("receipt replay duplicated or lost exact Result relation: count=%d err=%v", linkedCount, err)
	}

	failedOrigin := createOrigin("dispatch another worker")
	_, failedAttempt := createWorker("failed-worker")
	failedBinding := SecretaryOriginIdentity{PersonID: person.ID, Capability: capability, SecretaryTurnID: failedOrigin.ID, InputID: failedOrigin.InputID}
	if _, _, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", currentAttempt.WorkerID, currentAttempt.ID, failedBinding); !errors.Is(err, ErrInvalidSecretaryOrigin) {
		t.Fatalf("delivered command accepted a second Secretary origin: err=%v", err)
	}
	failedCommand, duplicate, err := store.ClaimWorkerCommand(ctx, "dispatch", "attempt", failedAttempt.WorkerID, failedAttempt.ID, failedBinding)
	if err != nil || duplicate {
		t.Fatalf("claim failed command=%#v duplicate=%v err=%v", failedCommand, duplicate, err)
	}
	if _, err := store.MarkWorkerCommandFailed(ctx, failedCommand.ID, "synthetic dispatch failure"); err != nil {
		t.Fatal(err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, failedOrigin.ID, failedOrigin.InputID); err != nil || related {
		t.Fatalf("failed dispatch created an accepted origin link: related=%v err=%v", related, err)
	}
}

func TestAddressedCompletionSuppressesOnlyOriginLinkedOutput(t *testing.T) {
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
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "test profile"}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "dispatch a task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, origin.ID); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "related-worker", Intent: "work", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic"}, TurnSpec{Input: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LinkSecretaryWorkerTurn(ctx, person.ID, capability, origin.ID, origin.InputID, attempt.TurnID); err != nil {
		t.Fatal(err)
	}
	if _, recorded, err := store.RecordAddressedSecretaryTextDelta(ctx, origin.ID, origin.InputID, "accepted before Result"); err != nil || !recorded {
		t.Fatalf("pre-Result stream delta was not preserved: recorded=%v err=%v", recorded, err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "canonical result"}); err != nil || result == nil {
		t.Fatalf("record related result: result=%#v err=%v", result, err)
	}
	if _, recorded, err := store.RecordAddressedSecretaryTextDelta(ctx, origin.ID, origin.InputID, "suppressed after Result"); err != nil || recorded {
		t.Fatalf("post-Result unaddressed delta escaped: recorded=%v err=%v", recorded, err)
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, origin.ID, origin.InputID, SecretaryTurnSucceeded, "", "same result narration", []string{"unaddressed stream echo"}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Kind == EntrySecretary {
			t.Fatalf("unaddressed Summary became a Conversation entry after linked Result: %#v", entry)
		}
	}
	events, err := store.SecretaryEvents(ctx, origin.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	streamDeltas := 0
	for _, event := range events {
		if event.Kind == SecretaryTextDeltaEvent {
			streamDeltas++
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err != nil || payload["text"] != "accepted before Result" {
				t.Fatalf("unexpected unaddressed ActivityText after Result: payload=%#v err=%v", payload, err)
			}
		}
	}
	if streamDeltas != 1 {
		t.Fatalf("v1 did not preserve exactly the accepted pre-Result delta: %#v", events)
	}

	// An unrelated Result cannot suppress a different user turn's legacy reply.
	next, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "answer a new question")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, next.ID, next.InputID, SecretaryTurnSucceeded, "", "ordinary answer", []string{"ordinary delta"}); err != nil {
		t.Fatal(err)
	}
	entries, err = store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secretaryReplies := 0
	for _, entry := range entries {
		if entry.Kind == EntrySecretary && entry.Body == "ordinary answer" {
			secretaryReplies++
		}
	}
	if secretaryReplies != 1 {
		t.Fatalf("unrelated origin Result suppressed a normal new-turn answer: %#v", entries)
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != 2 {
		t.Fatalf("Result/output handling created or lost a Secretary turn: turns=%#v err=%v", turns, err)
	}
}
