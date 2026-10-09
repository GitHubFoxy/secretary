package node

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type reviewReceiptHandler struct {
	connections chan *ProtocolConnection
	receipts    chan CommandOutcome
	heartbeats  chan struct{}
}

func (h *reviewReceiptHandler) HandleNodeHandshake(_ context.Context, hs Handshake) (HandshakeAccepted, error) {
	return HandshakeAccepted{Node: hs.Node, ProtocolVersion: ProtocolVersion}, nil
}
func (h *reviewReceiptHandler) HandleNodeConnection(c *ProtocolConnection) { h.connections <- c }
func (h *reviewReceiptHandler) HandleNodeCommandOutcome(_ context.Context, o CommandOutcome) error {
	h.receipts <- o
	return nil
}
func (h *reviewReceiptHandler) HandleNodeHeartbeat(context.Context, Heartbeat) error {
	select {
	case h.heartbeats <- struct{}{}:
	default:
	}
	return nil
}

type reviewReceiptFailRuntime struct {
	disconnected atomic.Pointer[ProtocolConnection]
	calls        atomic.Int32
}

func (r *reviewReceiptFailRuntime) Start(context.Context, StartRequest) (Session, error) {
	r.calls.Add(1)
	_ = r.disconnected.Load().Close()
	return nil, errors.New("native authorization rejected")
}
func (r *reviewReceiptFailRuntime) Resume(ctx context.Context, request StartRequest, _ string) (Session, error) {
	return r.Start(ctx, request)
}

func TestReviewFailedDispatchReceiptSurvivesReconnect(t *testing.T) {
	for _, resume := range []bool{false, true} {
		name := "Dispatch"
		if resume {
			name = "Resume"
		}
		t.Run(name, func(t *testing.T) { testFailedReceiptSurvivesReconnect(t, resume) })
	}
}

func testFailedReceiptSurvivesReconnect(t *testing.T, resume bool) {

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	secret := []byte(strings.Repeat("r", 32))
	h := &reviewReceiptHandler{connections: make(chan *ProtocolConnection, 3), receipts: make(chan CommandOutcome, 3), heartbeats: make(chan struct{}, 10)}
	server := httptest.NewServer(&ProtocolServer{Auth: NewAuthenticator(secret), Handler: h})
	defer server.Close()
	st, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rt := &reviewReceiptFailRuntime{}
	d := Daemon{Identity: NodeIdentity{Node: "macbook", Credential: base64.RawURLEncoding.EncodeToString(secret), ConnectURL: "ws" + strings.TrimPrefix(server.URL, "http")}, Store: st, Runtime: rt, Inventory: daemonStaticInventory{snapshot: daemonInventoryFixture("macbook")}, HeartbeatInterval: 20 * time.Millisecond, InventoryInterval: time.Hour, OutboxPollInterval: 5 * time.Millisecond, ReconnectMin: 5 * time.Millisecond, ReconnectMax: 5 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	defer func() { cancel(); <-done }()
	var first *ProtocolConnection
	select {
	case first = <-h.connections:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	rt.disconnected.Store(first)
	command := daemonDispatchFixture("macbook", "lost-failure")
	command.Dispatch.Envelope.Workspace = t.TempDir()
	if resume {
		checkpoint := *command.Dispatch
		checkpoint.Metadata.CommandID = "saved-dispatch"
		prior := Command{Kind: CommandDispatch, Dispatch: &checkpoint}
		if _, _, err := st.ClaimCommand(prior); err != nil {
			t.Fatal(err)
		}
		if _, err := st.CompleteCommand(checkpoint.Metadata.CommandID, CommandOutcome{CommandID: checkpoint.Metadata.CommandID, Kind: CommandDispatch, State: CommandAccepted}); err != nil {
			t.Fatal(err)
		}
		envelope := command.Dispatch.Envelope
		if err := st.SaveSessionMapping(LocalSessionMapping{WorkerRef: envelope.WorkerRef, TurnID: envelope.TurnID, AttemptID: envelope.AttemptID, HarnessInstanceID: envelope.HarnessInstance.ID, Workspace: envelope.Workspace, RuntimeSessionID: "saved-native-session"}); err != nil {
			t.Fatal(err)
		}
		command = Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: command.Dispatch.Metadata, Envelope: envelope}}
	}
	if err = first.SendCommand(ctx, command); err != nil {
		t.Fatal(err)
	}
	var second *ProtocolConnection
	select {
	case second = <-h.connections:
	case <-ctx.Done():
		t.Fatal("did not reconnect", ctx.Err())
	}
	select {
	case <-h.heartbeats:
	case <-ctx.Done():
		t.Fatal("reconnect not live", ctx.Err())
	}
	var receipt CommandOutcome
	select {
	case receipt = <-h.receipts:
		metadata := command.Metadata()
		if receipt.State != CommandFailed || receipt.Kind != command.Kind || receipt.CommandID != metadata.CommandID || receipt.TurnID != metadata.TurnID || receipt.AttemptID != metadata.AttemptID || receipt.ErrorMessage == "" {
			t.Fatalf("receipt=%#v", receipt)
		}
	case <-time.After(150 * time.Millisecond):
		record, err := st.Command("lost-failure")
		pending, _ := st.PendingEvents()
		t.Fatalf("failed receipt not replayed after live reconnect: stored=%s error=%v pendingEvents=%d nativeCalls=%d", record.State, err, len(pending), rt.calls.Load())
	}
	for {
		pending, err := st.PendingEvents()
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("receipt was not acknowledged", ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	var third *ProtocolConnection
	select {
	case third = <-h.connections:
	case <-ctx.Done():
		t.Fatal("did not reconnect after ACK", ctx.Err())
	}
	select {
	case unexpected := <-h.receipts:
		t.Fatalf("acknowledged receipt replayed: %#v", unexpected)
	case <-time.After(30 * time.Millisecond):
	}
	if err := third.SendCommand(ctx, command); err != nil {
		t.Fatal(err)
	}
	select {
	case repeated := <-h.receipts:
		if repeated != receipt {
			t.Fatalf("duplicate command changed receipt: %#v want %#v", repeated, receipt)
		}
	case <-ctx.Done():
		t.Fatal("duplicate command receipt missing", ctx.Err())
	}
	if rt.calls.Load() != 1 {
		t.Fatalf("native execution repeated: calls=%d", rt.calls.Load())
	}
}
