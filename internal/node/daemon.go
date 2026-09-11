package node

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type Daemon struct {
	Identity  NodeIdentity
	Store     *LocalStore
	Runtime   Runtime
	Inventory InventorySource

	Capacity           int
	HeartbeatInterval  time.Duration
	InventoryInterval  time.Duration
	OutboxPollInterval time.Duration
	ReconnectMin       time.Duration
	ReconnectMax       time.Duration
}

type serverMessage struct {
	Command *Command
	Ack     uint64
}

func (d *Daemon) Run(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}
	execution := NewExecutionNode(d.Identity.Node, d.Runtime, d.Store)
	if err := execution.Restore(ctx); err != nil {
		return fmt.Errorf("node: restore local execution state: %w", err)
	}

	backoff := d.reconnectMin()
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := d.runConnection(ctx, execution)
		if ctx.Err() != nil {
			return nil
		}
		_ = err // reconnect is the recovery path for all transport failures.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
		backoff *= 2
		if backoff > d.reconnectMax() {
			backoff = d.reconnectMax()
		}
	}
}

func (d *Daemon) runConnection(ctx context.Context, execution *ExecutionNode) error {
	inventory, err := d.Inventory.Discover(ctx)
	if err != nil {
		return fmt.Errorf("node: discover harness inventory: %w", err)
	}
	if err := inventory.Validate(); err != nil {
		return fmt.Errorf("node: invalid harness inventory: %w", err)
	}
	if inventory.Node != d.Identity.Node {
		return errors.New("node: inventory belongs to another Node")
	}
	auth, err := d.Identity.Authenticator()
	if err != nil {
		return err
	}
	nonce, err := randomNonce()
	if err != nil {
		return err
	}
	handshake := Handshake{
		Node: d.Identity.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory,
		Nonce: nonce, LastAcknowledgedSequence: d.Store.LastAcknowledgedSequence(),
	}
	connection, err := DialProtocol(ctx, d.Identity.ConnectURL, d.Identity.Node, auth, handshake)
	if err != nil {
		return fmt.Errorf("node: connect: %w", err)
	}
	defer connection.Close()
	return d.serveConnection(ctx, execution, connection, inventory)
}

func (d *Daemon) serveConnection(ctx context.Context, execution *ExecutionNode, connection *ProtocolConnection, inventory core.HarnessInventorySnapshot) error {
	messages := make(chan serverMessage, 16)
	readErrors := make(chan error, 1)
	go func() {
		for {
			message, err := receiveServerMessage(ctx, connection)
			if err != nil {
				select {
				case readErrors <- err:
				default:
				}
				return
			}
			select {
			case messages <- message:
			case <-ctx.Done():
				return
			}
		}
	}()

	sentThrough := d.Store.LastAcknowledgedSequence()
	if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
		return err
	}

	heartbeats := time.NewTicker(d.heartbeatInterval())
	inventories := time.NewTicker(d.inventoryInterval())
	outbox := time.NewTicker(d.outboxPollInterval())
	defer heartbeats.Stop()
	defer inventories.Stop()
	defer outbox.Stop()
	lastProcessedCommand := ""

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErrors:
			return err
		case message := <-messages:
			if message.Ack != 0 {
				if err := d.Store.AckThrough(message.Ack); err != nil {
					return err
				}
				continue
			}
			if message.Command == nil {
				continue
			}
			command := *message.Command
			outcome, err := execution.HandleCommand(ctx, command)
			if err != nil {
				return err
			}
			if err := connection.SendCommandOutcome(ctx, outcome); err != nil {
				return err
			}
			lastProcessedCommand = command.Metadata().CommandID
			if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
				return err
			}
		case <-outbox.C:
			if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
				return err
			}
		case <-heartbeats.C:
			if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
				return err
			}
			sequence, err := d.reserveControlSequence(ctx, connection, &sentThrough)
			if err != nil {
				return err
			}
			heartbeat := Heartbeat{
				Node: d.Identity.Node, Online: true, Capacity: d.capacity(), Inventory: inventory,
				ActiveAttempts: execution.ActiveAttempts(), LastProcessedCommand: lastProcessedCommand,
			}
			if err := connection.SendHeartbeat(ctx, heartbeat, sequence); err != nil {
				return err
			}
			sentThrough = sequence
		case <-inventories.C:
			refreshed, err := d.Inventory.Discover(ctx)
			if err != nil {
				continue
			}
			if err := refreshed.Validate(); err != nil || refreshed.Node != d.Identity.Node {
				continue
			}
			if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
				return err
			}
			sequence, err := d.reserveControlSequence(ctx, connection, &sentThrough)
			if err != nil {
				return err
			}
			if err := connection.SendInventory(ctx, refreshed, sequence); err != nil {
				return err
			}
			sentThrough = sequence
			inventory = refreshed
		}
	}
}

