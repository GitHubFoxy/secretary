package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestServerProtocolRefusesNodeEventWithoutSink(t *testing.T) {
	manager := &ServerManager{}
	handler := &serverProtocolHandler{manager: manager, expected: "macbook"}
	event := NodeEvent{EventID: "evt-1", Node: "macbook", Kind: "activity"}
	if err := handler.HandleNodeEvent(context.Background(), event); err == nil {
		t.Fatal("Node event was accepted without a server event sink; protocol would ACK and lose the durable outbox entry")
	}

	accepted := false
	manager.SetEventSink(func(context.Context, NodeEvent) error {
		accepted = true
		return nil
	})
	if err := handler.HandleNodeEvent(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Fatal("configured event sink did not receive Node event")
	}
}

func TestTrustedLocalApprovalHandoffDoesNotBlockProtocolReadLoop(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		state      CommandState
		wrongKind  bool
		wantCommit bool
	}{
		{name: "accepted", state: CommandAccepted, wantCommit: true},
		{name: "failed", state: CommandFailed, wantCommit: false},
		{name: "accepted wrong kind is ignored before valid outcome", state: CommandAccepted, wrongKind: true, wantCommit: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			store, err := core.Open(ctx, filepath.Join(t.TempDir(), "trusted-local.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			_, conversation, err := store.CreatePersonWithConversation(ctx)
			if err != nil {
				t.Fatal(err)
			}
			spec := core.WorkerSpec{WorkerRef: "trusted-local-worker", Title: "trusted local", Intent: "run", ProjectID: "project", NodeID: "macbook", HarnessInstanceID: "macbook/fx"}
			worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, spec, core.TurnSpec{Input: "run"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
				t.Fatal(err)
			}

			manager, err := NewServerManager(ctx, store, "trusted-pair", "trusted-admin")
			if err != nil {
				t.Fatal(err)
			}
			const requestID = "trusted-local-request"
			const commandID = "trusted-local-command"
			handoffDone := make(chan error, 1)
			manager.SetEventSink(NewStoreEventSinkWithTrustedLocalApproval(store, func(applyCtx context.Context, request string, nodeRef core.NodeReference) error {
				command := Command{Kind: CommandRespondWorker, RespondWorker: &RespondWorkerCommand{
					Metadata:  core.CommandMetadata{CommandID: commandID, Node: nodeRef, HarnessInstanceID: "macbook/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC()},
					RequestID: request, Response: "approved",
				}}
				if err := manager.SendCommandAndWait(applyCtx, nodeRef, command); err != nil {
					if testCase.wrongKind {
						handoffDone <- err
					}
					return err
				}
				resolved, _, err := store.CommitApprovalResolution(applyCtx, request, core.ApprovalApproved, "trusted-local-policy", "auto_approved")
				if err != nil {
					if testCase.wrongKind {
						handoffDone <- err
					}
					return err
				}
				_, err = store.RecordEventWithMetadata(applyCtx, core.EventInput{Kind: "approval.auto_approved", AggregateType: "approval", AggregateID: resolved.ID, Source: "policy", CorrelationID: resolved.TurnID, AttemptID: resolved.AttemptID, Payload: map[string]any{"request_id": resolved.RequestID, "node_id": resolved.NodeID, "policy": "trusted_local_explicit"}})
				if testCase.wrongKind {
					handoffDone <- err
				}
				return err
			}))
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
			mux.Handle("/v1/nodes", manager)
			mux.Handle("/v1/nodes/", manager)
			httpServer := httptest.NewServer(mux)
			defer httpServer.Close()
			identity, err := EnrollNode(ctx, httpServer.Client(), httpServer.URL, "trusted-pair", "macbook")
			if err != nil {
				t.Fatal(err)
			}
			auth, err := identity.Authenticator()
			if err != nil {
				t.Fatal(err)
			}
			instance := core.HarnessInstance{ID: "macbook/fx", Node: "macbook", Kind: core.HarnessFX, Version: "1.0.0", Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady}
			connection, err := DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{Node: identity.Node, ProtocolVersion: ProtocolVersion, Inventory: core.HarnessInventorySnapshot{Node: identity.Node, Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}, Nonce: "trusted-local-nonce"})
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()

			activity := core.Activity{Metadata: core.ActivityMetadata{EventID: "trusted-local-activity", Node: "macbook", HarnessInstanceID: "macbook/fx", WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 1, ObservedAt: time.Now().UTC()}, Kind: core.ActivityPermissionRequest, Request: &core.ActivityRequest{RequestID: requestID, Summary: "run shell"}}
			eventPayload, err := json.Marshal(NodeEvent{EventID: activity.Metadata.EventID, Node: "macbook", Kind: "activity", Sequence: 1, Activity: &activity})
			if err != nil {
				t.Fatal(err)
			}
			if err := sendPendingEventWithoutWaiting(ctx, connection, PendingEvent{Sequence: 1, EventID: activity.Metadata.EventID, Payload: eventPayload}); err != nil {
				t.Fatal(err)
			}

			var command Command
			var acknowledged bool
			for command.Kind == "" || !acknowledged {
				data, err := connection.read(ctx)
				if err != nil {
					t.Fatalf("same connection did not make progress after event: %v", err)
				}
				envelope, err := DecodeEnvelope(data)
				if err != nil {
					t.Fatal(err)
				}
				switch envelope.Type {
				case MessageEventAck:
					acknowledged = envelope.Ack == 1
				case MessageCommandRespond:
					command, err = decodeCommandEnvelope(envelope)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if command.Metadata().CommandID != commandID {
				t.Fatalf("command=%#v", command)
			}
			if testCase.wrongKind {
				if err := connection.SendCommandOutcome(ctx, CommandOutcome{CommandID: commandID, Kind: CommandDispatch, State: CommandAccepted}); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-handoffDone:
					t.Fatalf("accepted outcome with wrong kind finalized respond_worker handoff: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				approval, err := store.Approval(ctx, requestID)
				if err != nil {
					t.Fatal(err)
				}
				if approval.State == core.ApprovalApproved {
					t.Fatalf("accepted outcome with wrong kind resolved approval: %#v", approval)
				}
			}
			if err := connection.SendCommandOutcome(ctx, CommandOutcome{CommandID: commandID, Kind: CommandRespondWorker, State: testCase.state, ErrorCode: "simulated_failure", ErrorMessage: "simulated Node rejection"}); err != nil {
				t.Fatal(err)
			}

			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				approval, approvalErr := store.Approval(ctx, requestID)
				if approvalErr == nil && ((approval.State == core.ApprovalApproved) == testCase.wantCommit) {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			approval, err := store.Approval(ctx, requestID)
			if err != nil {
				t.Fatal(err)
			}
			if (approval.State == core.ApprovalApproved) != testCase.wantCommit {
				t.Fatalf("approval=%#v, want committed=%v", approval, testCase.wantCommit)
			}
		})
	}
}

func TestHeartbeatTelemetryIsAvailableToServerStatusAndResolver(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "heartbeat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "heartbeat-pair", "heartbeat-admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "heartbeat-node"); err != nil {
		t.Fatal(err)
	}
	inventory := daemonInventoryFixture("heartbeat-node")
	if err := store.MarkNodeConnected(ctx, "heartbeat-node", inventory); err != nil {
		t.Fatal(err)
	}
	handler := &serverProtocolHandler{manager: manager, expected: "heartbeat-node"}
	if err := handler.HandleNodeHeartbeat(ctx, Heartbeat{
		Node: "heartbeat-node", Online: true, Capacity: 3,
		ActiveAttempts:       []ActiveAttempt{{WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt"}},
		LastProcessedCommand: "command-42", Inventory: inventory,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(ctx, "heartbeat-node")
	if err != nil {
		t.Fatal(err)
	}
	if status.Capacity != 3 || status.LastProcessedCommand != "command-42" || len(status.ActiveAttempts) != 1 || status.ActiveAttempts[0].AttemptID != "attempt" {
		t.Fatalf("server heartbeat status=%#v", status)
	}
}

func TestNodeBecomesOnlineOnlyAfterAcceptedProtocolConnection(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManager(ctx, store, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "macbook"); err != nil {
		t.Fatal(err)
	}

	handler := &serverProtocolHandler{manager: manager, expected: "macbook"}
	handshake := Handshake{
		Node: "macbook", ProtocolVersion: ProtocolVersion,
		Inventory: daemonInventoryFixture("macbook"),
		Nonce:     "nonce", NonceSignature: "signature",
	}
	if _, err := handler.HandleNodeHandshake(ctx, handshake); err != nil {
		t.Fatal(err)
	}
	record, err := store.NodeRecord(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if record.Online {
		t.Fatal("handshake validation marked Node online before the socket was accepted")
	}

	connection := &ProtocolConnection{node: "macbook"}
	handler.HandleNodeConnection(connection)
	record, err = store.NodeRecord(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Online {
		t.Fatal("accepted protocol connection did not mark Node online")
	}
	manager.unregisterConnection("macbook", connection)
}
