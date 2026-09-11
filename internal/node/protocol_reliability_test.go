package node

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestAuthenticatedWebSocketReplaysTypedNodeEventAndAcknowledgesSequence(t *testing.T) {
	auth := NewAuthenticator([]byte("secret"))
	received := make(chan NodeEvent, 1)
	handler := nodeProtocolTestHandler{events: received}
	server := &ProtocolServer{Auth: auth, Handler: handler}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	inventory := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{dispatchFixture("fixture").Dispatch.Envelope.HarnessInstance}, ObservedAt: time.Now().UTC()}
	handshake := Handshake{Node: "macbook", ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: "nonce"}
	connection, err := DialProtocol(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), "macbook", auth, handshake)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	outcome := outcomeFixture("event-1")
	outcome.Node = "macbook"
	outcome.HarnessInstanceID = "macbook/claude"
	pending, err := store.QueueOutcome(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.SendPendingEvent(context.Background(), pending); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-received:
		if event.Outcome == nil || event.Outcome.EventID != "event-1" {
			t.Fatalf("event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive event")
	}
}

type nodeProtocolTestHandler struct{ events chan NodeEvent }

func (h nodeProtocolTestHandler) HandleNodeHandshake(context.Context, Handshake) (HandshakeAccepted, error) {
	return HandshakeAccepted{ProtocolVersion: ProtocolVersion}, nil
}
func (h nodeProtocolTestHandler) HandleNodeEvent(_ context.Context, event NodeEvent) error {
	h.events <- event
	return nil
}

func TestAuthenticatedWebSocketDeliversTypedCommandWithCommandID(t *testing.T) {
	auth := NewAuthenticator([]byte("command-secret"))
	command := dispatchFixture("command-1")
	outcomes := make(chan CommandOutcome, 1)
	server := &ProtocolServer{Auth: auth, Handler: commandHandler{command: command, outcomes: outcomes}}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	fixture := command.Dispatch.Envelope.HarnessInstance
	handshake := Handshake{Node: "macbook", ProtocolVersion: ProtocolVersion, Inventory: core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{fixture}, ObservedAt: time.Now().UTC()}, Nonce: "nonce"}
	connection, err := DialProtocol(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), "macbook", auth, handshake)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("macbook", &countingRuntime{}, store)
	outbound := NewOutboundNode(execution, connection, store)
	if err := outbound.HandleNextCommand(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-outcomes:
		if outcome.CommandID != "command-1" || outcome.State != CommandAccepted {
			t.Fatalf("outcome=%#v", outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive command outcome")
	}
}

type commandHandler struct {
	command  Command
	outcomes chan CommandOutcome
}

func (h commandHandler) HandleNodeHandshake(_ context.Context, handshake Handshake) (HandshakeAccepted, error) {
	return HandshakeAccepted{Node: handshake.Node, ProtocolVersion: ProtocolVersion}, nil
}
func (h commandHandler) HandleNodeConnection(connection *ProtocolConnection) {
	go func() { _ = connection.SendCommand(context.Background(), h.command) }()
}
func (h commandHandler) HandleNodeCommandOutcome(_ context.Context, outcome CommandOutcome) error {
	if h.outcomes != nil {
		h.outcomes <- outcome
	}
	return nil
}

func TestProtocolRejectsWrongAuthentication(t *testing.T) {
	server := &ProtocolServer{Auth: NewAuthenticator([]byte("server-secret"))}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	fixture := dispatchFixture("auth").Dispatch.Envelope.HarnessInstance
	handshake := Handshake{Node: "macbook", ProtocolVersion: ProtocolVersion, Inventory: core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{fixture}, ObservedAt: time.Now().UTC()}, Nonce: "nonce"}
	if _, err := DialProtocol(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), "macbook", NewAuthenticator([]byte("wrong")), handshake); err == nil {
		t.Fatal("wrong authentication was accepted")
	}
}

func TestExecutionNodeClaimsBeforeDispatchAndDeduplicatesAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/node.json"
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &countingRuntime{}
	node := NewExecutionNode("macbook", runtime, store)
	command := dispatchFixture("dispatch-1")

	first, err := node.HandleCommand(ctx, command)
	if err != nil || first.State != CommandAccepted {
		t.Fatalf("first dispatch=%#v err=%v", first, err)
	}
	second, err := node.HandleCommand(ctx, command)
	if err != nil || second != first {
		t.Fatalf("duplicate dispatch=%#v first=%#v err=%v", second, first, err)
	}
	if runtime.starts != 1 {
		t.Fatalf("runtime started %d times", runtime.starts)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	mapping, ok := reopened.SessionMapping("attempt-1")
	if !ok || mapping.RuntimeSessionID != "native-worker-1" {
		t.Fatalf("mapping=%#v ok=%v", mapping, ok)
	}
	afterRestart := NewExecutionNode("macbook", &countingRuntime{}, reopened)
	stored, err := afterRestart.HandleCommand(ctx, command)
	if err != nil || stored != first {
		t.Fatalf("restart duplicate=%#v first=%#v err=%v", stored, first, err)
	}
}

