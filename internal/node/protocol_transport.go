package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/coder/websocket"
)

type ProtocolHandler interface{}
type HandshakeHandler interface {
	HandleNodeHandshake(context.Context, Handshake) (HandshakeAccepted, error)
}
type HeartbeatHandler interface {
	HandleNodeHeartbeat(context.Context, Heartbeat) error
}
type InventoryHandler interface {
	HandleNodeInventory(context.Context, core.HarnessInventorySnapshot) error
}
type EventHandler interface {
	HandleNodeEvent(context.Context, NodeEvent) error
}
type CommandOutcomeHandler interface {
	HandleNodeCommandOutcome(context.Context, CommandOutcome) error
}
type ConnectionHandler interface{ HandleNodeConnection(*ProtocolConnection) }

type ProtocolServer struct {
	Auth          Authenticator
	Handler       ProtocolHandler
	AcceptOptions *websocket.AcceptOptions
	mu            sync.Mutex
	connections   map[core.NodeReference]*ProtocolConnection
}

func (s *ProtocolServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, s.AcceptOptions)
	if err != nil {
		return
	}
	connection := &ProtocolConnection{conn: conn, auth: s.Auth, server: true, writes: &sync.Mutex{}, node: "", nextSequence: 0}
	if err := connection.acceptHandshake(r.Context(), s.Handler); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	s.mu.Lock()
	if s.connections == nil {
		s.connections = map[core.NodeReference]*ProtocolConnection{}
	}
	s.connections[connection.node] = connection
	s.mu.Unlock()
	if callback, ok := s.Handler.(ConnectionHandler); ok {
		callback.HandleNodeConnection(connection)
	}
	defer func() {
		s.mu.Lock()
		delete(s.connections, connection.node)
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()
	connection.readLoop(r.Context(), s.Handler)
}

func (s *ProtocolServer) Connection(node core.NodeReference) (*ProtocolConnection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	connection, ok := s.connections[node]
	return connection, ok
}

type ProtocolConnection struct {
	conn         *websocket.Conn
	auth         Authenticator
	server       bool
	node         core.NodeReference
	writes       *sync.Mutex
	nextSequence uint64
	inbound      SequenceTracker
	outbound     SequenceTracker
}

func (c *ProtocolConnection) Node() core.NodeReference { return c.node }
func (c *ProtocolConnection) Close() error             { return c.conn.Close(websocket.StatusNormalClosure, "") }

func (c *ProtocolConnection) acceptHandshake(ctx context.Context, handler ProtocolHandler) error {
	data, err := c.read(ctx)
	if err != nil {
		return err
	}
	envelope, err := DecodeEnvelope(data)
	if err != nil {
		return err
	}
	if err := c.auth.Verify(envelope); err != nil {
		return err
	}
	if envelope.Type != MessageHandshake {
		return errors.New("node protocol: handshake is required first")
	}
	var handshake Handshake
	if err := decodePayload(envelope.Payload, &handshake); err != nil {
		return err
	}
	if err := handshake.Validate(); err != nil {
		return err
	}
	if err := c.auth.VerifyNonce(handshake.Node, handshake.Nonce, handshake.NonceSignature); err != nil {
		return err
	}
	if envelope.Node != handshake.Node {
		return errors.New("node protocol: handshake identity mismatch")
	}
	c.node = handshake.Node
	c.inbound.SetLast(handshake.LastAcknowledgedSequence)
	accepted := HandshakeAccepted{Node: c.node, ProtocolVersion: ProtocolVersion, ReplayFromSequence: handshake.LastAcknowledgedSequence}
	if callback, ok := handler.(HandshakeHandler); ok {
		accepted, err = callback.HandleNodeHandshake(ctx, handshake)
		if err != nil {
			return err
		}
	}
	if accepted.Node == "" {
		accepted.Node = c.node
	}
	if accepted.Node != c.node || accepted.ProtocolVersion != ProtocolVersion {
		return errors.New("node protocol: invalid handshake acceptance")
	}
	payload, err := json.Marshal(accepted)
	if err != nil {
		return err
	}
	response, err := NewEnvelope(MessageHandshakeAccepted, c.node, 0, handshake.LastAcknowledgedSequence, payload, c.auth)
	if err != nil {
		return err
	}
	return c.write(ctx, response)
}

