package node

import (
	"context"
	"path/filepath"
	"testing"

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
		Nonce: "nonce", NonceSignature: "signature",
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
