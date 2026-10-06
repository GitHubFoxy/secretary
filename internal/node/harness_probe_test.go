package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type fakeProbeRunner struct {
	responses map[string]CommandResult
	errors    map[string]error
	acpError  error
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

func (r fakeProbeRunner) ProbeACP(context.Context, string, ...string) error { return r.acpError }

func (r fakeProbeRunner) ObserveOpenCodeModels(ctx context.Context, command string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	// This is explicit synthetic metadata; the production runner uses the V2
	// model API and never infers reasoning from plaintext model IDs.
	result, err := r.Run(ctx, command, "models")
	if err != nil {
		return nil, nil, err
	}
	return parseObservedModels(result.Stdout), parseObservedReasoning(result.Stdout), nil
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

func TestOpenCodeProbeRequiresSelectedPersistentStore(t *testing.T) {
	spec := DefaultOpenCodeProbeSpec()
	spec.Binary = filepath.Join(t.TempDir(), "must-not-run-opencode")
	result := (HarnessProbe{Node: "node-a", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
	if result.ErrorCode != "native_store_unselected" || result.Instance.Status != core.HarnessUnavailable {
		t.Fatal("OpenCode inventory ran without an explicitly selected persistent store")
	}
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
	if !reflect.DeepEqual(open.AuthenticationArgs, []string{"auth", "list", "--format", "json", "--standalone"}) || !reflect.DeepEqual(open.ModelsArgs, []string{"models"}) || !reflect.DeepEqual(open.ACPArgs, []string{"acp"}) {
		t.Fatalf("OpenCode probe commands=%#v", open)
	}
}

func TestOpenCodeProbeRequiresStoredCredentialMetadata(t *testing.T) {
	storedCredential := `[{"id":"openai","name":"OpenAI","connections":[{"type":"credential"}]}]`
	for _, test := range []struct {
		name   string
		result CommandResult
		want   bool
	}{
		{name: "stored credential", result: CommandResult{Stdout: storedCredential}, want: true},
		{name: "only ambient credentials", result: CommandResult{Stdout: `[]`}},
		{name: "empty output", result: CommandResult{}},
		{name: "malformed output", result: CommandResult{Stdout: `[{"id":`}},
		{name: "connection is not a credential", result: CommandResult{Stdout: `[{"id":"openai","name":"OpenAI","connections":[{"type":"oauth"}]}]`}},
		{name: "credential output with failed exit", result: CommandResult{Stdout: storedCredential, ExitCode: 1}},
		{name: "credential only on stderr", result: CommandResult{Stderr: storedCredential}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := fakeProbeRunner{responses: map[string]CommandResult{
				"opencode --version":                            {Stdout: "opencode v2.0.22"},
				"opencode auth list --format json --standalone": test.result,
				"opencode models":                               {Stdout: "openai/model-a"},
			}}
			result := NewOpenCodeCompatibilityProbe("node-a", runner).Probe(context.Background())
			if result.Available() != test.want || result.Instance.Authentication.Authenticated != test.want {
				t.Fatalf("OpenCode auth result ready=%v authenticated=%v, want %v", result.Available(), result.Instance.Authentication.Authenticated, test.want)
			}
			if !test.want && result.ErrorCode != "unauthenticated" {
				t.Fatalf("OpenCode auth error code=%q, want unauthenticated", result.ErrorCode)
			}
		})
	}
}

func TestOpenCodeProbeRejectsUnsupportedVersionBeforeReadiness(t *testing.T) {
	runner := fakeProbeRunner{responses: map[string]CommandResult{
		"opencode --version":                            {Stdout: "opencode v1.18.29"},
		"opencode auth list --format json --standalone": {Stdout: `[{"id":"openai","name":"OpenAI","connections":[{"type":"credential"}]}]`},
	}}
	result := NewOpenCodeCompatibilityProbe("node-a", runner).Probe(context.Background())
	if result.Available() || result.Instance.Status == core.HarnessReady || result.ErrorCode != "unsupported_version" {
		t.Fatalf("unsupported OpenCode version was ready: %#v", result)
	}
}

type executableOpenCodeProbeRunner struct{ ExecCommandRunner }

func (executableOpenCodeProbeRunner) ObserveOpenCodeModels(context.Context, string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	return []core.ObservedModelID{"openai/model-a"}, []core.ObservedReasoningLevel{"xhigh"}, nil
}

func TestOpenCodeProbeUsesSelectedStoreAndIgnoresAmbientCredentials(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "opencode-fixture")
	logPath := filepath.Join(root, "probe-metadata.log")
	personalHome := filepath.Join(root, "personal-data")
	selectedHome := filepath.Join(root, "selected-data")
	if err := os.MkdirAll(filepath.Join(personalHome, "opencode"), 0o700); err != nil {
		t.Fatal("personal canary directory unavailable")
	}
	if err := os.WriteFile(filepath.Join(personalHome, "opencode", "auth.json"), []byte(`[{"id":"openai","name":"OpenAI","connections":[{"type":"credential"}]}]`), 0o600); err != nil {
		t.Fatal("personal canary unavailable")
	}
	if err := os.MkdirAll(filepath.Join(selectedHome, "opencode"), 0o700); err != nil {
		t.Fatal("selected store unavailable")
	}
	script := `#!/bin/sh
printf '%s|%s|%s|%s\n' "$XDG_DATA_HOME" "${OPENAI_API_KEY:+present}" "$HOME" "$*" >> "$AUTH_PROBE_LOG"
case "$1" in
  --version) printf 'opencode v2.0.22\n' ;;
  auth)
    if [ "$2" != "list" ] || [ "$3" != "--format" ] || [ "$4" != "json" ] || [ "$5" != "--standalone" ]; then exit 2; fi
    if [ "${AUTH_PROBE_STALL:-false}" = "true" ]; then printf '%s\n' "$$" > "$AUTH_PROBE_PID"; exec /bin/sleep 30; fi
    if [ -f "$XDG_DATA_HOME/opencode/auth.json" ]; then /bin/cat "$XDG_DATA_HOME/opencode/auth.json"; else printf '[]\n'; fi
    ;;
  *) exit 0 ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal("OpenCode executable fixture unavailable")
	}
	t.Setenv("XDG_DATA_HOME", personalHome)
	t.Setenv("OPENAI_API_KEY", "fixture-only-ambient-key")
	t.Setenv("AUTH_PROBE_LOG", logPath)
	t.Setenv("AUTH_PROBE_PID", filepath.Join(root, "probe.pid"))

	writeSelectedCredential := func(home string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(home, "opencode"), 0o700); err != nil {
			t.Fatal("selected native store unavailable")
		}
		if err := os.WriteFile(filepath.Join(home, "opencode", "auth.json"), []byte(`[{"id":"openai","name":"OpenAI","connections":[{"type":"credential"}]}]`), 0o600); err != nil {
			t.Fatal("selected native credential fixture unavailable")
		}
	}
	probe := func(home string, stepTimeout time.Duration) ProbeResult {
		t.Helper()
		spec := DefaultOpenCodeProbeSpec()
		spec.Binary = binary
		spec.ACPArgs = nil
		spec.StepTimeout = stepTimeout
		return (HarnessProbe{Node: "node-a", Spec: spec, Runner: executableOpenCodeProbeRunner{ExecCommandRunner{OpenCodeDataHome: home}}}).Probe(context.Background())
	}
	writeSelectedCredential(selectedHome)
	ready := probe(selectedHome, time.Second)
	if !ready.Available() || !ready.Instance.Authentication.Authenticated {
		t.Fatalf("selected stored credential was not ready: status=%s error=%s", ready.Instance.Status, ready.ErrorCode)
	}
	lines, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal("probe did not reach the executable boundary")
	}
	var authRecord string
	for _, line := range strings.Split(strings.TrimSpace(string(lines)), "\n") {
		if strings.Contains(line, "auth list") {
			authRecord = line
			break
		}
	}
	fields := strings.SplitN(authRecord, "|", 4)
	if len(fields) != 4 || fields[0] != selectedHome || fields[1] != "" || fields[3] != "auth list --format json --standalone" {
		t.Fatal("OpenCode auth probe used ambient credentials, wrong store, or wrong CLI flags")
	}
	if _, err := os.Stat(fields[2]); !os.IsNotExist(err) {
		t.Fatal("temporary OpenCode probe environment was not removed")
	}

	if err := os.Remove(logPath); err != nil {
		t.Fatal("could not reset private probe metadata")
	}
	emptySelectedHome := filepath.Join(root, "empty-selected-data")
	unauthenticated := probe(emptySelectedHome, time.Second)
	if unauthenticated.Available() || unauthenticated.Instance.Authentication.Authenticated || unauthenticated.ErrorCode != "unauthenticated" {
		t.Fatal("credential from ambient personal store authenticated the selected empty store")
	}
}

func TestOpenCodeProbeTimeoutStopsNativeProcessAndRemovesPrivateEnvironment(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "opencode-stall-fixture")
	logPath := filepath.Join(root, "probe-metadata.log")
	pidPath := filepath.Join(root, "probe.pid")
	script := `#!/bin/sh
printf '%s|%s|%s|%s\n' "$XDG_DATA_HOME" "${OPENAI_API_KEY:+present}" "$HOME" "$*" >> "$AUTH_PROBE_LOG"
case "$1" in
  --version) printf 'opencode v2.0.22\n' ;;
  auth) printf '%s\n' "$$" > "$AUTH_PROBE_PID"; exec /bin/sleep 30 ;;
  *) exit 0 ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal("OpenCode timeout fixture unavailable")
	}
	t.Setenv("AUTH_PROBE_LOG", logPath)
	t.Setenv("AUTH_PROBE_PID", pidPath)
	spec := DefaultOpenCodeProbeSpec()
	spec.Binary = binary
	spec.ACPArgs = nil
	spec.StepTimeout = 500 * time.Millisecond
	result := (HarnessProbe{Node: "node-a", Spec: spec, Runner: executableOpenCodeProbeRunner{ExecCommandRunner{OpenCodeDataHome: filepath.Join(root, "selected")}}}).Probe(context.Background())
	if result.Available() || result.ErrorCode != "unauthenticated" || !strings.Contains(result.Err.Error(), "deadline exceeded") {
		t.Fatalf("timed-out native auth command was accepted: status=%s error=%s detail=%v", result.Instance.Status, result.ErrorCode, result.Err)
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal("native auth process did not start")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal("native auth process ID was unavailable")
	}
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.Signal(0)) == nil {
		t.Fatal("timed-out native auth process was not stopped")
	}
	line, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal("native auth process did not record its temporary environment")
	}
	fields := strings.SplitN(strings.TrimSpace(string(line)), "|", 4)
	if len(fields) != 4 || fields[2] == "" {
		t.Fatal("temporary OpenCode probe environment metadata was incomplete")
	}
	if _, err := os.Stat(fields[2]); !os.IsNotExist(err) {
		t.Fatal("timed-out OpenCode probe left its private environment behind")
	}
}

