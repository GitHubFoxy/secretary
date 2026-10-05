package acp

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestRequestDrainingEventsWaitsForFIFOConsumer(t *testing.T) {
	reader, input := io.Pipe()
	client := NewClient(&recordingWriter{})
	client.wait = func() error { return nil }
	go client.read(reader)
	defer client.Close()
	defer input.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- client.RequestDrainingEvents(ctx, "session/load", map[string]string{}, nil) }()
	// Wait for admission without relying on scheduler sleeps.
	for {
		if _, exists := client.pending.Load(uint64(1)); exists {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("request not admitted")
		default:
		}
	}
	go func() { _, _ = io.WriteString(input, "{\"method\":\"session/update\"}\n{\"id\":1,\"result\":{}}\n") }()
	first := <-client.Events()
	if first.Method != "session/update" || first.AcknowledgeEventBarrier() {
		t.Fatal("native notification was replaced by a barrier")
	}
	barrier := <-client.Events()
	select {
	case <-finished:
		t.Fatal("request completed before its preceding notifications were acknowledged")
	default:
	}
	if !barrier.AcknowledgeEventBarrier() {
		t.Fatal("ordered local event barrier missing")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal("barrier request failed")
		}
	case <-ctx.Done():
		t.Fatal("barrier request did not complete")
	}
}