func (d *Daemon) flushOutbox(ctx context.Context, connection *ProtocolConnection, sentThrough *uint64) error {
	pending, err := d.Store.PendingEvents()
	if err != nil {
		return err
	}
	for _, event := range pending {
		if event.Sequence <= *sentThrough {
			continue
		}
		if err := sendPendingEventWithoutWaiting(ctx, connection, event); err != nil {
			return err
		}
		*sentThrough = event.Sequence
	}
	return nil
}

func (d *Daemon) reserveControlSequence(ctx context.Context, connection *ProtocolConnection, sentThrough *uint64) (uint64, error) {
	for {
		sequence, err := d.Store.ReserveControlSequence(*sentThrough)
		if err == nil {
			return sequence, nil
		}
		if !errors.Is(err, ErrUnsentNodeEvents) {
			return 0, err
		}
		if err := d.flushOutbox(ctx, connection, sentThrough); err != nil {
			return 0, err
		}
	}
}

func receiveServerMessage(ctx context.Context, connection *ProtocolConnection) (serverMessage, error) {
	data, err := connection.read(ctx)
	if err != nil {
		return serverMessage{}, err
	}
	envelope, err := DecodeEnvelope(data)
	if err != nil {
		return serverMessage{}, err
	}
	if err := connection.auth.Verify(envelope); err != nil {
		return serverMessage{}, err
	}
	if envelope.Node != connection.node {
		return serverMessage{}, errors.New("node protocol: server message node mismatch")
	}
	if envelope.Type == MessageEventAck {
		if envelope.Ack == 0 {
			return serverMessage{}, errors.New("node protocol: invalid event acknowledgement")
		}
		return serverMessage{Ack: envelope.Ack}, nil
	}
	if err := connection.outbound.Accept(envelope); err != nil {
		return serverMessage{}, err
	}
	command, err := decodeCommandEnvelope(envelope)
	if err != nil {
		return serverMessage{}, err
	}
	if err := command.Validate(connection.node); err != nil {
		return serverMessage{}, err
	}
	return serverMessage{Command: &command}, nil
}

func sendPendingEventWithoutWaiting(ctx context.Context, connection *ProtocolConnection, pending PendingEvent) error {
	var event NodeEvent
	if err := json.Unmarshal(pending.Payload, &event); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Sequence != pending.Sequence {
		return errors.New("node protocol: pending event sequence mismatch")
	}
	kind := MessageActivity
	if event.Outcome != nil {
		kind = MessageAttemptOutcome
	}
	envelope, err := NewEnvelope(kind, connection.node, pending.Sequence, 0, pending.Payload, connection.auth)
	if err != nil {
		return err
	}
	return connection.write(ctx, envelope)
}

func randomNonce() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("node: generate connection nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (d *Daemon) validate() error {
	if err := d.Identity.Validate(); err != nil {
		return err
	}
	if d.Store == nil || d.Runtime == nil || d.Inventory == nil {
		return errors.New("node: daemon requires local store, runtime and inventory source")
	}
	return nil
}

func (d *Daemon) capacity() int {
	if d.Capacity > 0 {
		return d.Capacity
	}
	return 1
}

func (d *Daemon) heartbeatInterval() time.Duration {
	if d.HeartbeatInterval > 0 {
		return d.HeartbeatInterval
	}
	return 15 * time.Second
}

func (d *Daemon) inventoryInterval() time.Duration {
	if d.InventoryInterval > 0 {
		return d.InventoryInterval
	}
	return 5 * time.Minute
}

func (d *Daemon) outboxPollInterval() time.Duration {
	if d.OutboxPollInterval > 0 {
		return d.OutboxPollInterval
	}
	return 250 * time.Millisecond
}

func (d *Daemon) reconnectMin() time.Duration {
	if d.ReconnectMin > 0 {
		return d.ReconnectMin
	}
	return time.Second
}

func (d *Daemon) reconnectMax() time.Duration {
	if d.ReconnectMax > 0 {
		return d.ReconnectMax
	}
	return 30 * time.Second
}
