package node

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/beruseruko/secretary/internal/core"
)

func TestRespondWorkerOutcomeErrorsDistinguishUnknownFromDenial(t *testing.T) {
	for _, code := range []string{"execution_state_unknown", "response_failed", "response_record_failed"} {
		if !uncertainCommandOutcome(CommandOutcome{State: CommandFailed, ErrorCode: code}) {
			t.Errorf("outcome %q was treated as authoritative denial", code)
		}
	}
	if !uncertainCommandOutcome(CommandOutcome{State: CommandInterrupted}) {
		t.Error("interrupted outcome was treated as authoritative denial")
	}
	if uncertainCommandOutcome(CommandOutcome{State: CommandFailed, ErrorCode: "runtime_does_not_accept_response"}) {
		t.Error("explicit pre-delivery rejection was treated as uncertain")
	}
	if !uncertainCommandOutcome(CommandOutcome{Kind: CommandSteering, State: CommandFailed, AcceptanceUnknown: true}) {
		t.Error("explicit unknown-acceptance marker was treated as a definitive refusal")
	}
}

func TestNodeCommandError_PreservesOutcome(t *testing.T) {
	outcome := CommandOutcome{
		CommandID:       "cmd-1",
		State:           CommandFailed,
		ErrorCode:       "usage_limit_exceeded",
		ErrorMessage:    "Usage limit reached",
		FailureCategory: core.FailureCategoryUsageLimit,
		ProviderCode:    "usage_limit_exceeded",
		Technical:       "acp rpc -32603: Internal error",
	}
	err := &NodeCommandError{Outcome: outcome}

	expectedMsg := "node command usage_limit_exceeded: Usage limit reached"
	if err.Error() != expectedMsg {
		t.Fatalf("expected error message %q, got %q", expectedMsg, err.Error())
	}

	var cmdErr *NodeCommandError
	if !errors.As(err, &cmdErr) {
		t.Fatal("expected errors.As to succeed for NodeCommandError")
	}
	if cmdErr.Outcome.FailureCategory != core.FailureCategoryUsageLimit {
		t.Fatalf("expected category %q, got %q", core.FailureCategoryUsageLimit, cmdErr.Outcome.FailureCategory)
	}
	if cmdErr.Outcome.ProviderCode != "usage_limit_exceeded" {
		t.Fatalf("expected provider code 'usage_limit_exceeded', got %q", cmdErr.Outcome.ProviderCode)
	}
	if cmdErr.Outcome.Technical != "acp rpc -32603: Internal error" {
		t.Fatalf("expected technical diagnostics preserved, got %q", cmdErr.Outcome.Technical)
	}
}

func TestNodeCommandErrorUnwrapsLocalCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := &NodeCommandError{Outcome: CommandOutcome{State: CommandFailed}, Err: cause}
		if !errors.Is(err, cause) {
			t.Errorf("NodeCommandError does not preserve %v", cause)
		}
	}
}

func TestSanitizeCommandOutcomeFailureMetadata(t *testing.T) {
	outcome := sanitizeCommandOutcome(CommandOutcome{
		FailureCategory: "sk-private-secret", ProviderCode: "sk-private-secret",
		ErrorCode: "sk-private-secret", ErrorMessage: "Bearer private-message-secret",
		Technical: strings.Repeat("界", 1100),
	})
	if outcome.FailureCategory != core.FailureCategoryInternalError || outcome.ProviderCode != "" {
		t.Fatalf("failure metadata was not allow-listed: %#v", outcome)
	}
	for name, value := range map[string]string{"error_code": outcome.ErrorCode, "error_message": outcome.ErrorMessage, "technical": outcome.Technical} {
		if strings.Contains(value, "private-secret") || strings.Contains(value, "private-message-secret") || utf8.RuneCountInString(value) > 1000 {
			t.Errorf("unsafe %s: %q", name, value)
		}
	}
}

func TestCompleteCommand_SteeringFailedPersistsOutcomeAtomicallyAndRecovers(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "node-store.json")
	store, err := OpenLocalStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	cmd := Command{
		Kind: CommandSteering,
		Steering: &SteeringCommand{
			Metadata: core.CommandMetadata{
				CommandID: "steer-cmd-1",
				Node:      "macbook",
				TurnID:    "turn-1",
				AttemptID: "attempt-1",
			},
			Text: "redirect output",
		},
	}
	if _, _, err := store.ClaimCommand(cmd); err != nil {
		t.Fatal(err)
	}

	outcome := CommandOutcome{
		CommandID:       "steer-cmd-1",
		Kind:            CommandSteering,
		State:           CommandFailed,
		TurnID:          "turn-1",
		AttemptID:       "attempt-1",
		ErrorCode:       "usage_limit_exceeded",
		ErrorMessage:    "Usage limit reached",
		FailureCategory: core.FailureCategoryUsageLimit,
		ProviderCode:    "usage_limit_exceeded",
		Technical:       "acp rpc -32603: Internal error",
	}

	rec, err := store.CompleteCommand("steer-cmd-1", outcome)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != CommandFailed {
		t.Fatalf("expected state CommandFailed, got %v", rec.State)
	}

	events, err := store.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 pending event in outbox, got %d", len(events))
	}
	if events[0].EventID != "command-outcome-steer-cmd-1" {
		t.Fatalf("expected eventID command-outcome-steer-cmd-1, got %q", events[0].EventID)
	}

	reopened, err := OpenLocalStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	reopenedEvents, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(reopenedEvents) != 1 || reopenedEvents[0].EventID != "command-outcome-steer-cmd-1" {
		t.Fatalf("reopened outbox mismatch: %#v", reopenedEvents)
	}

	reopened.mu.Lock()
	reopened.state.Outbox = nil
	if err := reopened.persistLocked(); err != nil {
		t.Fatal(err)
	}
	reopened.mu.Unlock()

	if err := reopened.RecoverRunning(ctx, nil); err != nil {
		t.Fatal(err)
	}
	recoveredEvents, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(recoveredEvents) != 1 || recoveredEvents[0].EventID != "command-outcome-steer-cmd-1" {
		t.Fatalf("expected recovered event in outbox after crash, got: %#v", recoveredEvents)
	}

	if err := reopened.AckThrough(recoveredEvents[0].Sequence); err != nil {
		t.Fatal(err)
	}
	clearedEvents, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(clearedEvents) != 0 {
		t.Fatalf("expected outbox cleared after ack, got %d events", len(clearedEvents))
	}

	if err := reopened.RecoverRunning(ctx, nil); err != nil {
		t.Fatal(err)
	}
	afterAckEvents, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAckEvents) != 0 {
		t.Fatalf("expected 0 events after ack and recovery, got %d", len(afterAckEvents))
	}
}
