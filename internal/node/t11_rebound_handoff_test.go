package node

import (
	"context"
	"testing"
	"time"
)

func TestT11RecoveredACPRequestRemainsAnswerableDuringNativeRegistration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]any
	}{
		{name: "inferred original input", params: map[string]any{}},
		{name: "explicit original ID", params: map[string]any{"request_id": "durable-input"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session := &acpSession{
				pending:          make(map[string]*pendingACPResponse),
				resolved:         make(map[string]struct{}),
				reboundResponses: make(map[string]*pendingACPResponse),
				activityDone:     make(chan struct{}),
			}
			session.RebindPendingRequests([]PendingRequest{{RequestID: "durable-input", Kind: ActivityUserInput}})
			id, err := session.reboundRequestID(tc.params, ActivityUserInput)
			if err != nil || id != "durable-input" {
				t.Fatalf("native request handoff ID=%q err=%v", id, err)
			}
			session.requestMu.Lock()
			pending := session.reboundResponses[id]
			remaining := len(session.rebound)
			session.requestMu.Unlock()
			if pending == nil || remaining != 0 {
				t.Fatalf("durable request disappeared during handoff: placeholder=%t rebound=%d", pending != nil, remaining)
			}

			responded := make(chan error, 1)
			go func() {
				responded <- session.Respond(ctx, id, "Please analyze this original image")
			}()
			select {
			case response := <-pending.response:
				if response != "Please analyze this original image" {
					t.Fatalf("recovered interaction response=%q", response)
				}
			case err := <-responded:
				t.Fatalf("response failed before native request registered: %v", err)
			case <-ctx.Done():
				t.Fatal("response could not cross durable/native registration window")
			}

			session.requestMu.Lock()
			session.pending[id] = pending
			delete(session.reboundResponses, id)
			session.requestMu.Unlock()
			pending.delivered <- nil
			select {
			case err := <-responded:
				if err != nil {
					t.Fatalf("native response delivery failed: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("response remained blocked after native registration")
			}
			if !pending.responseSent {
				t.Fatal("durable response was not marked sent exactly once")
			}
		})
	}
}