func TestClaudeCapabilitiesMatchImplementedRuntime(t *testing.T) {
	spec := DefaultClaudeCodeProbeSpec()
	if spec.Kind != core.HarnessClaudeCode {
		t.Fatalf("Claude spec kind=%q", spec.Kind)
	}
	for _, unsupported := range []core.ExecutionCapability{core.CapabilitySteering, core.CapabilityApprovals} {
		if specHasExecutionCapability(spec, unsupported) {
			t.Fatalf("Claude advertises unsupported execution capability %q", unsupported)
		}
	}
	want := []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel}
	if !reflect.DeepEqual(spec.ExecutionCapabilities, want) {
		t.Fatalf("Claude execution capabilities=%#v, want %#v", spec.ExecutionCapabilities, want)
	}

	result := ProbeClaudeCode(context.Background(), "macbook", fakeProbeRunner{responses: map[string]CommandResult{
		"claude --version":   {Stdout: "2.1.0"},
		"claude auth status": {Stdout: `{"loggedIn":true}`},
	}})
	if result.Instance.Capabilities.SupportsExecution(core.CapabilitySteering) || result.Instance.Capabilities.SupportsExecution(core.CapabilityApprovals) {
		t.Fatalf("Claude inventory advertises unsupported capabilities: %#v", result.Instance.Capabilities.Execution)
	}
}

