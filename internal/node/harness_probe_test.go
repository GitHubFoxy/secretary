package node

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
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

func requiredProbeRunner(version, fxModel, codexModel string) fakeProbeRunner {
	codexCatalog := fmt.Sprintf(`{"models":[{"slug":%q,"visibility":"visible","selectable":true,"default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"medium"},{"effort":"high"}]}]}`, codexModel)
	return fakeProbeRunner{responses: map[string]CommandResult{
		"fx --version":       {Stdout: "fx " + version},
		"fx models":          {Stdout: fxModel},
		"claude --version":   {Stdout: "claude " + version},
		"claude auth status": {Stdout: `{"loggedIn":true,"authMethod":"claude.ai"}`},
		"codex --version":    {Stdout: "codex " + version},
		"codex login status": {Stdout: "Logged in using ChatGPT"},
		"codex doctor":       {Stdout: "ready"},
		"codex debug models": {Stdout: codexCatalog},
	}}
}

func TestDefaultProbeCommandsAreExplicitContracts(t *testing.T) {
	claude := DefaultClaudeCodeProbeSpec()
	if !reflect.DeepEqual(claude.VersionArgs, []string{"--version"}) || !reflect.DeepEqual(claude.AuthenticationArgs, []string{"auth", "status"}) {
		t.Fatalf("Claude probe commands=%#v", claude)
	}
	if len(claude.ModelsArgs) != 0 || !claude.ModelsOptional {
		t.Fatalf("Claude must not invent a model-list command: %#v", claude)
	}
	codex := DefaultCodexProbeSpec()
	if !reflect.DeepEqual(codex.AuthenticationArgs, []string{"login", "status"}) || !reflect.DeepEqual(codex.HealthArgs, []string{"doctor"}) || !reflect.DeepEqual(codex.ModelsArgs, []string{"debug", "models"}) {
		t.Fatalf("Codex probe commands=%#v", codex)
	}
	open := DefaultOpenCodeProbeSpec()
	if !reflect.DeepEqual(open.AuthenticationArgs, []string{"auth", "list"}) || !reflect.DeepEqual(open.ModelsArgs, []string{"models"}) {
		t.Fatalf("OpenCode probe commands=%#v", open)
	}
}

func TestClaudeProbeUsesObservedAuthAndDoesNotInventModels(t *testing.T) {
	runner := fakeProbeRunner{responses: map[string]CommandResult{
		"claude --version":   {Stdout: "2.1.0"},
		"claude auth status": {Stdout: `{"loggedIn":true,"authMethod":"claude.ai"}`},
	}}
	result := ProbeClaudeCode(context.Background(), "macbook", runner)
	if !result.Available() || !result.Instance.Authentication.Authenticated || result.Instance.Authentication.Method != "claude.ai" {
		t.Fatalf("Claude result=%#v", result)
	}
	if len(result.Instance.ModelIDs) != 0 || len(result.Instance.ReasoningLevels) != 0 {
		t.Fatalf("unobserved Claude pins were invented: %#v", result.Instance)
	}
	if err := result.Instance.ValidateSelection("claude-sonnet-4", ""); !errors.Is(err, core.ErrObservedPinUnavailable) {
		t.Fatalf("unobserved Claude model pin err=%v", err)
	}

	unauth := fakeProbeRunner{responses: map[string]CommandResult{
		"claude --version":   {Stdout: "2.1.0"},
		"claude auth status": {Stdout: `{"loggedIn":false}`},
	}}
	failed := ProbeClaudeCode(context.Background(), "macbook", unauth)
	if !errors.Is(failed.Err, ErrProbeUnauthenticated) || failed.Instance.Authentication.Authenticated {
		t.Fatalf("unauthenticated Claude=%#v", failed)
	}
}

func TestCodexAuthMethodIsObserved(t *testing.T) {
	for _, test := range []struct {
		output string
		want   string
	}{
		{"Logged in using ChatGPT", "chatgpt"},
		{"Logged in using an API key", "api_key"},
		{"Logged in using Agent Identity", "agent_identity"},
	} {
		ok, method := parseAuthentication(core.HarnessCodex, test.output, "")
		if !ok || method != test.want {
			t.Fatalf("auth %q => ok=%v method=%q", test.output, ok, method)
		}
	}
	if ok, _ := parseAuthentication(core.HarnessCodex, "Not logged in", ""); ok {
		t.Fatal("Codex Not logged in accepted")
	}
}

func TestModelCatalogFiltersUnselectableRecords(t *testing.T) {
	catalog := `[
		{"slug":"gpt-5-codex","visibility":"visible","selectable":true,"default_reasoning_level":"high","supported_reasoning_levels":[{"effort":"medium"},{"effort":"high"}]},
		{"slug":"internal-model","visibility":"hidden","default_reasoning_level":"xhigh"},
		{"slug":"disabled-model","available":false,"default_reasoning_level":"low"},
		{"slug":"fast","selectable":true}
	]`
	models := parseObservedModels(catalog)
	if !reflect.DeepEqual(models, []core.ObservedModelID{"gpt-5-codex"}) {
		t.Fatalf("models=%#v", models)
	}
	levels := parseObservedReasoning(catalog)
	if !reflect.DeepEqual(levels, []core.ObservedReasoningLevel{"high", "medium"}) {
		t.Fatalf("reasoning=%#v", levels)
	}
}

