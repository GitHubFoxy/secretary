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

func TestLateAcceptedRespondOutcomeReconcilesAfterWaiterRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
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

	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secretary, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
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
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, secretary.ID, "respond to the Worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, origin.ID); err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "late-receipt-worker", Intent: "wait for a response", ProjectID: "project", NodeID: "macbook", HarnessInstanceID: "macbook/fx",
	}, core.TurnSpec{Input: "wait for a response"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	commandRecord, _, err := store.ClaimWorkerCommand(ctx, "respond", "request:approval-request", worker.ID, attempt.ID, core.SecretaryOriginIdentity{
		PersonID: person.ID, Capability: capability, SecretaryTurnID: origin.ID, InputID: origin.InputID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandUncertain(ctx, commandRecord.ID, "response acknowledgement timed out"); err != nil {
		t.Fatal(err)
	}

	inventory := daemonInventoryFixture("macbook")
	auth, err := identity.Authenticator()
	if err != nil {
		t.Fatal(err)
	}
	connection, err := DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{
		Node: identity.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: "late-receipt-nonce",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	command := Command{Kind: CommandRespondWorker, RespondWorker: &RespondWorkerCommand{
		Metadata: core.CommandMetadata{
			CommandID: commandRecord.ID, Node: "macbook", HarnessInstanceID: "macbook/fx", WorkerRef: worker.WorkerRef,
			TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC(),
		},
		RequestID: "approval-request", Response: "proceed",
	}}
	waitCtx, stopWait := context.WithTimeout(ctx, 60*time.Millisecond)
	defer stopWait()
	waitDone := make(chan error, 1)
	go func() { waitDone <- manager.SendCommandAndWait(waitCtx, "macbook", command) }()
	delivered, err := connection.ReceiveCommand(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if delivered.Metadata().CommandID != commandRecord.ID {
		t.Fatalf("delivered command=%#v", delivered)
	}
	if err := <-waitDone; !errors.Is(err, ErrCommandOutcomeUnknown) {
		t.Fatalf("response wait did not report an ambiguous handoff: %v", err)
	}

	// The authenticated outcome arrives only after SendCommandAndWait returned,
	// so no in-memory waiter remains. The production Core sink must still commit it.
	accepted := CommandOutcome{
		CommandID: commandRecord.ID, Kind: CommandRespondWorker, State: CommandAccepted,
		TurnID: turn.ID, AttemptID: attempt.ID,
	}
	if err := connection.SendCommandOutcome(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "late accepted receipt reconciliation", func() bool {
		current, found, findErr := store.FindWorkerCommand(ctx, "respond", "request:approval-request", worker.ID, attempt.ID)
		return findErr == nil && found && current.State == core.WorkerCommandDelivered
	})
	if err := connection.SendCommandOutcome(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "duplicate receipt remains idempotent", func() bool {
		current, found, findErr := store.FindWorkerCommand(ctx, "respond", "request:approval-request", worker.ID, attempt.ID)
		return findErr == nil && found && current.State == core.WorkerCommandDelivered
	})
}