func TestExecutionNodeUnknownRunningDispatchBecomesInterrupted(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/node.json"
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatchFixture("dispatch-running")
	if _, duplicate, err := store.ClaimCommand(command); err != nil || duplicate {
		t.Fatalf("claim duplicate=%v err=%v", duplicate, err)
	}
	if err := store.RecoverRunning(ctx, nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	record, err := reopened.Command(command.Metadata().CommandID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Outcome.State != CommandInterrupted || record.Outcome.ErrorCode != "execution_state_unknown" {
		t.Fatalf("recovered command=%#v", record)
	}
	pending, err := reopened.PendingEvents()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	var event NodeEvent
	if err := json.Unmarshal(pending[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Outcome == nil || event.Outcome.Status != core.OutcomeInterrupted {
		t.Fatalf("interruption event=%#v", event)
	}
}

func TestLocalOutboxReplaysUntilAuthenticatedAcknowledgement(t *testing.T) {
	path := t.TempDir() + "/node.json"
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	outcome := outcomeFixture("evt-1")
	queued, err := store.QueueOutcome(outcome)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Sequence != 1 {
		t.Fatalf("sequence=%d", queued.Sequence)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.PendingEvents()
	if err != nil || len(pending) != 1 || pending[0].EventID != "evt-1" {
		t.Fatalf("replayed=%#v err=%v", pending, err)
	}
	if err := reopened.AckThrough(queued.Sequence); err != nil {
		t.Fatal(err)
	}
	pending, err = reopened.PendingEvents()
	if err != nil || len(pending) != 0 {
		t.Fatalf("ack did not remove outbox: %#v err=%v", pending, err)
	}
	if got := reopened.LastAcknowledgedSequence(); got != queued.Sequence {
		t.Fatalf("ack boundary=%d", got)
	}
}

func TestProtocolEnvelopeAuthenticatesAndRejectsMalformedOrReplayedMessages(t *testing.T) {
	auth := NewAuthenticator([]byte("node-secret"))
	payload, err := json.Marshal(Heartbeat{Node: "macbook", Online: true})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := NewEnvelope(MessageHeartbeat, "macbook", 1, 0, payload, auth)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Verify(decoded); err != nil {
		t.Fatal(err)
	}
	tracker := SequenceTracker{}
	if err := tracker.Accept(decoded); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Accept(decoded); err == nil {
		t.Fatal("replayed sequence was accepted")
	}
	decoded.Signature = "bad"
	if err := auth.Verify(decoded); err == nil {
		t.Fatal("invalid signature was accepted")
	}
	if _, err := DecodeEnvelope([]byte(`{"type":"heartbeat"}`)); err == nil {
		t.Fatal("malformed envelope was accepted")
	}
}

func dispatchFixture(commandID string) Command {
	instance := core.HarnessInstance{
		ID: "macbook/claude", Node: "macbook", Kind: core.HarnessClaudeCode, Version: "1.2.3",
		Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}, Activity: []core.ActivityCapability{core.ActivitySessionStarted, core.ActivityAssistantTextDelta}},
	}
	return Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{
		Metadata: core.CommandMetadata{CommandID: commandID, Node: "macbook", HarnessInstanceID: instance.ID, WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1", IssuedAt: time.Now()},
		Envelope: WorkerEnvelope{WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1", OriginalUserIntent: "inspect", HarnessInstance: instance, Workspace: tWorkspace},
	}}
}

const tWorkspace = "/tmp/secretary-test-workspace"

func outcomeFixture(eventID string) core.AttemptOutcomeEnvelope {
	return core.AttemptOutcomeEnvelope{EventID: eventID, Node: "home-server", HarnessInstanceID: "home-server/fx", WorkerRef: "worker-1", TurnID: "turn-1", AttemptID: "attempt-1", Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done", OccurredAt: time.Now()}
}

type countingRuntime struct{ starts int }

func (r *countingRuntime) Start(_ context.Context, request StartRequest) (Session, error) {
	r.starts++
	return &countingSession{id: "native-" + request.WorkerRef, activity: make(chan Activity), result: make(chan Result)}, nil
}

type countingSession struct {
	id       string
	activity chan Activity
	result   chan Result
}

func (s *countingSession) ID() string                                  { return s.id }
func (s *countingSession) Prompt(context.Context, string) error        { return nil }
func (s *countingSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (s *countingSession) Cancel(context.Context) error                { return nil }
func (s *countingSession) Activity() <-chan Activity                   { return s.activity }
func (s *countingSession) Result() <-chan Result                       { return s.result }
func (s *countingSession) Close() error                                { return nil }