func specHasExecutionCapability(spec HarnessProbeSpec, want core.ExecutionCapability) bool {
	for _, capability := range spec.ExecutionCapabilities {
		if capability == want {
			return true
		}
	}
	return false
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

func TestCodexSuccessfulEmptyAuthStatusIsAuthenticated(t *testing.T) {
	runner := fakeProbeRunner{responses: map[string]CommandResult{
		"codex --version":    {Stdout: "codex 0.135.0"},
		"codex login status": {ExitCode: 0},
		"codex doctor":       {Stdout: "ready"},
		"codex debug models": {Stdout: `{"models":[{"slug":"gpt-5.4","visibility":"list","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"medium"}]}]}`},
	}}
	result := ProbeCodex(context.Background(), "home-server", runner)
	if !result.Available() || !result.Instance.Authentication.Authenticated || result.Instance.Authentication.Method != "credential" {
		t.Fatalf("Codex result=%#v", result)
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
		core.ActivityThinkingSummary:    true,
		core.ActivityToolResult:         true,
		core.ActivityPermissionRequest:  true,
		core.ActivityUserInputRequest:   true,
	}
	for _, spec := range []HarnessProbeSpec{DefaultFXProbeSpec(), DefaultClaudeCodeProbeSpec(), DefaultCodexProbeSpec(), DefaultOpenCodeProbeSpec()} {
		for _, capability := range spec.ActivityCapabilities {
			if !allowed[capability] {
				t.Fatalf("%s advertises activity not emitted by current adapter: %s", spec.Kind, capability)
			}
		}
	}
	for _, spec := range []HarnessProbeSpec{DefaultFXProbeSpec(), DefaultCodexProbeSpec()} {
		if !containsActivityCapability(spec.ActivityCapabilities, core.ActivityPermissionRequest) {
			t.Fatalf("%s must advertise permission requests for the ACP approval path", spec.Kind)
		}
		if !containsActivityCapability(spec.ActivityCapabilities, core.ActivityUserInputRequest) {
			t.Fatalf("%s must advertise user input requests for the ACP input path", spec.Kind)
		}
	}
}

func containsActivityCapability(capabilities []core.ActivityCapability, want core.ActivityCapability) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
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
	if got := NewDefaultHarnessProbes("macbook", nil); len(got) != 4 {
		t.Fatalf("default probes=%d", len(got))
	}
	foundOpenCode := false
	for _, probe := range NewDefaultHarnessProbes("macbook", nil) {
		foundOpenCode = foundOpenCode || probe.Spec.Kind == core.HarnessOpenCode
	}
	if !foundOpenCode {
		t.Fatal("OpenCode is missing from default probe set")
	}
	runner := fakeProbeRunner{responses: map[string]CommandResult{
		"opencode --version":                            {Stdout: "opencode v2.0.22"},
		"opencode auth list --format json --standalone": {Stdout: `[{"id":"openai","name":"OpenAI","connections":[{"type":"credential"}]}]`},
		"opencode models":                               {Stdout: "openai/model-a"},
	}}
	open := NewOpenCodeCompatibilityProbe("macbook", runner).Probe(context.Background())
	if open.Instance.Kind != core.HarnessOpenCode || !open.Available() {
		t.Fatalf("OpenCode v2 ACP result=%#v", open)
	}
	failedACP := NewOpenCodeCompatibilityProbe("macbook", fakeProbeRunner{responses: runner.responses, acpError: errors.New("synthetic ACP failure")}).Probe(context.Background())
	if failedACP.Available() || failedACP.ErrorCode != "acp_unavailable" {
		t.Fatal("OpenCode was advertised ready after ACP initialize failed")
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
	envelope := WorkerEnvelope{WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", OriginalUserIntent: "inspect", HarnessInstance: instance, Model: "gpt-5-codex", Reasoning: "high", Profile: workerTemplateFixture(instance, "gpt-5-codex", "high")}
	inventory := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{instance}, ObservedAt: time.Now().UTC()}
	if err := envelope.ValidateAgainstInventory("macbook", inventory); err != nil {
		t.Fatal(err)
	}
	envelope.Model = "not-observed"
	envelope.Profile = workerTemplateFixture(instance, envelope.Model, envelope.Reasoning)
	if err := envelope.ValidateAgainstInventory("macbook", inventory); !errors.Is(err, core.ErrObservedPinUnavailable) {
		t.Fatalf("missing model pin err=%v", err)
	}
}

func TestNormalizeRuntimeActivitySanitizesNestedToolPayloads(t *testing.T) {
	metadata := core.ActivityMetadata{EventID: "event", Node: "node", HarnessInstanceID: "node/fx", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityToolCall, core.ActivityToolResult}}
	call, ok := normalizeRuntimeActivity(Activity{Kind: ActivityToolCall, Tool: "shell", Arguments: json.RawMessage(`{"command":"ls","nested":{"analysis":"raw-analysis","safe":"keep","items":[{"reasoning":"raw-reasoning"},{"thought":"raw-thought"},{"chain_of_thought":"raw-chain"}]}}`)}, metadata, capabilities)
	if !ok {
		t.Fatal("nested tool call was rejected")
	}
	callJSON, _ := json.Marshal(call)
	for _, forbidden := range []string{"raw-analysis", "raw-reasoning", "raw-thought", "raw-chain"} {
		if strings.Contains(string(callJSON), forbidden) {
			t.Fatalf("tool call leaked %q: %s", forbidden, callJSON)
		}
	}
	if !strings.Contains(string(callJSON), `"command":"ls"`) || !strings.Contains(string(callJSON), `"safe":"keep"`) {
		t.Fatalf("ordinary tool arguments were not preserved: %s", callJSON)
	}

	result, ok := normalizeRuntimeActivity(Activity{Kind: ActivityToolResult, Tool: "shell", Result: `{"status":"ok","nested":{"analysis":"raw-analysis-result","safe":"keep-result","thought":"raw-thought-result"}}`}, metadata, capabilities)
	if !ok {
		t.Fatal("nested tool result was rejected")
	}
	for _, forbidden := range []string{"raw-analysis-result", "raw-thought-result"} {
		if strings.Contains(result.ToolResult.Output, forbidden) {
			t.Fatalf("tool result leaked %q: %s", forbidden, result.ToolResult.Output)
		}
	}
	if !strings.Contains(result.ToolResult.Output, `"status":"ok"`) || !strings.Contains(result.ToolResult.Output, `"safe":"keep-result"`) {
		t.Fatalf("ordinary tool result was not preserved: %s", result.ToolResult.Output)
	}
	plain, ok := normalizeRuntimeActivity(Activity{Kind: ActivityToolResult, Tool: "shell", Result: "[INFO] complete"}, metadata, capabilities)
	if !ok || plain.ToolResult.Output != "[INFO] complete" {
		t.Fatalf("ordinary text result was not preserved: %#v ok=%v", plain, ok)
	}
}

