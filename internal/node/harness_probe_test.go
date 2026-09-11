package node

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type fakeProbeRunner struct {
	responses map[string]CommandResult
	errors    map[string]error
}

func (r fakeProbeRunner) Run(_ context.Context, binary string, args ...string) (CommandResult, error) {
	key := binary + " " + strings.Join(args, " ")
	if err := r.errors[key]; err != nil {
		return CommandResult{ExitCode: -1}, err
	}
	if result, ok := r.responses[key]; ok {
		return result, nil
	}
	return CommandResult{ExitCode: -1}, fmt.Errorf("missing fake command %q", key)
}

func completeProbeRunner(version, model, reasoning string) fakeProbeRunner {
	responses := map[string]CommandResult{}
	for _, spec := range []HarnessProbeSpec{DefaultFXProbeSpec(), DefaultClaudeCodeProbeSpec(), DefaultCodexProbeSpec(), DefaultOpenCodeProbeSpec()} {
		responses[spec.Binary+" "+strings.Join(spec.VersionArgs, " ")] = CommandResult{Stdout: version}
		responses[spec.Binary+" "+strings.Join(spec.AuthenticationArgs, " ")] = CommandResult{Stdout: "authenticated"}
		responses[spec.Binary+" "+strings.Join(spec.HealthArgs, " ")] = CommandResult{Stdout: "ready"}
		responses[spec.Binary+" "+strings.Join(spec.ModelsArgs, " ")] = CommandResult{Stdout: model}
		responses[spec.Binary+" "+strings.Join(spec.ReasoningArgs, " ")] = CommandResult{Stdout: reasoning}
		responses[spec.Binary+" reasoning"] = CommandResult{Stdout: reasoning}
	}
	return fakeProbeRunner{responses: responses}
}

