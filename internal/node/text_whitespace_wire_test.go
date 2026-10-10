package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestNativeTextWhitespaceSurvivesAuthenticatedNodeWire(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	store, err := core.Open(ctx, t.TempDir()+"/server.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	instance := dispatchFixture("whitespace").Dispatch.Envelope.HarnessInstance
	instance.ID, instance.Kind = "macbook/codex", core.HarnessCodex
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "text-worker", Intent: "readable text", ProjectID: "project", NodeID: string(instance.Node), HarnessInstanceID: string(instance.ID)}, core.TurnSpec{Input: "readable text"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	chunks := []string{"# Heading", "\n\n", "word", " ", "next", "\n\n```javascript\n", `console.log("ok")`, "\n```"}
	for index, chunk := range chunks {
		metadata := core.ActivityMetadata{EventID: fmt.Sprintf("text-%d", index), Node: instance.Node, HarnessInstanceID: instance.ID, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, Sequence: uint64(index + 1), ObservedAt: time.Now().UTC()}
		activity, ok := NormalizeRuntimeActivity(Activity{Kind: ActivityText, Text: chunk}, metadata, instance.Capabilities)
		if !ok {
			t.Fatalf("native text fragment %d was dropped", index)
		}
		if err := activity.ValidateFor(instance); err != nil {
			t.Fatalf("native text fragment %d invalid: %v", index, err)
		}
		if _, err := local.QueueActivity(activity); err != nil {
			t.Fatal(err)
		}
	}
	handler := &reviewLateWireHandler{sink: NewStoreEventSink(store)}
	auth := NewAuthenticator([]byte(strings.Repeat("w", 32)))
	server := httptest.NewServer(&ProtocolServer{Auth: auth, Handler: handler})
	defer server.Close()
	inventory := core.HarnessInventorySnapshot{Node: instance.Node, Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
	connection, err := DialProtocol(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), instance.Node, auth, Handshake{Node: instance.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: "whitespace-wire"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	pending, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range pending {
		if err := connection.SendPendingEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := local.AckThrough(event.Sequence); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.EventsAfterSeq(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var observed []string
	for _, event := range events {
		if event.Kind != "attempt.activity" {
			continue
		}
		var activity core.Activity
		if err := json.Unmarshal(event.Payload, &activity); err != nil {
			t.Fatal(err)
		}
		observed = append(observed, activity.Text)
	}
	if len(observed) != len(chunks) || strings.Join(observed, "") != strings.Join(chunks, "") {
		t.Fatalf("canonical activity lost native spacing: chunks=%d want=%d", len(observed), len(chunks))
	}
}

func TestWhitespaceTextKeepsActivityValidationBoundaries(t *testing.T) {
	metadata := core.ActivityMetadata{EventID: "text", Node: "macbook", HarnessInstanceID: "macbook/codex", WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityAssistantTextDelta, core.ActivityThinkingSummary, core.ActivityStatus}}
	activity := core.Activity{Metadata: metadata, Kind: core.ActivityAssistantTextDelta, Text: " \n\t"}
	if err := activity.Validate(capabilities); err != nil {
		t.Fatalf("nonempty native text whitespace rejected: %v", err)
	}
	if err := activity.ValidatePayload(); err != nil {
		t.Fatal(err)
	}
	if err := activity.Validate(core.HarnessCapabilities{}); err == nil {
		t.Fatal("unsupported capability accepted")
	}
	wrong := activity
	wrong.Metadata.AttemptID = ""
	if err := wrong.ValidatePayload(); err == nil {
		t.Fatal("missing Attempt identity accepted")
	}
	empty := activity
	empty.Text = ""
	if err := empty.ValidatePayload(); err == nil {
		t.Fatal("empty text accepted")
	}
	for _, kind := range []core.ActivityKind{core.ActivityThinkingSummary, core.ActivityStatus} {
		forbidden := activity
		forbidden.Kind = kind
		if err := forbidden.Validate(capabilities); err == nil {
			t.Fatalf("whitespace %s accepted", kind)
		}
	}
	for _, item := range []Activity{{Kind: ActivityText}, {Kind: ActivityThinkingSummary, Text: " \n\t"}, {Kind: ActivityStatus, Text: " \n\t"}} {
		if _, ok := NormalizeRuntimeActivity(item, metadata, capabilities); ok {
			t.Fatalf("empty/whitespace runtime %s synthesized", item.Kind)
		}
	}
	if _, ok := NormalizeRuntimeActivity(Activity{Kind: ActivityText, Text: " "}, metadata, core.HarnessCapabilities{}); ok {
		t.Fatal("unsupported native text synthesized")
	}
}