func (c *ProtocolConnection) readLoop(ctx context.Context, handler ProtocolHandler) {
	for {
		data, err := c.read(ctx)
		if err != nil {
			return
		}
		envelope, err := DecodeEnvelope(data)
		if err != nil {
			_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
			return
		}
		if err := c.auth.Verify(envelope); err != nil {
			_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
			return
		}
		if envelope.Node != c.node {
			_ = c.conn.Close(websocket.StatusPolicyViolation, "node identity changed")
			return
		}
		if envelope.Type != MessageHeartbeat && envelope.Type != MessageInventory && envelope.Type != MessageActivity && envelope.Type != MessageAttemptOutcome && envelope.Type != MessageCommandOutcome {
			_ = c.conn.Close(websocket.StatusPolicyViolation, "unexpected node message")
			return
		}
		if envelope.Type != MessageCommandOutcome {
			if err := c.inbound.Accept(envelope); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
		}
		switch envelope.Type {
		case MessageHeartbeat:
			var heartbeat Heartbeat
			if err := decodePayload(envelope.Payload, &heartbeat); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if heartbeat.Node != c.node || heartbeat.Inventory.Node != c.node {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "heartbeat node mismatch")
				return
			}
			if err := heartbeat.Inventory.Validate(); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if handler != nil {
				if callback, ok := handler.(HeartbeatHandler); ok {
					if err := callback.HandleNodeHeartbeat(ctx, heartbeat); err != nil {
						return
					}
				}
			}
		case MessageInventory:
			var inventory core.HarnessInventorySnapshot
			if err := decodePayload(envelope.Payload, &inventory); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if inventory.Node != c.node {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "inventory node mismatch")
				return
			}
			if err := inventory.Validate(); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if handler != nil {
				if callback, ok := handler.(InventoryHandler); ok {
					if err := callback.HandleNodeInventory(ctx, inventory); err != nil {
						return
					}
				}
			}
		case MessageCommandOutcome:
			var outcome CommandOutcome
			if err := decodePayload(envelope.Payload, &outcome); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if handler != nil {
				if callback, ok := handler.(CommandOutcomeHandler); ok {
					if err := callback.HandleNodeCommandOutcome(ctx, outcome); err != nil {
						return
					}
				}
			}
		case MessageActivity, MessageAttemptOutcome:
			var event NodeEvent
			if err := decodePayload(envelope.Payload, &event); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if event.Sequence == 0 {
				event.Sequence = envelope.Sequence
			}
			if event.Sequence != envelope.Sequence {
				_ = c.conn.Close(websocket.StatusPolicyViolation, "event sequence mismatch")
				return
			}
			if err := event.Validate(); err != nil {
				_ = c.conn.Close(websocket.StatusPolicyViolation, err.Error())
				return
			}
			if handler != nil {
				if callback, ok := handler.(EventHandler); ok {
					if err := callback.HandleNodeEvent(ctx, event); err != nil {
						return
					}
				}
			}
			_ = c.writeAck(ctx, envelope.Sequence)
		}
	}
}

