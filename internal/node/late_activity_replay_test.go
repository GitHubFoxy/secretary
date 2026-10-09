package node

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/beruseruko/secretary/internal/core"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type reviewLateSession struct {
	activity chan Activity
	result   chan Result
}

func (s *reviewLateSession) ID() string                                  { return "native-late" }
func (s *reviewLateSession) Activity() <-chan Activity                   { return s.activity }
func (s *reviewLateSession) Result() <-chan Result                       { return s.result }
func (s *reviewLateSession) Prompt(context.Context, string) error        { return nil }
func (s *reviewLateSession) Steer(context.Context, string) (bool, error) { return false, nil }
func (s *reviewLateSession) Queue(context.Context, string) error         { return nil }
func (s *reviewLateSession) Cancel(context.Context) error                { return nil }
func (s *reviewLateSession) Close() error {
	s.activity <- Activity{Kind: ActivityText, Text: "buffered native text"}
	close(s.activity)
	close(s.result)
	return nil
}

type reviewLateRuntime struct{}

func (reviewLateRuntime) Start(context.Context, StartRequest) (Session, error) {
	s := &reviewLateSession{activity: make(chan Activity, 1), result: make(chan Result, 1)}
	s.result <- Result{Status: "succeeded", Summary: "done"}
	return s, nil
}

type reviewLateWireHandler struct {
	sink func(context.Context, NodeEvent) error
}

