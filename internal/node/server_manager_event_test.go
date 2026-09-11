package node

import (
	"context"
	"testing"
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
