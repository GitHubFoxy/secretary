package acp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestLargeSessionTitleDoesNotStrandPromptResponse(t *testing.T) {
	reader, peer := io.Pipe()
	client := NewClient(&recordingWriter{})
	waited := make(chan struct{})
	client.wait = func() error { <-waited; return nil }
	go client.read(reader)
	defer func() { close(waited); _ = reader.Close(); _ = peer.Close(); _ = client.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- client.RequestDrainingEvents(ctx, "session/prompt", map[string]any{}, nil) }()
	for {
		if _, exists := client.pending.Load(uint64(1)); exists {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("prompt was not admitted")
		default:
		}
	}
	frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "session", "update": map[string]any{"sessionUpdate": "session_info_update", "title": strings.Repeat("canonical context ", 10000)}}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = peer.Write(append(frame, '\n'))
		_, _ = io.WriteString(peer, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"stopReason\":\"end_turn\"}}\n")
	}()
	observed := make(chan Message, 1)
	go func() {
		for message := range client.Events() {
			if !message.AcknowledgeEventBarrier() {
				observed <- message
			}
		}
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("native terminal response stranded behind valid large notification: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("native terminal response stranded behind valid large notification")
	}
	select {
	case message := <-observed:
		if message.Method != "session/update" || !strings.Contains(string(message.Params), strings.Repeat("canonical context ", 10000)) {
			t.Fatal("large native notification was dropped or truncated")
		}
	default:
		t.Fatal("terminal response bypassed preceding notification")
	}
}
