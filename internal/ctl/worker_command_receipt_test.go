package ctl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestRespondTimeoutResultThenLateAcceptedReceiptDoesNotRetryOrReviveTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := node.NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	identity, err := node.EnrollNode(ctx, httpServer.Client(), httpServer.URL, "pair-token", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{
		ID: "macbook/fx", Node: "macbook", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true},
	}
	inventory := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
	auth, err := identity.Authenticator()
	if err != nil {
		t.Fatal(err)
	}
	connection, err := node.DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, node.Handshake{
		Node: identity.Node, ProtocolVersion: node.ProtocolVersion, Inventory: inventory, Nonce: "respond-late-receipt",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	secretary, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, secretary.ID, "answer the Worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, origin.ID); err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "late-ack-worker", Intent: "wait for owner input", ProjectID: "project", NodeID: "macbook", HarnessInstanceID: "macbook/fx",
	}, core.TurnSpec{Input: "wait for owner input"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability,
		Runtime: NodeRuntime{Manager: manager},
	}

	respondCtx, stopRespond := context.WithTimeout(ctx, 350*time.Millisecond)
	defer stopRespond()
	respondDone := make(chan error, 1)
	go func() {
		_, respondErr := service.RespondWorker(respondCtx, MessageWorkerRequest{
			WorkerRef: worker.WorkerRef, Text: "continue", RequestID: "owner-input",
			SecretaryTurnID: origin.ID, InputID: origin.InputID,
		})
		respondDone <- respondErr
	}()
	command, err := connection.ReceiveCommand(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if command.Kind != node.CommandRespondWorker || command.Metadata().TurnID != turn.ID || command.Metadata().AttemptID != attempt.ID {
		t.Fatalf("received wrong response command: %#v", command)
	}
	respondErr := <-respondDone
	if !errors.Is(respondErr, node.ErrCommandOutcomeUnknown) {
		currentRecord, currentFound, lookupErr := store.FindWorkerCommand(ctx, "respond", "request:owner-input", worker.ID, attempt.ID)
		t.Fatalf("Runtime.Respond timeout was not classified as uncertain: %T %#v; command=%#v found=%v lookupErr=%v", respondErr, respondErr, currentRecord, currentFound, lookupErr)
	}
	commandRecord, found, err := store.FindWorkerCommand(ctx, "respond", "request:owner-input", worker.ID, attempt.ID)
	if err != nil || !found || commandRecord.State != core.WorkerCommandUncertain {
		t.Fatalf("timed-out command was treated as denied: command=%#v found=%v err=%v", commandRecord, found, err)
	}

	_, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "Worker completed before receipt",
	})
	if err != nil || result == nil {
		t.Fatalf("record final Worker Result before retry/receipt: result=%#v err=%v", result, err)
	}
	if related, err := store.SecretaryOriginHasResult(ctx, origin.ID, origin.InputID); err != nil || !related {
		t.Fatalf("uncertain exact Result did not suppress duplicate Summary: related=%v err=%v", related, err)
	}

	accepted := node.CommandOutcome{
		CommandID: commandRecord.ID, Kind: node.CommandRespondWorker, State: node.CommandAccepted,
		TurnID: turn.ID, AttemptID: attempt.ID,
	}
	if err := connection.SendCommandOutcome(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	waitForReceipt(t, ctx, func() bool {
		current, exists, findErr := store.FindWorkerCommand(ctx, "respond", "request:owner-input", worker.ID, attempt.ID)
		return findErr == nil && exists && current.State == core.WorkerCommandDelivered
	})
	if err := connection.SendCommandOutcome(ctx, node.CommandOutcome{
		CommandID: commandRecord.ID, Kind: node.CommandRespondWorker, State: node.CommandFailed,
		TurnID: turn.ID, AttemptID: attempt.ID, ErrorCode: "stale_denial", ErrorMessage: "superseded by accepted receipt",
	}); err != nil {
		t.Fatal(err)
	}
	waitForReceipt(t, ctx, func() bool {
		status, statusErr := manager.Status(ctx, "macbook")
		if statusErr != nil || status.LastCommandOutcome == nil || status.LastCommandOutcome.ErrorCode != "stale_denial" {
			return false
		}
		current, exists, findErr := store.FindWorkerCommand(ctx, "respond", "request:owner-input", worker.ID, attempt.ID)
		return findErr == nil && exists && current.State == core.WorkerCommandDelivered
	})
	if current, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef); err != nil || current.CurrentAttempt() == nil || current.CurrentAttempt().State != core.AttemptSucceeded {
		t.Fatalf("late receipt revived or changed terminal Worker Turn: details=%#v err=%v", current, err)
	}
}

func waitForReceipt(t *testing.T, ctx context.Context, predicate func() bool) {
	t.Helper()
	deadline := time.NewTicker(5 * time.Millisecond)
	defer deadline.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("condition did not become true before timeout")
		case <-deadline.C:
		}
	}
}