func TestProbeStepsHaveBoundedTimeout(t *testing.T) {
	runner := CommandRunnerFunc(func(ctx context.Context, _ string, args ...string) (CommandResult, error) {
		if len(args) > 0 && args[0] == "--version" {
			return CommandResult{Stdout: "fx 1.0.0"}, nil
		}
		<-ctx.Done()
		return CommandResult{ExitCode: -1}, ctx.Err()
	})
	spec := DefaultFXProbeSpec()
	spec.StepTimeout = 5 * time.Millisecond
	result := (HarnessProbe{Node: "node-a", Spec: spec, Runner: runner}).Probe(context.Background())
	if result.ErrorCode != "unauthenticated" || !errors.Is(result.Err, ErrProbeUnauthenticated) || !strings.Contains(result.Err.Error(), "deadline exceeded") {
		t.Fatalf("timeout result=%#v", result)
	}
}

func TestDefaultCapabilitiesOnlyAdvertiseObservableRuntimeActivity(t *testing.T) {
	allowed := map[core.ActivityCapability]bool{
		core.ActivityAssistantTextDelta: true,
		core.ActivityToolCall:           true,
		core.ActivityStatus:             true,
		core.ActivityAttemptOutcome:     true,
	}
	for _, spec := range []HarnessProbeSpec{DefaultFXProbeSpec(), DefaultClaudeCodeProbeSpec(), DefaultCodexProbeSpec(), DefaultOpenCodeProbeSpec()} {
		for _, capability := range spec.ActivityCapabilities {
			if !allowed[capability] {
				t.Fatalf("%s advertises activity not emitted by current adapter: %s", spec.Kind, capability)
			}
		}
	}
}

func TestRequiredProbesDiscoverDifferentInstancesOnTwoNodes(t *testing.T) {
	at := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	first, err := (HarnessDiscovery{Node: "macbook", Runner: requiredProbeRunner("1.2.3", "fx-model-a", "gpt-5-codex"), Now: func() time.Time { return at }}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := (HarnessDiscovery{Node: "home-server", Runner: requiredProbeRunner("9.0.1", "fx-model-b", "gpt-5.6-luna"), Now: func() time.Time { return at }}).Discover(context.Background())
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
	if !ok || !macClaude.Available() || macClaude.Authentication.Method != "claude.ai" || len(macClaude.ModelIDs) != 0 {
		t.Fatalf("macbook Claude=%#v found=%v", macClaude, ok)
	}
	macCodex, ok := first.Instance("macbook/codex")
	if !ok || !macCodex.SupportsModel("gpt-5-codex") || !macCodex.SupportsReasoning("high") {
		t.Fatalf("macbook Codex=%#v found=%v", macCodex, ok)
	}
	homeCodex, ok := second.Instance("home-server/codex")
	if !ok || !homeCodex.SupportsModel("gpt-5.6-luna") {
		t.Fatalf("home Codex=%#v found=%v", homeCodex, ok)
	}
	if macCodex.ID == homeCodex.ID {
		t.Fatal("instances on different Nodes must have distinct IDs")
	}
}

func TestRequiredProbesAndOpenCodeCompatibilityAreIsolated(t *testing.T) {
	if got := NewDefaultHarnessProbes("macbook", nil); len(got) != 3 {
		t.Fatalf("mandatory probes=%d", len(got))
	}
	for _, probe := range NewDefaultHarnessProbes("macbook", nil) {
		if probe.Spec.Kind == core.HarnessOpenCode {
			t.Fatal("OpenCode leaked into mandatory probe set")
		}
	}
	runner := fakeProbeRunner{responses: map[string]CommandResult{
		"opencode --version": {Stdout: "1.0.0"},
		"opencode auth list": {Stdout: "anthropic"},
		"opencode models":    {Stdout: "provider/model-a"},
	}}
	open := NewOpenCodeCompatibilityProbe("macbook", runner).Probe(context.Background())
	if open.Instance.Kind != core.HarnessOpenCode || !open.Available() {
		t.Fatalf("OpenCode compatibility result=%#v", open)
	}
}

func TestProbeMakesMissingUnauthenticatedAndUnhealthyExplicit(t *testing.T) {
	spec := HarnessProbeSpec{Kind: core.HarnessFX, Binary: "fx", VersionArgs: []string{"version"}, AuthenticationArgs: []string{"auth"}, HealthArgs: []string{"health"}, ModelsArgs: []string{"models"}}
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
