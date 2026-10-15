package core

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWorkerFollowUpDeliveredEventRequiresAcceptedDirectTurn(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, initialAttempt, err := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "initial task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, initialAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, initialAttempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	followUpCount := func() int {
		t.Helper()
		events, err := store.EventsAfterSeq(ctx, 0, 500)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, event := range events {
			if event.Kind == "worker.followup.delivered" {
				count++
			}
		}
		return count
	}
	commandFor := func(turn Turn, attempt Phase4Attempt) WorkerCommand {
		t.Helper()
		command, found, err := store.FindWorkerCommand(ctx, "dispatch", "attempt", worker.ID, attempt.ID)
		if err != nil || !found {
			t.Fatalf("dispatch command=%#v found=%v err=%v", command, found, err)
		}
		if turn.CurrentAttemptID != attempt.ID {
			t.Fatalf("Turn current attempt=%q, want %q", turn.CurrentAttemptID, attempt.ID)
		}
		return command
	}
	receiptFor := func(turn Turn, attempt Phase4Attempt, state WorkerCommandReceiptState) WorkerCommandReceipt {
		command := commandFor(turn, attempt)
		return WorkerCommandReceipt{CommandID: command.ID, Kind: command.Kind, TurnID: turn.ID, AttemptID: attempt.ID, State: state}
	}
	failedTurn, failedAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "do not show this failed delivery", IdempotencyKey: "failed-follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	denied := receiptFor(failedTurn, failedAttempt, WorkerCommandReceiptDenied)
	denied.ErrorCode = "dispatch_failed"
	denied.ErrorMessage = "runtime did not accept dispatch"
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, NodeReference(worker.NodeID), denied); err != nil {
		t.Fatal(err)
	}
	if followUpCount() != 0 {
		t.Fatal("failed dispatch produced a delivered follow-up event")
	}

	text := "Continue with the edge case."
	followUp, attempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: text, IdempotencyKey: "accepted-follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	if followUpCount() != 0 {
		t.Fatal("Turn creation produced a delivered follow-up event before Node acceptance")
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if followUpCount() != 1 {
		t.Fatal("accepted Worker start did not publish its follow-up")
	}
	accepted := receiptFor(followUp, attempt, WorkerCommandReceiptAccepted)
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, NodeReference(worker.NodeID), accepted); err != nil {
		t.Fatal(err)
	}
	events, err := store.EventsAfterSeq(ctx, 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	var delivered []Event
	for _, event := range events {
		if event.Kind == "worker.followup.delivered" {
			delivered = append(delivered, event)
		}
	}
	if len(delivered) != 1 || delivered[0].WorkerRef != worker.WorkerRef || delivered[0].CorrelationID != followUp.ID || delivered[0].AttemptID != attempt.ID {
		t.Fatalf("delivered follow-up events=%#v", delivered)
	}
	var payload map[string]string
	if err := json.Unmarshal(delivered[0].Payload, &payload); err != nil || payload["text"] != text {
		t.Fatalf("delivered payload=%s err=%v", delivered[0].Payload, err)
	}
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, NodeReference(worker.NodeID), accepted); err != nil {
		t.Fatal(err)
	}
	if followUpCount() != 1 {
		t.Fatal("duplicate accepted receipt duplicated the follow-up event")
	}

	queued, err := store.EnqueueWorkerMessage(ctx, worker.ID, "wait until idle", "queued-follow-up")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	queuedTurn, queuedAttempt, err := store.PromoteQueuedWorkerMessage(ctx, queued, "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	queuedReceipt := receiptFor(queuedTurn, queuedAttempt, WorkerCommandReceiptAccepted)
	if _, err := store.ReconcileWorkerCommandReceipt(ctx, NodeReference(worker.NodeID), queuedReceipt); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteQueuedWorkerMessage(ctx, queued.ID, "delivered", ""); err != nil {
		t.Fatal(err)
	}
	if followUpCount() != 1 {
		t.Fatal("queued message produced an ordinary follow-up event")
	}
}
