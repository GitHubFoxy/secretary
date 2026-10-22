package node

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestClaudeFailedToolCanonicalResultReplayAndBinding(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "claude-outcome.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "claude-outcome", Intent: "run one command", NodeID: "node-a", HarnessInstanceID: "node-a/claude",
	}, core.TurnSpec{Input: "run the command once"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	outcome := core.AttemptOutcomeEnvelope{
		EventID: "attempt-outcome-" + attempt.ID,
		Node: "node-a", HarnessInstanceID: "node-a/claude",
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		Status: core.OutcomeFailed, Classification: core.OutcomeFinal,
		Summary: "Задача не выполнена", FailureCode: "tool_execution_failed",
		ErrorCode: "tool_execution_failed", Diagnostics: "Claude Bash tool failed with exit code 7",
		OccurredAt: time.Now().UTC(),
	}
	sink := NewStoreEventSink(store)
	event := NodeEvent{Kind: "attempt.outcome", Outcome: &outcome}
	wrong := outcome
	wrong.AttemptID = "wrong-attempt"
	if err := sink(ctx, NodeEvent{Kind: "attempt.outcome", Outcome: &wrong}); !errors.Is(err, core.ErrInvalidTransition) {
		t.Fatalf("wrong Attempt accepted: %v", err)
	}
	wrong = outcome
	wrong.WorkerRef = "different-worker"
	if err := sink(ctx, NodeEvent{Kind: "attempt.outcome", Outcome: &wrong}); !errors.Is(err, core.ErrInvalidTransition) {
		t.Fatalf("wrong Worker accepted: %v", err)
	}
	wrong = outcome
	wrong.TurnID = "different-turn"
	if err := sink(ctx, NodeEvent{Kind: "attempt.outcome", Outcome: &wrong}); !errors.Is(err, core.ErrInvalidTransition) {
		t.Fatalf("wrong Turn accepted: %v", err)
	}
	wrong = outcome
	wrong.HarnessInstanceID = "node-b/claude"
	if err := sink(ctx, NodeEvent{Kind: "attempt.outcome", Outcome: &wrong}); !errors.Is(err, core.ErrInvalidTransition) {
		t.Fatalf("wrong harness accepted: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := sink(ctx, event); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Results) != 1 || details.CurrentAttempt().State != core.AttemptFailed {
		t.Fatalf("canonical terminal state: %#v", details)
	}
	if len(details.Outcomes) != 1 || details.Outcomes[0].Diagnostics != "Claude Bash tool failed with exit code 7" { t.Fatalf("canonical diagnostics: %#v", details.Outcomes) }
	result := details.Results[0]
	if result.Status != core.OutcomeFailed || result.FailureCode != "tool_execution_failed" {
		t.Fatalf("canonical result: %#v", result)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult {
			count++
			if entry.ResultID != result.ID {
				t.Fatalf("wrong result identity: %#v", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("canonical Result projection count=%d", count)
	}
}