func (c *ProtocolConnection) SendCommand(ctx context.Context, command Command) error {
	if !c.server {
		return errors.New("node protocol: only server sends commands")
	}
	if err := command.Validate(c.node); err != nil {
		return err
	}
	kind := MessageCommandDispatch
	switch command.Kind {
	case CommandCancel:
		kind = MessageCommandCancel
	case CommandSteering:
		kind = MessageCommandSteering
	case CommandResume:
		kind = MessageCommandResume
	case CommandRespondWorker:
		kind = MessageCommandRespond
	}
	var payload any
	switch command.Kind {
	case CommandDispatch:
		payload = command.Dispatch
	case CommandCancel:
		payload = command.Cancel
	case CommandSteering:
		payload = command.Steering
	case CommandResume:
		payload = command.Resume
	case CommandRespondWorker:
		payload = command.RespondWorker
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Sequence allocation must share the same mutex as envelope creation and
	// socket write. Otherwise concurrent SendCommand calls can race and put a
	// lower sequence on the wire after a higher one.
	c.writes.Lock()
	defer c.writes.Unlock()
	c.nextSequence++
	envelope, err := NewEnvelope(kind, c.node, c.nextSequence, 0, encoded, c.auth)
	if err != nil {
		return err
	}
	return c.writeLocked(ctx, envelope)
}

func (c *ProtocolConnection) writeAck(ctx context.Context, sequence uint64) error {
	payload, err := json.Marshal(struct {
		Sequence uint64 `json:"sequence"`
	}{sequence})
	if err != nil {
		return err
	}
	envelope, err := NewEnvelope(MessageEventAck, c.node, 0, sequence, payload, c.auth)
	if err != nil {
		return err
	}
	return c.write(ctx, envelope)
}

func (c *ProtocolConnection) read(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.Read(ctx)
	return data, err
}
func (c *ProtocolConnection) write(ctx context.Context, envelope Envelope) error {
	c.writes.Lock()
	defer c.writes.Unlock()
	return c.writeLocked(ctx, envelope)
}

func (c *ProtocolConnection) writeLocked(ctx context.Context, envelope Envelope) error {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, encoded)
}

func DialProtocol(ctx context.Context, url string, node core.NodeReference, auth Authenticator, handshake Handshake) (*ProtocolConnection, error) {
	if handshake.Node == "" {
		handshake.Node = node
	}
	if handshake.NonceSignature == "" {
		handshake.NonceSignature = auth.SignNonce(handshake.Node, handshake.Nonce)
	}
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	client := &ProtocolConnection{conn: conn, auth: auth, server: false, node: node, writes: &sync.Mutex{}, nextSequence: 1}
	payload, err := json.Marshal(handshake)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
		return nil, err
	}
	envelope, err := NewEnvelope(MessageHandshake, node, 0, handshake.LastAcknowledgedSequence, payload, auth)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
		return nil, err
	}
	if err := client.write(ctx, envelope); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
		return nil, err
	}
	data, err := client.read(ctx)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "")
		return nil, err
	}
	response, err := DecodeEnvelope(data)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "")
		return nil, err
	}
	if err := auth.Verify(response); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "")
		return nil, err
	}
	if response.Type != MessageHandshakeAccepted {
		_ = conn.Close(websocket.StatusPolicyViolation, "")
		return nil, fmt.Errorf("node protocol: expected handshake acceptance")
	}
	return client, nil
}

func (c *ProtocolConnection) ReceiveCommand(ctx context.Context) (Command, error) {
	if c.server {
		return Command{}, errors.New("node protocol: only Node receives commands")
	}
	data, err := c.read(ctx)
	if err != nil {
		return Command{}, err
	}
	envelope, err := DecodeEnvelope(data)
	if err != nil {
		return Command{}, err
	}
	if err := c.auth.Verify(envelope); err != nil {
		return Command{}, err
	}
	if envelope.Node != c.node {
		return Command{}, errors.New("node protocol: command node mismatch")
	}
	if err := c.outbound.Accept(envelope); err != nil {
		return Command{}, err
	}
	command, err := decodeCommandEnvelope(envelope)
	if err != nil {
		return Command{}, err
	}
	if err := command.Validate(c.node); err != nil {
		return Command{}, err
	}
	return command, nil
}

func (c *ProtocolConnection) SendCommandOutcome(ctx context.Context, outcome CommandOutcome) error {
	if c.server {
		return errors.New("node protocol: only Node sends command outcomes")
	}
	if outcome.CommandID == "" || outcome.Kind == "" || outcome.State == CommandProcessing {
		return errors.New("node protocol: incomplete command outcome")
	}
	payload, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	envelope, err := NewEnvelope(MessageCommandOutcome, c.node, 0, 0, payload, c.auth)
	if err != nil {
		return err
	}
	return c.write(ctx, envelope)
}

