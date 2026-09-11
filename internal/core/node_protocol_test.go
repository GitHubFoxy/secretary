package core

import (
	"context"
	"encoding/json"
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