func TestRequiredProbesDiscoverDifferentInstancesOnTwoNodes(t *testing.T) {
	at := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	firstRunner := completeProbeRunner("1.2.3", "claude-sonnet-4,gpt-5-codex", "default,extended")
	firstProbes := NewDefaultHarnessProbes("macbook", firstRunner)
	for i := range firstProbes {
		firstProbes[i].Spec.ReasoningArgs = []string{"reasoning"}
	}
	first, err := (HarnessDiscovery{Node: "macbook", Probes: firstProbes, Now: func() time.Time { return at }}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondRunner := completeProbeRunner("9.0.1", "gpt-5.6-luna", "high")
	secondProbes := NewDefaultHarnessProbes("home-server", secondRunner)
	for i := range secondProbes {
		secondProbes[i].Spec.ReasoningArgs = []string{"reasoning"}
	}
	second, err := (HarnessDiscovery{Node: "home-server", Probes: secondProbes, Now: func() time.Time { return at }}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := second.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(first.Instances) != 3 || len(second.Instances) != 3 {
		t.Fatalf("required inventory sizes: %d and %d", len(first.Instances), len(second.Instances))
	}
	macClaude, ok := first.Instance("macbook/claude")
	if !ok || macClaude.Kind != core.HarnessClaudeCode || macClaude.Version != "1.2.3" || !macClaude.SupportsModel("claude-sonnet-4") || !macClaude.SupportsReasoning("extended") {
		t.Fatalf("macbook Claude=%#v found=%v", macClaude, ok)
	}
	homeFX, ok := second.Instance("home-server/fx")
	if !ok || homeFX.Version != "9.0.1" || !homeFX.SupportsModel("gpt-5.6-luna") || !homeFX.SupportsReasoning("high") {
		t.Fatalf("home-server fx=%#v found=%v", homeFX, ok)
	}
	if macClaude.ID == homeFX.ID {
		t.Fatal("instances on different Nodes must have distinct IDs")
	}
}

func TestRequiredProbesAndOpenCodeCompatibilityAreIsolated(t *testing.T) {
	runner := completeProbeRunner("1.0.0", "model-a", "default")
	if got := NewDefaultHarnessProbes("macbook", runner); len(got) != 3 {
		t.Fatalf("mandatory probes=%d", len(got))
	}
	for _, probe := range NewDefaultHarnessProbes("macbook", runner) {
		if probe.Spec.Kind == core.HarnessOpenCode {
			t.Fatal("OpenCode leaked into mandatory probe set")
		}
	}
	open := NewOpenCodeCompatibilityProbe("macbook", runner).Probe(context.Background())
	if open.Instance.Kind != core.HarnessOpenCode || !open.Available() {
		t.Fatalf("OpenCode compatibility result=%#v", open)
	}
}

func TestProbeMakesMissingUnauthenticatedAndUnhealthyExplicit(t *testing.T) {
	spec := HarnessProbeSpec{Kind: core.HarnessFX, Binary: "fx", VersionArgs: []string{"version"}, AuthenticationArgs: []string{"auth"}, HealthArgs: []string{"health"}, ModelsArgs: []string{"models"}, ReasoningArgs: []string{"reasoning"}}
	missing := (HarnessProbe{Node: "node-a", Spec: spec, Runner: fakeProbeRunner{}}).Probe(context.Background())
	if missing.Instance.Status != core.HarnessUnavailable || missing.Instance.Authentication.Authenticated || missing.ErrorCode != "missing" || !errors.Is(missing.Err, ErrProbeMissing) {
		t.Fatalf("missing=%#v", missing)
	}
	unauthRunner := fakeProbeRunner{responses: map[string]CommandResult{
		"fx version": {Stdout: "fx 1.0.0"},
		"fx auth":    {Stdout: "not authenticated"},
	}}
	unauth := (HarnessProbe{Node: "node-a", Spec: spec, Runner: unauthRunner}).Probe(context.Background())
	if unauth.Instance.Status != core.HarnessUnavailable || unauth.Instance.Authentication.Authenticated || unauth.ErrorCode != "unauthenticated" || !errors.Is(unauth.Err, ErrProbeUnauthenticated) {
		t.Fatalf("unauthenticated=%#v", unauth)
	}
	unhealthyRunner := fakeProbeRunner{responses: map[string]CommandResult{
		"fx version": {Stdout: "fx 1.0.0"},
		"fx auth":    {Stdout: "authenticated"},
		"fx health":  {Stdout: "unhealthy"},
	}}
	unhealthy := (HarnessProbe{Node: "node-a", Spec: spec, Runner: unhealthyRunner}).Probe(context.Background())
	if unhealthy.Instance.Status != core.HarnessUnavailable || !unhealthy.Instance.Authentication.Authenticated || unhealthy.ErrorCode != "unhealthy" || !errors.Is(unhealthy.Err, ErrProbeUnhealthy) {
		t.Fatalf("unhealthy=%#v", unhealthy)
	}
}

type fakeInventorySource struct {
	inventory core.HarnessInventorySnapshot
}

func (s fakeInventorySource) Discover(context.Context) (core.HarnessInventorySnapshot, error) {
	return s.inventory, nil
}

type inventoryProtocolHandler struct {
	updates chan core.HarnessInventorySnapshot
}

func (h inventoryProtocolHandler) HandleNodeHandshake(_ context.Context, handshake Handshake) (HandshakeAccepted, error) {
	return HandshakeAccepted{Node: handshake.Node, ProtocolVersion: ProtocolVersion}, nil
}

func (h inventoryProtocolHandler) HandleNodeInventory(_ context.Context, inventory core.HarnessInventorySnapshot) error {
	h.updates <- inventory
	return nil
}

func TestInventoryRefreshUsesProtocolAndKeepsObservedBindingLocal(t *testing.T) {
	auth := NewAuthenticator([]byte("inventory-secret"))
	updates := make(chan core.HarnessInventorySnapshot, 1)
	server := &ProtocolServer{Auth: auth, Handler: inventoryProtocolHandler{updates: updates}}
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	initial := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{{ID: "macbook/fx", Node: "macbook", Kind: core.HarnessFX, Version: "1.0.0", Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady}}, ObservedAt: time.Now().UTC()}
	connection, err := DialProtocol(context.Background(), "ws"+strings.TrimPrefix(httpServer.URL, "http"), "macbook", auth, Handshake{Node: "macbook", ProtocolVersion: ProtocolVersion, Inventory: initial, Nonce: "nonce"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	updated := initial
	updated.Instances[0].Version = "2.0.0"
	outbound := NewOutboundNode(nil, connection, store)
	got, err := outbound.RefreshInventory(context.Background(), fakeInventorySource{inventory: updated})
	if err != nil || got.Instances[0].Version != "2.0.0" {
		t.Fatalf("refresh got=%#v err=%v", got, err)
	}
	select {
	case observed := <-updates:
		if observed.Instances[0].Version != "2.0.0" {
			t.Fatalf("server observed=%#v", observed)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive inventory update")
	}
}

func TestWorkerEnvelopeUsesObservedInventoryForPins(t *testing.T) {
	instance := core.HarnessInstance{ID: "macbook/codex", Node: "macbook", Kind: core.HarnessCodex, Version: "1.0.0", Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady, ModelIDs: []core.ObservedModelID{"gpt-5-codex"}, ReasoningLevels: []core.ObservedReasoningLevel{"high"}}
	envelope := WorkerEnvelope{WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", OriginalUserIntent: "inspect", HarnessInstance: instance, Model: "gpt-5-codex", Reasoning: "high"}
	inventory := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
	if err := envelope.ValidateAgainstInventory("macbook", inventory); err != nil {
		t.Fatal(err)
	}
	envelope.Model = "not-observed"
	if err := envelope.ValidateAgainstInventory("macbook", inventory); !errors.Is(err, core.ErrObservedPinUnavailable) {
		t.Fatalf("missing model pin err=%v", err)
	}
}

func TestNormalizeRuntimeActivityNeverSynthesizesUnsupportedEvents(t *testing.T) {
	metadata := core.ActivityMetadata{EventID: "event", Node: "node", HarnessInstanceID: "node/fx", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityAssistantTextDelta}}
	if activity, ok := normalizeRuntimeActivity(Activity{Kind: ActivityText, Text: "hello"}, metadata, capabilities); !ok || activity.Kind != core.ActivityAssistantTextDelta {
		t.Fatalf("supported activity=%#v ok=%v", activity, ok)
	}
	for _, item := range []Activity{{Kind: ActivityTool, Text: "shell"}, {Kind: ActivityStatus, Text: "working"}, {Kind: ActivityKind("unknown"), Text: "fake"}} {
		if activity, ok := normalizeRuntimeActivity(item, metadata, capabilities); ok || activity.Kind != "" {
			t.Fatalf("synthetic activity=%#v ok=%v", activity, ok)
		}
	}
}