func TestToolSanitizersPreserveMultilineContentWhileRedactingSecrets(t *testing.T) {
	command := "python3 - <<'PY'\n  print('safe block')\nTOKEN=synthetic-command-password\nPY"
	arguments, _ := json.Marshal(map[string]string{"command": command})
	sanitizedArguments, ok := SanitizeToolArguments(arguments)
	if !ok {
		t.Fatal("multiline command was rejected")
	}
	var decodedArguments map[string]string
	if err := json.Unmarshal(sanitizedArguments, &decodedArguments); err != nil {
		t.Fatal(err)
	}
	wantCommand := "python3 - <<'PY'\n  print('safe block')\nTOKEN=[redacted]\nPY"
	if decodedArguments["command"] != wantCommand || strings.Contains(decodedArguments["command"], "synthetic-command-password") {
		t.Fatalf("sanitized command lost whitespace or retained credential: %q", decodedArguments["command"])
	}

	structured := `{"content":"first line\n  indented second line\nTOKEN=synthetic-output-password"}`
	cleanOutput, ok := SanitizeToolResult(structured)
	if !ok {
		t.Fatal("structured multiline tool output was rejected")
	}
	var decodedOutput map[string]string
	if err := json.Unmarshal([]byte(cleanOutput), &decodedOutput); err != nil {
		t.Fatal(err)
	}
	wantOutput := "first line\n  indented second line\nTOKEN=[redacted]"
	if decodedOutput["content"] != wantOutput || strings.Contains(decodedOutput["content"], "synthetic-output-password") {
		t.Fatalf("sanitized structured output lost whitespace or retained credential: %q", decodedOutput["content"])
	}
}

