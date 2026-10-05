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

func TestApprovalIntentWinsExpiryAndConflictingRetryAfterTerminalResult(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		decision string
		state    core.ApprovalState
		conflict string
	}{
		{name: "approve", decision: "approve", state: core.ApprovalApproved, conflict: "deny"},
		{name: "deny", decision: "deny", state: core.ApprovalDenied, conflict: "approve"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			store, err := core.Open(ctx, filepath.Join(t.TempDir(), "approval-late.db"))
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
			nodeRef := core.NodeReference("approval-" + testCase.name)
			identity, err := node.EnrollNode(ctx, httpServer.Client(), httpServer.URL, "pair-token", nodeRef)
			if err != nil {
				t.Fatal(err)
			}
			instance := core.HarnessInstance{
				ID: core.HarnessInstanceID(string(nodeRef) + "/fx"), Node: nodeRef, Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady,
				Authentication: core.HarnessAuthentication{Authenticated: true},
			}
			inventory := core.HarnessInventorySnapshot{Node: nodeRef, Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
			auth, err := identity.Authenticator()
			if err != nil {
				t.Fatal(err)
			}
			connection, err := node.DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, node.Handshake{
				Node: nodeRef, ProtocolVersion: node.ProtocolVersion, Inventory: inventory, Nonce: "approval-late-" + testCase.name,
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
			worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
				WorkerRef: "approval-late-" + testCase.name, Intent: "wait for approval", ProjectID: "project",
				NodeID: string(nodeRef), HarnessInstanceID: string(instance.ID),
			}, core.TurnSpec{Input: "wait for approval"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
				t.Fatal(err)
			}
			expiresAt := time.Now().UTC().Add(120 * time.Millisecond)
			requestID := "permission-" + testCase.name
			activity := core.Activity{Metadata: core.ActivityMetadata{
				EventID: "approval-event-" + testCase.name, Node: nodeRef, HarnessInstanceID: instance.ID,
				WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC(),
			}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: requestID, Summary: "synthetic permission", ExpiresAt: &expiresAt}}
			if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
				t.Fatal(err)
			}
			service := WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: NodeRuntime{Manager: manager}}

			respondCtx, stopRespond := context.WithTimeout(ctx, 300*time.Millisecond)
			respondDone := make(chan error, 1)
			go func() {
				_, respondErr := service.RespondWorker(respondCtx, MessageWorkerRequest{WorkerRef: worker.WorkerRef, Text: testCase.decision, RequestID: requestID, ClientID: "owner"})
				respondDone <- respondErr
			}()
			command, err := connection.ReceiveCommand(ctx)
			if err != nil {
				stopRespond()
				t.Fatal(err)
			}
			if command.Kind != node.CommandRespondWorker || command.Metadata().TurnID != turn.ID || command.Metadata().AttemptID != attempt.ID || command.RespondWorker == nil || command.RespondWorker.Response != testCase.decision {
				stopRespond()
				t.Fatal("Node received a response other than the original approval decision")
			}
			if err := <-respondDone; !errors.Is(err, node.ErrCommandOutcomeUnknown) {
				stopRespond()
				t.Fatalf("approval ACK timeout was not reported as uncertain: %v", err)
			}
			stopRespond()

			approval, err := store.Approval(ctx, requestID)
			if err != nil || approval.State != core.ApprovalResolving || approval.ResolutionState != testCase.state || approval.ResolutionCommandID != command.Metadata().CommandID {
				t.Fatalf("approval intent was not durable before handoff: state=%q err=%v", approval.State, err)
			}
			// Model a Node which accepted the original command but has not delivered
			// its authenticated receipt to the server yet.
			if _, _, err := store.ExpireApproval(ctx, requestID, expiresAt.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			approval, err = store.Approval(ctx, requestID)
			if err != nil || approval.State != core.ApprovalResolving {
				t.Fatalf("expiry replaced an earlier durable decision intent: state=%q err=%v", approval.State, err)
			}
			_, canonical, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
				Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "canonical Result before late approval receipt",
			})
			if err != nil || canonical == nil {
				t.Fatalf("record terminal Result before receipt: err=%v", err)
			}

			_, conflictErr := service.RespondWorker(ctx, MessageWorkerRequest{WorkerRef: worker.WorkerRef, Text: testCase.conflict, RequestID: requestID, ClientID: "other-owner"})
			if !errors.Is(conflictErr, core.ErrApprovalResolutionConflict) {
				t.Fatalf("conflicting approval replay replaced first intent: %v", conflictErr)
			}
			sameCommand, found, findErr := store.FindWorkerCommand(ctx, "respond", "request:"+requestID, worker.ID, attempt.ID)
			if findErr != nil || !found || sameCommand.ID != command.Metadata().CommandID || sameCommand.State != core.WorkerCommandUncertain {
				t.Fatalf("conflicting replay changed the original durable command: found=%v state=%q err=%v", found, sameCommand.State, findErr)
			}

			if err := connection.SendCommandOutcome(ctx, node.CommandOutcome{
				CommandID: command.Metadata().CommandID, Kind: node.CommandRespondWorker, State: node.CommandAccepted,
				TurnID: turn.ID, AttemptID: attempt.ID,
			}); err != nil {
				t.Fatal(err)
			}
			waitForApprovalReceipt(t, ctx, func() bool {
				current, lookupErr := store.Approval(ctx, requestID)
				return lookupErr == nil && current.State == testCase.state
			})
			resolved, err := store.Approval(ctx, requestID)
			if err != nil || resolved.State != testCase.state || resolved.Response != testCase.decision || resolved.ResolvedBy != "owner" {
				t.Fatalf("late receipt did not finalize original approval intent: state=%q actor=%q err=%v", resolved.State, resolved.ResolvedBy, err)
			}
			final, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
			if err != nil || len(final.Results) != 1 || final.Results[0].ID != canonical.ID || final.Turns[0].State != core.TurnSucceeded || final.Worker.Status != core.WorkerIdle {
				t.Fatalf("late receipt changed terminal Worker state or Result: turns=%d results=%d worker=%q err=%v", len(final.Turns), len(final.Results), final.Worker.Status, err)
			}
		})
	}
}

func waitForApprovalReceipt(t *testing.T, ctx context.Context, predicate func() bool) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if predicate() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("approval receipt was not durably reconciled")
		case <-ticker.C:
		}
	}
}
