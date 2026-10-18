package node

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
	"sync"

	"github.com/beruseruko/secretary/internal/core"
)

type Daemon struct {
	Identity  NodeIdentity
	Store     *LocalStore
	Runtime   Runtime
	Inventory InventorySource

	Capacity           int
	Workspaces         []Workspace
	MCPServers         []MCPServer
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

// The user can cancel a dispatched Worker before its queued video becomes ready.
var errWorkerDispatchCanceled = errors.New("worker: dispatch canceled before native start")

// nativeHandoffSignal is attached to the command preparation Context. A
// durable native boundary releases the socket WaitGroup without stopping
// the daemon-lifetime provider operation.
type nativeHandoffSignal struct {ready chan struct{};once sync.Once}
type nativeHandoffKey struct{}
func notifyNativeHandoff(ctx context.Context) {
	if signal,ok:=ctx.Value(nativeHandoffKey{}).(*nativeHandoffSignal);ok {
		signal.once.Do(func(){close(signal.ready)})
	}
}

type runningNodeCommand struct {
	kind CommandKind
	attemptID string
	interrupt context.CancelCauseFunc
}

type commandCompletion struct {
	command Command
	outcome CommandOutcome
	err     error
	// An explicitly replayed CommandID whose original receipt was already
	// ACKed gets its cached response directly; it must not create another
	// outbox event. New native completions always use the durable outbox.
	replyAfterAck bool
}

func (d *Daemon) Run(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}
	execution := NewExecutionNode(d.Identity.Node, d.Runtime, d.Store)
	// Unlike serveConnection's commandCtx, this context survives WebSocket
	// reconnects and ends only when the daemon is actually stopped.
	execution.SetNativeLifetime(ctx)
	execution.SetArtifactFetcher(d.Identity.FetchArtifact)
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	if err := execution.SetArtifactPublisher(d.Identity, binary); err != nil {
		return err
	}
	defer execution.CloseArtifactPublisher()
	if err := execution.SetMCPServers(d.MCPServers); err != nil {
		return err
	}
	if err := execution.SetWorkspaces(d.Workspaces); err != nil {
		return fmt.Errorf("node: set workspace mappings: %w", err)
	}
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
		_ = err
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
	if err := execution.SetInventory(inventory); err != nil {
		return fmt.Errorf("node: set harness inventory: %w", err)
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
		Workspaces: append([]Workspace(nil), d.Workspaces...),
		Nonce:      nonce, LastAcknowledgedSequence: d.Store.LastAcknowledgedSequence(),
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
	completions := make(chan commandCompletion, 4)
	commandCtx, stopCommands := context.WithCancel(ctx)
	var commandWG sync.WaitGroup
	// A disconnected socket must cancel and drain in-flight pre-native
	// commands before the next socket replays their durable claims.
	defer func(){stopCommands();commandWG.Wait()}()
	pendingCommands := make([]Command, 0, 32)
	runningWorkers := make(map[string]bool)
	runningDetails := make(map[string]runningNodeCommand)
	runningCommands := 0
	commandWorkerKey := func(command Command) string {
		worker := command.Metadata().WorkerRef
		if worker != "" {
			return worker
		}
		return command.Metadata().CommandID
	}
	startPending := func() {
		for runningCommands < 4 {
			index := -1
			for i, command := range pendingCommands {
				if !runningWorkers[commandWorkerKey(command)] {
					index = i
					break
				}
			}
			if index < 0 {
				break
			}
			command := pendingCommands[index]
			pendingCommands = append(pendingCommands[:index], pendingCommands[index+1:]...)
			key:=commandWorkerKey(command)
			runningWorkers[key] = true
			runningCommands++
			individualCtx,interrupt:=context.WithCancelCause(commandCtx)
			runningDetails[key]=runningNodeCommand{kind:command.Kind,attemptID:command.Metadata().AttemptID,interrupt:interrupt}
			commandWG.Add(1)
			signal:=&nativeHandoffSignal{ready:make(chan struct{})}
			finished:=make(chan struct{})
			// Only pre-native preparation belongs to the WebSocket. After
			// durable handoff, reconnect must not wait for a provider Resume
			// that may block indefinitely.
			go func(){
				select {case <-signal.ready:case <-finished:}
				commandWG.Done()
			}()
			go func() {
				defer close(finished)
				outcome, err := execution.HandleCommand(context.WithValue(individualCtx,nativeHandoffKey{},signal), command)
				if err!=nil || outcome.State!=CommandAccepted {interrupt(nil)}
				// Native Dispatch/Resume can complete after this socket
				// disconnects. Persist the receipt BEFORE selecting a
				// connection-owned channel: when both cases are ready,
				// selecting completions would silently discard the result.
				// All lifecycle receipts are sent only via the durable outbox,
				// including the normal connected path.
				replyAfterAck:=false
				if err==nil && (command.Kind==CommandDispatch || command.Kind==CommandResume) && outcome.State!=CommandProcessing {
					receipt,queueErr:=d.Store.QueueLifecycleCommandOutcome(outcome)
					if queueErr!=nil {
						err=fmt.Errorf("node: persist native command outcome: %w",queueErr)
					} else if receipt.Sequence==0 {
						// The original terminal receipt (ACCEPTED or FAILED)
						// was already acknowledged. Answer this explicit
						// replay from the cached outcome without creating
						// another durable event.
						replyAfterAck=true
					}
				}
				select {
				case completions <- commandCompletion{command: command, outcome: outcome, err: err, replyAfterAck:replyAfterAck}:
				case <-commandCtx.Done():
					// The native outcome is already durable and the new
					// socket's outbox poll will deliver it.
				}
			}()
		}
	}
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
	lastProcessedCommand := ""
	sendHeartbeat := func() error {
		if err := execution.SetInventory(inventory); err != nil {
			return fmt.Errorf("node: refresh execution inventory: %w", err)
		}
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
		return nil
	}
	if err := sendHeartbeat(); err != nil {return err}
	// Recover pre-native Dispatch/Resume with their original IDs and envelope.
	// ClaimCommand atomically serializes this replay against server redelivery.
	replay,err:=d.Store.PendingPrestartCommands()
	if err!=nil{return err}
	pendingCommands=append(pendingCommands,replay...)
	startPending()

	heartbeats := time.NewTicker(d.heartbeatInterval())
	inventories := time.NewTicker(d.inventoryInterval())
	outbox := time.NewTicker(d.outboxPollInterval())
	defer heartbeats.Stop()
	defer inventories.Stop()
	defer outbox.Stop()
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
			// A Cancel for the same Attempt preempts a dispatched command
			// waiting for video conversion. It must not remain blocked by the
			// per-Worker FIFO lane it is meant to interrupt. Other Workers and
			// attempts remain unaffected.
			if message.Command.Kind==CommandCancel && message.Command.Cancel!=nil {
				key:=commandWorkerKey(*message.Command)
				if running,ok:=runningDetails[key];ok &&
					(running.kind==CommandDispatch || running.kind==CommandResume || running.kind==CommandSteering) &&
					running.attemptID==message.Command.Cancel.Metadata.AttemptID {
					running.interrupt(errWorkerDispatchCanceled)
				}
			}
			if len(pendingCommands) >= 32 {
				return errors.New("node: command queue full")
			}
			pendingCommands = append(pendingCommands, *message.Command)
			startPending()
		case completed := <-completions:
			key:=commandWorkerKey(completed.command)
			delete(runningWorkers,key)
			delete(runningDetails,key)
			runningCommands--
			startPending()
			if completed.err != nil {
				return completed.err
			}
			outcome := completed.outcome
			if outcome.Kind==CommandDispatch || outcome.Kind==CommandResume {
				// A new native receipt was persisted before this channel
				// send. Preserve the established response for an explicit
				// replay of an ALREADY-ACKed CommandID without requeuing it.
				if completed.replyAfterAck {
					if err := connection.SendCommandOutcome(ctx,outcome);err!=nil{return err}
				}
			} else if outcome.State == CommandFailed && outcome.Kind == CommandSteering {
				if _, err := d.Store.QueueCommandOutcome(outcome); err != nil {
					return err
				}
			} else if err := connection.SendCommandOutcome(ctx, outcome); err != nil {
				return err
			}
			lastProcessedCommand = completed.command.Metadata().CommandID
			if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
				return err
			}
		case <-outbox.C:
			if err := d.flushOutbox(ctx, connection, &sentThrough); err != nil {
				return err
			}
		case <-heartbeats.C:
			if err := sendHeartbeat(); err != nil {
				return err
			}
		case <-inventories.C:
			refreshed, err := d.Inventory.Discover(ctx)
			if err != nil {
				continue
			}
			if err := refreshed.Validate(); err != nil || refreshed.Node != d.Identity.Node {
				continue
			}
			if err := execution.SetInventory(refreshed); err != nil {
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
	kind := pending.Type
	if kind == MessageCommandOutcome {
		var outcome CommandOutcome
		if err := json.Unmarshal(pending.Payload, &outcome); err != nil {
			return err
		}
		if outcome.CommandID == "" || outcome.Kind == "" || outcome.State == CommandProcessing {
			return errors.New("node protocol: incomplete command outcome")
		}
	} else {
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
		kind = MessageActivity
		if event.Outcome != nil {
			kind = MessageAttemptOutcome
		}
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