func TestNormalizeRuntimeActivityPreservesOnlyAllowlistedToolFailureMetadata(t *testing.T) {
	metadata := core.ActivityMetadata{EventID: "failure-event", Node: "node", HarnessInstanceID: "node/fx", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityToolResult}}
	failure := core.ToolFailureMetadata{Category: core.ToolFailureCategoryHTTP, Code: core.ToolFailureCodeHTTPError, HTTPStatus: 503}
	activity, ok := NormalizeRuntimeActivity(Activity{
		Kind: ActivityToolResult, Tool: "web_fetch", Status: "failed",
		Arguments: json.RawMessage(`{"url":"https://example.invalid/?token=private-url-token"}`),
		Result:    "private raw output", Error: "private error text Bearer private-error-token",
		Failure: &failure,
	}, metadata, capabilities)
	if !ok || activity.ToolResult == nil || activity.ToolResult.Failure == nil {
		t.Fatalf("normalized failure=%#v ok=%v", activity, ok)
	}
	if *activity.ToolResult.Failure != failure {
		t.Fatalf("safe failure metadata=%#v want=%#v", activity.ToolResult.Failure, failure)
	}
	if activity.ToolResult.Error != "" || activity.ToolResult.Output != "" {
		t.Fatalf("free-form failure data crossed the adapter: %#v", activity.ToolResult)
	}
	encoded, err := json.Marshal(activity)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-url-token", "private raw output", "private error text", "private-error-token", `"arguments"`, `"error"`, `"output"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("normalized failure leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestNormalizeRuntimeActivityPreservesToolLifecycleAndSafePreview(t *testing.T) {
	metadata := core.ActivityMetadata{EventID: "event", Node: "node", HarnessInstanceID: "node/fx", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityToolCall, core.ActivityToolResult}}
	call, ok := normalizeRuntimeActivity(Activity{Kind: ActivityToolCall, Tool: "read", Arguments: json.RawMessage(`{"path":"/worker/project/src/config.toml"}`)}, metadata, capabilities, "/worker/project")
	if !ok || call.ToolCall.Name != "read" || call.ToolCall.Preview != "src/config.toml" {
		t.Fatalf("normalized tool call=%#v ok=%v", call, ok)
	}
	finished, ok := normalizeRuntimeActivity(Activity{Kind: ActivityToolResult, Tool: "bash", Arguments: json.RawMessage(`{"command":"make test"}`), Status: "failed", Error: "synthetic failure"}, metadata, capabilities)
	if !ok || finished.ToolResult.Name != "bash" || finished.ToolResult.Status != "failed" || finished.ToolResult.Preview != "make test" || finished.ToolResult.Output != "" {
		t.Fatalf("normalized tool failure=%#v ok=%v", finished, ok)
	}
	if activity, ok := normalizeRuntimeActivity(Activity{Kind: ActivityToolCall, Text: "Running"}, metadata, capabilities); ok || activity.Kind != "" {
		t.Fatalf("progress title was used as missing tool identity: %#v", activity)
	}
}

