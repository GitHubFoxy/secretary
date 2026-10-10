package node

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestAuthenticatedProtocolReplaysOutboxAfterStoreReopen(t *testing.T) {
	for _, summary := range []string{"done  with spaces\n\tinside", "<tag>& value \u2028\u2029"} {
		t.Run(summary, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			path := t.TempDir() + "/node.json"
			store, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			outcome := outcomeFixture("persisted-event")
			outcome.Node = "macbook"
			outcome.HarnessInstanceID = "macbook/claude"
			outcome.Summary = summary
			if _, err := store.QueueOutcome(outcome); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := reopened.PendingEvents()
			if err != nil || len(pending) != 1 || !strings.Contains(string(pending[0].Payload), "\n") {
				t.Fatalf("reopened payload was not persisted formatted JSON: pending=%d err=%v", len(pending), err)
			}
			auth := NewAuthenticator([]byte("replay-secret"))
			received := make(chan NodeEvent, 1)
			server := httptest.NewServer(&ProtocolServer{Auth: auth, Handler: nodeProtocolTestHandler{events: received}})
			defer server.Close()
			inventory := core.HarnessInventorySnapshot{Node: outcome.Node, Instances: []core.HarnessInstance{dispatchFixture("fixture").Dispatch.Envelope.HarnessInstance}, ObservedAt: time.Now().UTC()}
			connection, err := DialProtocol(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), outcome.Node, auth, Handshake{Node: outcome.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: "reopened"})
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			if err := connection.SendPendingEvent(ctx, pending[0]); err != nil {
				t.Fatalf("persisted replay failed authentication before ACK: %v", err)
			}
			select {
			case event := <-received:
				if event.Outcome == nil || event.Outcome.Summary != summary || event.Sequence != pending[0].Sequence || event.Outcome.EventID != outcome.EventID {
					t.Fatalf("replayed event=%#v", event)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := reopened.AckThrough(pending[0].Sequence); err != nil {
				t.Fatal(err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			again, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			remaining, err := again.PendingEvents()
			if err != nil || len(remaining) != 0 || again.LastAcknowledgedSequence() != pending[0].Sequence {
				t.Fatalf("ACK did not survive reopen: pending=%d ack=%d err=%v", len(remaining), again.LastAcknowledgedSequence(), err)
			}
		})
	}
}

func TestProtocolSignatureSurvivesPayloadWireFormatting(t *testing.T) {
	auth := NewAuthenticator([]byte("payload-secret"))
	for _, payload := range []string{
		`{"text":"literal  spaces and \\t escape"}`,
		"{\n \"text\": \"literal  spaces and \\\\t escape\"\n}",
		`{"text":"<tag>& value"}`,
		"{\"text\":\"\u2028\u2029\",\"number\":1e-4}",
	} {
		t.Run(payload, func(t *testing.T) {
			envelope, err := NewEnvelope(MessageActivity, "macbook", 7, 0, []byte(payload), auth)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeEnvelope(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if err := auth.Verify(decoded); err != nil {
				t.Fatalf("JSON wire formatting broke signature: %v", err)
			}
			decoded.Payload = json.RawMessage(`{"text":"tampered"}`)
			if err := auth.Verify(decoded); err == nil {
				t.Fatal("tampered payload authenticated")
			}
		})
	}
}

func TestProtocolAcceptsExistingAuthenticatedPayloadFormats(t *testing.T) {
	auth := NewAuthenticator([]byte("compat-secret"))
	for _, test := range []struct {
		payload   string
		signature string
	}{
		{`{"message":"ok  spaced"}`, "B2tfKCtwMFSB1WrGYvjQ_KOmYezZwEw1pRUy9HcvjQ4"},
		{"{\n \"message\": \"ok  spaced\"\n}", "rSIgM3pi4jnBPIvllpOIswaHMsLcWI4LUvrXbH_F0p8"},
		{`{"message":"before \u003ctag\u003e \u0026 after \u2028\u2029"}`, "Gs3R-Xio3rMbkDtbH62I4u1KUvCVVpqgx2cMbjxsntM"},
	} {
		t.Run(test.payload, func(t *testing.T) {
			envelope := Envelope{Version: ProtocolVersion, Type: MessageActivity, Node: "macbook", Sequence: 7, Payload: json.RawMessage(test.payload), Signature: test.signature}
			if err := auth.Verify(envelope); err != nil {
				t.Fatalf("existing signed frame rejected: %v", err)
			}
			if test.payload == `{"message":"ok  spaced"}` && auth.Sign(envelope) != test.signature {
				t.Fatal("canonical compact signature changed")
			}
			envelope.Payload = json.RawMessage(strings.ReplaceAll(test.payload, "ok  spaced", "ok spaced"))
			if strings.Contains(test.payload, "ok  spaced") && auth.Verify(envelope) == nil {
				t.Fatal("whitespace inside a signed string was ignored")
			}
			envelope.Payload = json.RawMessage(test.payload)
			envelope.Node = "other-node"
			if err := auth.Verify(envelope); err == nil {
				t.Fatal("unknown Node identity change authenticated")
			}
		})
	}
	invalid := Envelope{Version: ProtocolVersion, Type: MessageActivity, Node: "macbook", Sequence: 7, Payload: json.RawMessage(`{"incomplete":`), Signature: "OPoXxraunuMT2Nw5h49MHERIbPkPoeF1uss37xKrneA"}
	if auth.Sign(invalid) != "" || auth.Verify(invalid) == nil {
		t.Fatal("invalid JSON did not fail closed")
	}
}
