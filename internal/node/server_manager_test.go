package node

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestSendCommandAndWait_PreservesNodeCommandError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	manager.SetCommandOutcomeSink(NewStoreCommandOutcomeSink(store))

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	identity, err := EnrollNode(ctx, httpServer.Client(), httpServer.URL, "pair-token", "macbook")
	if err != nil {
		t.Fatal(err)
	}

	auth, err := identity.Authenticator()
	if err != nil {
		t.Fatal(err)
	}
	inventory := daemonInventoryFixture("macbook")
	connection, err := DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{
		Node: identity.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: "steer-test-nonce",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef:         "worker-steer",
		Title:             "Steer test",
		Intent:            "Test steering failure propagation",
		ProjectID:         "project-1",
		NodeID:            "macbook",
		HarnessInstanceID: "macbook/claude",
	}, core.TurnSpec{Input: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.ClaimWorkerCommand(ctx, "steering", "steering:cmd-1", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}

	command := Command{
		Kind: CommandSteering,
		Steering: &SteeringCommand{
			Metadata: core.CommandMetadata{
				CommandID: claimed.ID,
				Node:      "macbook",
				WorkerRef: worker.WorkerRef,
				TurnID:    turn.ID,
				AttemptID: attempt.ID,
				IssuedAt:  time.Now().UTC(),
			},
			Text: "focus on backend tests",
		},
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- manager.SendCommandAndWait(ctx, "macbook", command)
	}()

	delivered, err := connection.ReceiveCommand(ctx)
	if err != nil {
		t.Fatalf("ReceiveCommand failed: %v", err)
	}
	if delivered.Metadata().CommandID != claimed.ID {
		t.Fatalf("delivered command ID: want %q, got %q", claimed.ID, delivered.Metadata().CommandID)
	}

	outcome := CommandOutcome{
		CommandID:       claimed.ID,
		Kind:            CommandSteering,
		TurnID:          turn.ID,
		AttemptID:       attempt.ID,
		State:           CommandFailed,
		ErrorCode:       "usage_limit_exceeded",
		ErrorMessage:    "Лимит использования исчерпан",
		FailureCategory: core.FailureCategoryUsageLimit,
		ProviderCode:    "usage_limit_exceeded",
		Technical:       "acp rpc -32603: Internal error",
	}
	if err := connection.SendCommandOutcome(ctx, outcome); err != nil {
		t.Fatalf("SendCommandOutcome failed: %v", err)
	}

	select {
	case err := <-waitDone:
		if err == nil {
			t.Fatal("expected SendCommandAndWait to fail, got nil")
		}
		var cmdErr *NodeCommandError
		if !errors.As(err, &cmdErr) {
			t.Fatalf("expected *NodeCommandError via errors.As, got %T: %v", err, err)
		}
		if cmdErr.Outcome.FailureCategory != core.FailureCategoryUsageLimit {
			t.Fatalf("expected FailureCategory %q, got %q", core.FailureCategoryUsageLimit, cmdErr.Outcome.FailureCategory)
		}
		if cmdErr.Outcome.ProviderCode != "usage_limit_exceeded" {
			t.Fatalf("expected ProviderCode 'usage_limit_exceeded', got %q", cmdErr.Outcome.ProviderCode)
		}
		if cmdErr.Outcome.Technical != "acp rpc -32603: Internal error" {
			t.Fatalf("expected Technical 'acp rpc -32603: Internal error', got %q", cmdErr.Outcome.Technical)
		}
		if cmdErr.Outcome.ErrorMessage != "Лимит использования исчерпан" {
			t.Fatalf("expected ErrorMessage 'Лимит использования исчерпан', got %q", cmdErr.Outcome.ErrorMessage)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SendCommandAndWait to return")
	}

	unknownCommand, _, err := store.ClaimWorkerCommand(ctx, "steering", "steering:cmd-unknown", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	unknown := command
	unknown.Steering.Metadata.CommandID = unknownCommand.ID
	waitUnknown := make(chan error, 1)
	go func() { waitUnknown <- manager.SendCommandAndWait(ctx, "macbook", unknown) }()
	if _, err := connection.ReceiveCommand(ctx); err != nil {
		t.Fatalf("ReceiveCommand for unknown acceptance: %v", err)
	}
	if err := connection.SendCommandOutcome(ctx, CommandOutcome{
		CommandID: unknownCommand.ID, Kind: CommandSteering, State: CommandFailed,
		TurnID: turn.ID, AttemptID: attempt.ID, ErrorCode: "steering_failed",
		ErrorMessage: "native steering timed out", AcceptanceUnknown: true,
	}); err != nil {
		t.Fatalf("SendCommandOutcome for unknown acceptance: %v", err)
	}
	select {
	case err := <-waitUnknown:
		if !errors.Is(err, ErrCommandOutcomeUnknown) {
			t.Fatalf("unknown acceptance became a definitive refusal: %v", err)
		}
		var cmdErr *NodeCommandError
		if !errors.As(err, &cmdErr) || !cmdErr.Outcome.AcceptanceUnknown {
			t.Fatalf("unknown marker lost from typed outcome: %#v err=%v", cmdErr, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for unknown command outcome")
	}
	stored, found, err := store.FindWorkerCommand(ctx, "steering", "steering:cmd-unknown", worker.ID, attempt.ID)
	if err != nil || !found || stored.State != core.WorkerCommandUncertain {
		t.Fatalf("remote unknown acceptance was not reconciled: command=%#v found=%v err=%v", stored, found, err)
	}
}