func TestNormalizeRuntimeActivityNeverSynthesizesUnsupportedEvents(t *testing.T) {
	metadata := core.ActivityMetadata{EventID: "event", Node: "node", HarnessInstanceID: "node/fx", AttemptID: "attempt", Sequence: 1, ObservedAt: time.Now().UTC()}
	capabilities := core.HarnessCapabilities{Activity: []core.ActivityCapability{core.ActivityAssistantTextDelta, core.ActivityPermissionRequest, core.ActivityUserInputRequest}}
	if activity, ok := normalizeRuntimeActivity(Activity{Kind: ActivityText, Text: "hello"}, metadata, capabilities); !ok || activity.Kind != core.ActivityAssistantTextDelta {
		t.Fatalf("supported activity=%#v ok=%v", activity, ok)
	}
	permission, ok := normalizeRuntimeActivity(Activity{Kind: ActivityPermission, RequestID: "permission", Summary: "write"}, metadata, capabilities)
	if !ok || permission.Kind != core.ActivityPermissionRequest || permission.Request == nil || permission.Request.RequestID != "permission" {
		t.Fatalf("permission activity=%#v ok=%v", permission, ok)
	}
	input, ok := normalizeRuntimeActivity(Activity{Kind: ActivityUserInput, RequestID: "input", Summary: "question"}, metadata, capabilities)
	if !ok || input.Kind != core.ActivityUserInputRequest || input.Request == nil || input.Request.RequestID != "input" {
		t.Fatalf("input activity=%#v ok=%v", input, ok)
	}
	for _, item := range []Activity{{Kind: ActivityTool, Text: "shell"}, {Kind: ActivityStatus, Text: "working"}, {Kind: ActivityKind("unknown"), Text: "fake"}} {
		if activity, ok := normalizeRuntimeActivity(item, metadata, capabilities); ok || activity.Kind != "" {
			t.Fatalf("synthetic activity=%#v ok=%v", activity, ok)
		}
	}
}
