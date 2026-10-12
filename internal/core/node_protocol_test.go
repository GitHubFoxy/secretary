package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNodeTerminalOutcomeIsBoundAndIdempotentWithoutNativeSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID = "macbook"
	spec.HarnessInstanceID = "macbook/claude"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	terminal := AttemptOutcomeEnvelope{EventID: "node-event-1", Node: "macbook", HarnessInstanceID: "macbook/claude", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done", OccurredAt: time.Now().UTC()}
	first, result, duplicate, err := store.RecordNodeAttemptOutcome(ctx, terminal)
	if err != nil || duplicate || result == nil {
		t.Fatalf("first outcome=%#v result=%#v duplicate=%v err=%v", first, result, duplicate, err)
	}
	second, secondResult, duplicate, err := store.RecordNodeAttemptOutcome(ctx, terminal)
	if err != nil || !duplicate || second.ID != first.ID || secondResult.ID != result.ID {
		t.Fatalf("duplicate outcome=%#v result=%#v duplicate=%v err=%v", second, secondResult, duplicate, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "runtime_session") {
		t.Fatalf("native session leaked: %s", encoded)
	}
}

func TestNodeTerminalFailureIsSanitizedBeforePersistenceAndConversation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	spec := phase4WorkerSpec()
	spec.WorkerRef = "worker-terminal-failure"
	spec.NodeID = "macbook"
	spec.HarnessInstanceID = "macbook/claude"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	terminal := AttemptOutcomeEnvelope{
		EventID: "node-event-terminal-failure", Node: "macbook", HarnessInstanceID: "macbook/claude",
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		Status: OutcomeFailed, Classification: OutcomeFinal,
		Summary:      "Authentication failed: Authorization: Bearer terminal-summary-secret",
		ErrorCode:    "upstream rejected sk-terminal-code-secret",
		ErrorMessage: "Provider returned Bearer terminal-error-secret",
		Diagnostics:  `Authorization: Digest username="receipt-owner", response="terminal-digest-secret", nonce="terminal-nonce-secret" (status 401)`,
		FailureCode:  "invalid_api_key", FailureCategory: FailureCategoryAuthRequired, OccurredAt: time.Now().UTC(),
	}
	_, result, duplicate, err := store.RecordNodeAttemptOutcome(ctx, terminal)
	if err != nil || duplicate || result == nil {
		t.Fatalf("terminal outcome result=%#v duplicate=%v err=%v", result, duplicate, err)
	}

	successSpec := phase4WorkerSpec()
	successSpec.WorkerRef = "worker-terminal-success"
	successSpec.NodeID = "macbook"
	successSpec.HarnessInstanceID = "macbook/claude"
	successWorker, successTurn, successAttempt, err := store.CreateWorker(ctx, conversation.ID, successSpec, TurnSpec{Input: "echo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, successAttempt.ID); err != nil {
		t.Fatal(err)
	}
	const successSummary = "Assistant output: Bearer ordinary output"
	_, successResult, _, err := store.RecordNodeAttemptOutcome(ctx, AttemptOutcomeEnvelope{
		EventID: "node-event-terminal-success", Node: "macbook", HarnessInstanceID: "macbook/claude",
		WorkerRef: successWorker.WorkerRef, TurnID: successTurn.ID, AttemptID: successAttempt.ID,
		Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: successSummary, OccurredAt: time.Now().UTC(),
	})
	if err != nil || successResult == nil || successResult.Summary != successSummary {
		t.Fatalf("successful summary was changed: result=%#v err=%v", successResult, err)
	}

	var rawOutcome, rawResult, rawConversation string
	if err := store.db.QueryRowContext(ctx, `SELECT error_code || ' ' || error_message || ' ' || diagnostics FROM phase4_attempt_outcomes WHERE attempt_id = ?`, attempt.ID).Scan(&rawOutcome); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT summary || ' ' || failure_details FROM phase4_results WHERE attempt_id = ?`, attempt.ID).Scan(&rawResult); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT body FROM conversation_entries WHERE result_id = ?`, result.ID).Scan(&rawConversation); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"attempt outcome": rawOutcome, "result": rawResult, "conversation": rawConversation} {
		for _, secret := range []string{"terminal-summary-secret", "terminal-code-secret", "terminal-error-secret", "terminal-digest-secret", "terminal-nonce-secret", "receipt-owner"} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s persisted %q: %s", name, secret, text)
			}
		}
	}
	if !strings.Contains(rawConversation, "Authentication failed") {
		t.Fatalf("safe failure explanation was discarded: %q", rawConversation)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	reopened, err := store.Phase4Result(ctx, turn.ID)
	if err != nil || reopened.Failure == nil {
		t.Fatalf("reopened failure=%#v err=%v", reopened, err)
	}
	if reopened.Failure.Category != FailureCategoryAuthRequired || reopened.Failure.ProviderCode != "invalid_api_key" || strings.Contains(reopened.Failure.Technical, "terminal-digest-secret") {
		t.Fatalf("reopened failure metadata=%#v", reopened.Failure)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reopenedConversation string
	for _, entry := range entries {
		if entry.ResultID == result.ID {
			reopenedConversation = entry.Body
		}
	}
	if reopenedConversation != rawConversation || !strings.Contains(reopenedConversation, "[redacted]") {
		t.Fatalf("reopened conversation body=%q raw=%q", reopenedConversation, rawConversation)
	}
	reopenedSuccess, err := store.Phase4Result(ctx, successTurn.ID)
	if err != nil || reopenedSuccess.Summary != successSummary {
		t.Fatalf("reopened successful output changed: result=%#v err=%v", reopenedSuccess, err)
	}
}

func TestNodeActivityReplayIsIdempotentByEventID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID = "macbook"
	spec.HarnessInstanceID = "macbook/claude"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "activity"})
	if err != nil {
		t.Fatal(err)
	}
	fixture := macbookClaudeFixture()
	activity := Activity{Metadata: ActivityMetadata{EventID: "node-activity-1", Node: "macbook", HarnessInstanceID: fixture.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: ActivityAssistantTextDelta, Text: "working"}
	first, err := store.RecordNodeActivity(ctx, fixture, activity)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RecordNodeActivity(ctx, fixture, activity)
	if err != nil || second.ID != first.ID || second.Seq != first.Seq {
		t.Fatalf("replayed activity first=%#v second=%#v err=%v", first, second, err)
	}
}

func TestNodeTerminalOutcomeRejectsWrongImmutableBinding(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	spec := phase4WorkerSpec()
	spec.NodeID = "macbook"
	spec.HarnessInstanceID = "macbook/claude"
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, TurnSpec{Input: "run"})
	if err != nil {
		t.Fatal(err)
	}
	wrong := AttemptOutcomeEnvelope{EventID: "node-event-wrong", Node: "home-server", HarnessInstanceID: "home-server/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "wrong", OccurredAt: time.Now().UTC()}
	if _, _, _, err := store.RecordNodeAttemptOutcome(ctx, wrong); err == nil {
		t.Fatal("wrong Node binding was accepted")
	}
}