func (h *reviewLateWireHandler) HandleNodeHandshake(_ context.Context, hs Handshake) (HandshakeAccepted, error) {
	return HandshakeAccepted{Node: hs.Node, ProtocolVersion: ProtocolVersion, ReplayFromSequence: hs.LastAcknowledgedSequence}, nil
}
func (h *reviewLateWireHandler) HandleNodeEvent(ctx context.Context, event NodeEvent) error {
	return h.sink(ctx, event)
}
func TestReviewLateNativeActivityDoesNotPoisonReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	serverStore, err := core.Open(ctx, t.TempDir()+"/server.db")
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	_, conversation, err := serverStore.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := serverStore.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "late-worker", Intent: "run", ProjectID: "project", NodeID: "macbook", HarnessInstanceID: "macbook/codex"}, core.TurnSpec{Input: "run"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	command := dispatchFixture("late-frame")
	md := &command.Dispatch.Metadata
	md.WorkerRef = worker.WorkerRef
	md.TurnID = turn.ID
	md.AttemptID = attempt.ID
	md.HarnessInstanceID = "macbook/codex"
	envelope := &command.Dispatch.Envelope
	envelope.WorkerRef = worker.WorkerRef
	envelope.TurnID = turn.ID
	envelope.AttemptID = attempt.ID
	envelope.Workspace = t.TempDir()
	envelope.HarnessInstance.ID = "macbook/codex"
	envelope.HarnessInstance.Kind = core.HarnessCodex
	envelope.Profile = workerTemplateFixture(envelope.HarnessInstance, "", "")
	outcome := core.AttemptOutcomeEnvelope{EventID: "attempt-outcome-" + attempt.ID, Node: "macbook", HarnessInstanceID: envelope.HarnessInstance.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done", OccurredAt: time.Now().UTC()}
	if _, err := local.QueueOutcome(outcome); err != nil {
		t.Fatal(err)
	}
	late := core.Activity{Metadata: core.ActivityMetadata{EventID: "late-text", Node: "macbook", HarnessInstanceID: envelope.HarnessInstance.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: 2, ObservedAt: time.Now().UTC()}, Kind: core.ActivityAssistantTextDelta, Text: "buffered native text"}
	if _, err := local.QueueActivity(late); err != nil {
		t.Fatal(err)
	}
	pending, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	var first, second NodeEvent
	_ = json.Unmarshal(pending[0].Payload, &first)
	_ = json.Unmarshal(pending[1].Payload, &second)
	if first.Outcome == nil || second.Activity == nil || second.Activity.Kind != core.ActivityAssistantTextDelta {
		t.Fatalf("unexpected ordering first=%#v second=%#v", first, second)
	}
	t.Logf("persisted terminal seq=%d then text delta seq=%d", pending[0].Sequence, pending[1].Sequence)
	var approvalCalls atomic.Int32
	h := &reviewLateWireHandler{sink: NewStoreEventSinkWithTrustedLocalApproval(serverStore, func(context.Context, string, core.NodeReference) error {
		approvalCalls.Add(1)
		return nil
	})}
	auth := NewAuthenticator([]byte(strings.Repeat("l", 32)))
	wire := httptest.NewServer(&ProtocolServer{Auth: auth, Handler: h})
	defer wire.Close()
	inventory := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{envelope.HarnessInstance}, ObservedAt: time.Now().UTC()}
	dial := func(nonce string) *ProtocolConnection {
		c, err := DialProtocol(ctx, "ws"+strings.TrimPrefix(wire.URL, "http"), "macbook", auth, Handshake{Node: "macbook", ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: nonce, LastAcknowledgedSequence: local.LastAcknowledgedSequence()})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := dial("first")
	if err = c.SendPendingEvent(ctx, pending[0]); err != nil {
		t.Fatal(err)
	}
	if err = local.AckThrough(pending[0].Sequence); err != nil {
		t.Fatal(err)
	}
	if err = c.SendPendingEvent(ctx, pending[1]); err != nil {
		t.Errorf("late terminal text closed protocol instead of ACK: %v", err)
	}
	_ = c.Close()

	c = dial("reconnect")
	defer c.Close()
	remaining, _ := local.PendingEvents()
	if len(remaining) != 1 {
		t.Fatalf("remaining=%d", len(remaining))
	}
	if err = c.SendPendingEvent(ctx, remaining[0]); err != nil {
		t.Fatalf("same late frame poisons reconnect again; ack=%d pending=%d: %v", local.LastAcknowledgedSequence(), len(remaining), err)
	}
	if err := local.AckThrough(remaining[0].Sequence); err != nil {
		t.Fatal(err)
	}
	remaining, _ = local.PendingEvents()
	if len(remaining) != 0 || local.LastAcknowledgedSequence() != 2 {
		t.Fatalf("replay did not advance: ack=%d pending=%d", local.LastAcknowledgedSequence(), len(remaining))
	}
	permission := late
	permission.Metadata.EventID = "late-permission"
	permission.Metadata.Sequence = 3
	permission.Kind, permission.Text = core.ActivityPermissionRequest, ""
	permission.Request = &core.ActivityRequest{RequestID: "late-approval", Summary: "late native permission"}
	queued, err := local.QueueActivity(permission)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SendPendingEvent(ctx, queued); err != nil {
		t.Fatalf("late permission did not ACK: %v", err)
	}
	if err := local.AckThrough(queued.Sequence); err != nil {
		t.Fatal(err)
	}
	if _, err := serverStore.Approval(ctx, "late-approval"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("late permission created Approval: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if approvalCalls.Load() != 0 {
		t.Fatal("late permission invoked trusted-local approval")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"node", "harness", "worker", "turn", "attempt"} {
		t.Run("wrong "+field, func(t *testing.T) {
			wrong := late
			wrong.Metadata.EventID = "wrong-" + field
			wrong.Metadata.Sequence = 4
			switch field {
			case "node":
				wrong.Metadata.Node = "other-node"
			case "harness":
				wrong.Metadata.HarnessInstanceID = "other-harness"
			case "worker":
				wrong.Metadata.WorkerRef = "other-worker"
			case "turn":
				wrong.Metadata.TurnID = "other-turn"
			case "attempt":
				wrong.Metadata.AttemptID = "unknown-attempt"
			}
			payload, err := json.Marshal(NodeEvent{EventID: wrong.Metadata.EventID, Node: "macbook", Kind: "activity", Sequence: 4, Activity: &wrong})
			if err != nil {
				t.Fatal(err)
			}
			connection := dial("mismatch-" + field)
			defer connection.Close()
			if err := connection.SendPendingEvent(ctx, PendingEvent{Sequence: 4, EventID: wrong.Metadata.EventID, Payload: payload}); err == nil {
				t.Fatal("wrong immutable binding received ACK")
			}
		})
	}

	state, err := serverStore.Phase4Attempt(ctx, attempt.ID)
	if err != nil || !state.State.Terminal() {
		t.Fatalf("terminal state=%s err=%v", state.State, err)
	}
}

func TestExecutionNodeStopsActivityAfterTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	local, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	command := dispatchFixture("late-frame")
	envelope := &command.Dispatch.Envelope
	envelope.Workspace = t.TempDir()
	envelope.HarnessInstance.Kind = core.HarnessCodex
	envelope.Profile = workerTemplateFixture(envelope.HarnessInstance, "", "")
	execution := NewExecutionNode("macbook", reviewLateRuntime{}, local)
	if outcome, err := execution.HandleCommand(ctx, command); err != nil || outcome.State != CommandAccepted {
		t.Fatalf("dispatch=%#v err=%v", outcome, err)
	}
	for {
		pending, err := local.PendingEvents()
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	time.Sleep(20 * time.Millisecond)
	pending, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("late native activity queued after terminal: events=%d", len(pending))
	}
	var event NodeEvent
	if err := json.Unmarshal(pending[0].Payload, &event); err != nil || event.Outcome == nil {
		t.Fatalf("terminal event=%#v err=%v", event, err)
	}
}
