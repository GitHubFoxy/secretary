package node

import (
	"context"
	"errors"
)

// OutboundNode joins the local execution core to an authenticated outbound
// protocol connection. Pairing and reconnect orchestration remain outside this
// slice, while replay and command handling are durable here.
type OutboundNode struct {
	Execution  *ExecutionNode
	Connection *ProtocolConnection
	Store      *LocalStore
}

func NewOutboundNode(execution *ExecutionNode, connection *ProtocolConnection, store *LocalStore) *OutboundNode {
	return &OutboundNode{Execution: execution, Connection: connection, Store: store}
}

func (n *OutboundNode) HandleNextCommand(ctx context.Context) error {
	if n.Execution == nil || n.Connection == nil {
		return errors.New("node: outbound runtime is not connected")
	}
	command, err := n.Connection.ReceiveCommand(ctx)
	if err != nil {
		return err
	}
	outcome, err := n.Execution.HandleCommand(ctx, command)
	if err != nil {
		return err
	}
	return n.Connection.SendCommandOutcome(ctx, outcome)
}

func (n *OutboundNode) ReplayOutbox(ctx context.Context) error {
	if n.Connection == nil || n.Store == nil {
		return errors.New("node: outbound runtime has no connection or local store")
	}
	pending, err := n.Store.PendingEvents()
	if err != nil {
		return err
	}
	for _, event := range pending {
		if err := n.Connection.SendPendingEvent(ctx, event); err != nil {
			return err
		}
		if err := n.Store.AckThrough(event.Sequence); err != nil {
			return err
		}
	}
	return nil
}

func (n *OutboundNode) Run(ctx context.Context) error {
	for {
		if err := n.HandleNextCommand(ctx); err != nil {
			return err
		}
	}
}