func decodeCommandEnvelope(envelope Envelope) (Command, error) {
	var command Command
	switch envelope.Type {
	case MessageCommandDispatch:
		var payload DispatchCommand
		if err := decodePayload(envelope.Payload, &payload); err != nil {
			return Command{}, err
		}
		command = Command{Kind: CommandDispatch, Dispatch: &payload}
	case MessageCommandCancel:
		var payload CancelCommand
		if err := decodePayload(envelope.Payload, &payload); err != nil {
			return Command{}, err
		}
		command = Command{Kind: CommandCancel, Cancel: &payload}
	case MessageCommandSteering:
		var payload SteeringCommand
		if err := decodePayload(envelope.Payload, &payload); err != nil {
			return Command{}, err
		}
		command = Command{Kind: CommandSteering, Steering: &payload}
	case MessageCommandResume:
		var payload ResumeCommand
		if err := decodePayload(envelope.Payload, &payload); err != nil {
			return Command{}, err
		}
		command = Command{Kind: CommandResume, Resume: &payload}
	case MessageCommandRespond:
		var payload RespondWorkerCommand
		if err := decodePayload(envelope.Payload, &payload); err != nil {
			return Command{}, err
		}
		command = Command{Kind: CommandRespondWorker, RespondWorker: &payload}
	default:
		return Command{}, fmt.Errorf("node protocol: %q is not a command", envelope.Type)
	}
	return command, nil
}

func (c *ProtocolConnection) SendHeartbeat(ctx context.Context, heartbeat Heartbeat, sequence uint64) error {
	if c.server {
		return errors.New("node protocol: only Node sends heartbeat")
	}
	if heartbeat.Node != c.node || heartbeat.Inventory.Node != c.node {
		return errors.New("node protocol: heartbeat node mismatch")
	}
	if err := heartbeat.Inventory.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	envelope, err := NewEnvelope(MessageHeartbeat, c.node, sequence, 0, payload, c.auth)
	if err != nil {
		return err
	}
	return c.write(ctx, envelope)
}

func (c *ProtocolConnection) SendInventory(ctx context.Context, inventory core.HarnessInventorySnapshot, sequence uint64) error {
	if c.server {
		return errors.New("node protocol: only Node sends inventory")
	}
	if inventory.Node != c.node {
		return errors.New("node protocol: inventory node mismatch")
	}
	if err := inventory.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	envelope, err := NewEnvelope(MessageInventory, c.node, sequence, 0, payload, c.auth)
	if err != nil {
		return err
	}
	return c.write(ctx, envelope)
}

func (c *ProtocolConnection) SendPendingEvent(ctx context.Context, pending PendingEvent) error {
	var event NodeEvent
	if err := json.Unmarshal(pending.Payload, &event); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	kind := MessageActivity
	if event.Outcome != nil {
		kind = MessageAttemptOutcome
	}
	envelope, err := NewEnvelope(kind, c.node, pending.Sequence, 0, pending.Payload, c.auth)
	if err != nil {
		return err
	}
	if err := c.write(ctx, envelope); err != nil {
		return err
	}
	data, err := c.read(ctx)
	if err != nil {
		return err
	}
	ack, err := DecodeEnvelope(data)
	if err != nil {
		return err
	}
	if err := c.auth.Verify(ack); err != nil {
		return err
	}
	if ack.Type != MessageEventAck || ack.Ack != pending.Sequence {
		return fmt.Errorf("node protocol: missing acknowledgement for sequence %d", pending.Sequence)
	}
	return nil
}

func decodePayload(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("node protocol: decode payload: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("node protocol: trailing payload data")
		}
		return fmt.Errorf("node protocol: trailing payload data: %w", err)
	}
	return nil
}
