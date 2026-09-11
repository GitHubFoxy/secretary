package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var (
	ErrProbeMissing         = errors.New("harness probe: executable is missing")
	ErrProbeUnauthenticated = errors.New("harness probe: harness is not authenticated")
	ErrProbeUnhealthy       = errors.New("harness probe: harness is unhealthy")
	ErrProbeMetadata        = errors.New("harness probe: metadata probe failed")
)

// CommandResult is deliberately small so probe tests can fake exact local
// command output without starting a harness process.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// CommandRunner is the only process boundary used by discovery. Production
// uses ExecCommandRunner; tests can provide a deterministic implementation.
type CommandRunner interface {
	Run(context.Context, string, ...string) (CommandResult, error)
}

type CommandRunnerFunc func(context.Context, string, ...string) (CommandResult, error)

func (f CommandRunnerFunc) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	return f(ctx, name, args...)
}

type ExecCommandRunner struct{}

type LocalCommandRunner = ExecCommandRunner

func (ExecCommandRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	result := CommandResult{Stdout: string(output), ExitCode: 0}
	if err == nil {
		return result, nil
	}
	result.ExitCode = -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		result.Stderr = string(exitErr.Stderr)
	}
	return result, err
}

// HarnessProbeSpec describes the direct commands and observed capabilities of
// one adapter. It is configuration, not a server-side model catalog.
type HarnessProbeSpec struct {
	Kind                  core.HarnessKind
	Binary                string
	VersionArgs           []string
	AuthenticationArgs    []string
	HealthArgs            []string
	ModelsArgs            []string
	ReasoningArgs         []string
	AuthenticationMethod  string
	ExecutionCapabilities []core.ExecutionCapability
	ActivityCapabilities  []core.ActivityCapability
}

func (s HarnessProbeSpec) validate() error {
	if s.Kind == "" || strings.TrimSpace(s.Binary) == "" {
		return errors.New("harness probe: kind and executable are required")
	}
	if len(s.VersionArgs) == 0 || len(s.AuthenticationArgs) == 0 || len(s.ModelsArgs) == 0 {
		return fmt.Errorf("harness probe: %s requires version, authentication and model commands", s.Kind)
	}
	return nil
}

// HarnessProbe is a deterministic local adapter probe. It reports an explicit
// unavailable/degraded instance instead of selecting another harness.
type HarnessProbe struct {
	Node   core.NodeReference
	Spec   HarnessProbeSpec
	Runner CommandRunner
}

type ProbeResult struct {
	Instance  core.HarnessInstance
	ErrorCode string
	Err       error
}

func (r ProbeResult) Available() bool { return r.Err == nil && r.Instance.Available() }

func (r ProbeResult) Error() error { return r.Err }

func (p HarnessProbe) Probe(ctx context.Context) ProbeResult {
	instance := core.HarnessInstance{
		ID:             harnessInstanceID(p.Node, p.Spec.Kind),
		Node:           p.Node,
		Kind:           p.Spec.Kind,
		Authentication: core.HarnessAuthentication{Method: p.Spec.AuthenticationMethod},
		Status:         core.HarnessUnavailable,
	}
	result := ProbeResult{Instance: instance}
	if err := p.Spec.validate(); err != nil {
		result.Err = err
		result.ErrorCode = "invalid_probe"
		return result
	}
	if p.Runner == nil {
		p.Runner = ExecCommandRunner{}
	}

	version, err := p.run(ctx, p.Spec.VersionArgs)
	if err != nil || version.ExitCode != 0 {
		result.Err, result.ErrorCode = probeError(ErrProbeMissing, err, "missing")
		return result
	}
	instance.Version = parseVersion(version.Stdout)
	if instance.Version == "" {
		result.Err = ErrProbeMetadata
		result.ErrorCode = "version_unavailable"
		return result
	}

	auth, err := p.run(ctx, p.Spec.AuthenticationArgs)
	if err != nil || auth.ExitCode != 0 || !authenticationOutputSaysReady(auth.Stdout+"\n"+auth.Stderr) {
		result.Err, result.ErrorCode = probeError(ErrProbeUnauthenticated, err, "unauthenticated")
		instance.Authentication.Authenticated = false
		result.Instance = instance
		return result
	}
	instance.Authentication.Authenticated = true

	if len(p.Spec.HealthArgs) > 0 {
		health, healthErr := p.run(ctx, p.Spec.HealthArgs)
		if healthErr != nil || health.ExitCode != 0 || !healthOutputSaysReady(health.Stdout+"\n"+health.Stderr) {
			result.Err, result.ErrorCode = probeError(ErrProbeUnhealthy, healthErr, "unhealthy")
			instance.Status = core.HarnessUnavailable
			result.Instance = instance
			return result
		}
	}

	models, err := p.run(ctx, p.Spec.ModelsArgs)
	if err != nil || models.ExitCode != 0 {
		result.Err, result.ErrorCode = probeError(ErrProbeMetadata, err, "models_probe_failed")
		instance.Status = core.HarnessDegraded
		result.Instance = instance
		return result
	}
	instance.ModelIDs = parseObservedModels(models.Stdout)
	if len(instance.ModelIDs) == 0 {
		result.Err = ErrProbeMetadata
		result.ErrorCode = "models_unavailable"
		instance.Status = core.HarnessDegraded
		result.Instance = instance
		return result
	}

	if len(p.Spec.ReasoningArgs) == 0 {
		// Some CLIs expose reasoning support beside model IDs in one catalog.
		// An empty result remains an honest "no observed levels" state.
		if json.Valid([]byte(models.Stdout)) || strings.Contains(strings.ToLower(models.Stdout), "reasoning") {
			instance.ReasoningLevels = parseObservedReasoning(models.Stdout)
		}
	}
	if len(p.Spec.ReasoningArgs) > 0 {
		reasoning, reasoningErr := p.run(ctx, p.Spec.ReasoningArgs)
		if reasoningErr != nil || reasoning.ExitCode != 0 {
			result.Err, result.ErrorCode = probeError(ErrProbeMetadata, reasoningErr, "reasoning_probe_failed")
			instance.Status = core.HarnessDegraded
			result.Instance = instance
			return result
		}
		instance.ReasoningLevels = parseObservedReasoning(reasoning.Stdout)
		if len(instance.ReasoningLevels) == 0 {
			result.Err = ErrProbeMetadata
			result.ErrorCode = "reasoning_unavailable"
			instance.Status = core.HarnessDegraded
			result.Instance = instance
			return result
		}
	}

	instance.Capabilities = core.HarnessCapabilities{
		Execution: append([]core.ExecutionCapability(nil), p.Spec.ExecutionCapabilities...),
		Activity:  append([]core.ActivityCapability(nil), p.Spec.ActivityCapabilities...),
	}
	instance.Status = core.HarnessReady
	result.Instance = instance
	return result
}

func (p HarnessProbe) run(ctx context.Context, args []string) (CommandResult, error) {
	return p.Runner.Run(ctx, p.Spec.Binary, args...)
}

func probeError(defaultErr error, commandErr error, code string) (error, string) {
	if commandErr != nil {
		return fmt.Errorf("%w: %v", defaultErr, commandErr), code
	}
	return defaultErr, code
}

var versionPattern = regexp.MustCompile(`(?i)\bv?([0-9]+(?:\.[0-9]+)+(?:[-+][0-9A-Za-z.-]+)?)\b`)

func parseVersion(output string) string {
	if match := versionPattern.FindStringSubmatch(output); len(match) == 2 {
		return match[1]
	}
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func authenticationOutputSaysReady(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	for _, marker := range []string{"not authenticated", "unauthenticated", "not logged", "login required", "no credentials", "logged out", "unhealthy", "degraded"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func healthOutputSaysReady(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	for _, marker := range []string{"unhealthy", "degraded", "not ready", "failed", "offline"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func parseObservedModels(output string) []core.ObservedModelID {
	var catalog struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if json.Unmarshal([]byte(output), &catalog) == nil && len(catalog.Models) > 0 {
		seen := map[core.ObservedModelID]struct{}{}
		models := make([]core.ObservedModelID, 0, len(catalog.Models))
		for _, model := range catalog.Models {
			observed := core.ObservedModelID(strings.TrimSpace(model.Slug))
			if observed != "" && !isModelAlias(string(observed)) {
				if _, ok := seen[observed]; !ok {
					seen[observed] = struct{}{}
					models = append(models, observed)
				}
			}
		}
		return models
	}
	values := parseObservedValues(output, "models", "model")
	models := make([]core.ObservedModelID, 0, len(values))
	for _, value := range values {
		if !isModelAlias(value) {
			models = append(models, core.ObservedModelID(value))
		}
	}
	return models
}

func parseObservedReasoning(output string) []core.ObservedReasoningLevel {
	var catalog struct {
		Models []struct {
			Default string `json:"default_reasoning_level"`
			Levels  []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal([]byte(output), &catalog) == nil && len(catalog.Models) > 0 {
		seen := map[core.ObservedReasoningLevel]struct{}{}
		levels := make([]core.ObservedReasoningLevel, 0)
		for _, model := range catalog.Models {
			if model.Default != "" {
				level := core.ObservedReasoningLevel(model.Default)
				if _, ok := seen[level]; !ok {
					seen[level] = struct{}{}
					levels = append(levels, level)
				}
			}
			for _, observed := range model.Levels {
				if observed.Effort != "" {
					level := core.ObservedReasoningLevel(observed.Effort)
					if _, ok := seen[level]; !ok {
						seen[level] = struct{}{}
						levels = append(levels, level)
					}
				}
			}
		}
		return levels
	}
	values := parseObservedValues(output, "reasoning", "levels")
	levels := make([]core.ObservedReasoningLevel, 0, len(values))
	for _, value := range values {
		levels = append(levels, core.ObservedReasoningLevel(value))
	}
	return levels
}

func parseObservedValues(output string, prefixes ...string) []string {
	seen := map[string]struct{}{}
	values := make([]string, 0)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		for _, prefix := range prefixes {
			if strings.HasPrefix(lower, prefix+":") {
				line = strings.TrimSpace(line[len(prefix)+1:])
				break
			}
		}
		line = strings.TrimSpace(strings.Trim(line, "[]\"'"))
		line = strings.TrimPrefix(line, "-")
		if strings.Contains(line, " · ") {
			line = strings.TrimSpace(strings.SplitN(line, " · ", 2)[0])
		}
		if strings.Contains(strings.ToLower(line), "available") && strings.HasPrefix(strings.ToLower(line), "models") {
			continue
		}
		for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
			value := strings.Trim(strings.TrimSpace(part), "\"'")
			if value == "" || strings.HasPrefix(value, "#") || strings.HasPrefix(value, "-") {
				continue
			}
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				values = append(values, value)
			}
		}
	}
	return values
}

func harnessInstanceID(node core.NodeReference, kind core.HarnessKind) core.HarnessInstanceID {
	name := string(kind)
	if kind == core.HarnessClaudeCode {
		name = "claude"
	}
	return core.HarnessInstanceID(string(node) + "/" + name)
}

func ProbeHarness(ctx context.Context, node core.NodeReference, spec HarnessProbeSpec, runner CommandRunner) ProbeResult {
	return (HarnessProbe{Node: node, Spec: spec, Runner: runner}).Probe(ctx)
}

func ProbeFX(ctx context.Context, node core.NodeReference, runner CommandRunner) ProbeResult {
	return ProbeHarness(ctx, node, DefaultFXProbeSpec(), runner)
}

func ProbeClaudeCode(ctx context.Context, node core.NodeReference, runner CommandRunner) ProbeResult {
	return ProbeHarness(ctx, node, DefaultClaudeCodeProbeSpec(), runner)
}

func ProbeCodex(ctx context.Context, node core.NodeReference, runner CommandRunner) ProbeResult {
	return ProbeHarness(ctx, node, DefaultCodexProbeSpec(), runner)
}

func ProbeOpenCode(ctx context.Context, node core.NodeReference, runner CommandRunner) ProbeResult {
	return ProbeHarness(ctx, node, DefaultOpenCodeProbeSpec(), runner)
}

// NewDefaultHarnessProbes returns the mandatory MVP probes only.
func NewDefaultHarnessProbes(node core.NodeReference, runner CommandRunner) []HarnessProbe {
	return []HarnessProbe{
		{Node: node, Runner: runner, Spec: DefaultFXProbeSpec()},
		{Node: node, Runner: runner, Spec: DefaultClaudeCodeProbeSpec()},
		{Node: node, Runner: runner, Spec: DefaultCodexProbeSpec()},
	}
}

// NewOpenCodeCompatibilityProbe is intentionally separate from mandatory MVP
// discovery and cannot become an implicit fallback.
func NewOpenCodeCompatibilityProbe(node core.NodeReference, runner CommandRunner) HarnessProbe {
	return HarnessProbe{Node: node, Runner: runner, Spec: DefaultOpenCodeProbeSpec()}
}

func DefaultFXProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{Kind: core.HarnessFX, Binary: "fx", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"models"}, ModelsArgs: []string{"models"}, AuthenticationMethod: "local", ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel}, ActivityCapabilities: []core.ActivityCapability{core.ActivitySessionStarted, core.ActivityAssistantTextDelta, core.ActivityStatus, core.ActivityAttemptOutcome}}
}

func DefaultClaudeCodeProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{Kind: core.HarnessClaudeCode, Binary: "claude", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"auth", "status"}, ModelsArgs: []string{"models"}, AuthenticationMethod: "oauth", ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel, core.CapabilitySteering, core.CapabilityApprovals}, ActivityCapabilities: []core.ActivityCapability{core.ActivitySessionStarted, core.ActivityThinkingSummary, core.ActivityAssistantTextDelta, core.ActivityToolCall, core.ActivityToolResult, core.ActivityPermissionRequest, core.ActivityStatus, core.ActivityAttemptOutcome}}
}

func DefaultCodexProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{Kind: core.HarnessCodex, Binary: "codex", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"login", "status"}, HealthArgs: []string{"doctor"}, ModelsArgs: []string{"debug", "models"}, ReasoningArgs: []string{"debug", "models"}, AuthenticationMethod: "api_key", ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel, core.CapabilitySteering}, ActivityCapabilities: []core.ActivityCapability{core.ActivitySessionStarted, core.ActivityAssistantTextDelta, core.ActivityToolCall, core.ActivityToolResult, core.ActivityStatus, core.ActivityAttemptOutcome}}
}

func DefaultOpenCodeProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{Kind: core.HarnessOpenCode, Binary: "opencode", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"auth", "list"}, ModelsArgs: []string{"models"}, AuthenticationMethod: "provider", ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel, core.CapabilitySteering}, ActivityCapabilities: []core.ActivityCapability{core.ActivitySessionStarted, core.ActivityAssistantTextDelta, core.ActivityToolCall, core.ActivityToolResult, core.ActivityStatus, core.ActivityAttemptOutcome}}
}

// HarnessDiscovery is an InventorySource backed by local adapter probes.
type HarnessDiscovery struct {
	Node            core.NodeReference
	Runner          CommandRunner
	Probes          []HarnessProbe
	IncludeOpenCode bool
	Now             func() time.Time
}

func (d HarnessDiscovery) Discover(ctx context.Context) (core.HarnessInventorySnapshot, error) {
	if strings.TrimSpace(string(d.Node)) == "" {
		return core.HarnessInventorySnapshot{}, errors.New("harness discovery: Node is required")
	}
	probes := append([]HarnessProbe(nil), d.Probes...)
	if len(probes) == 0 {
		probes = NewDefaultHarnessProbes(d.Node, d.Runner)
	}
	if d.IncludeOpenCode {
		probes = append(probes, NewOpenCodeCompatibilityProbe(d.Node, d.Runner))
	}
	instances := make([]core.HarnessInstance, 0, len(probes))
	for _, probe := range probes {
		instances = append(instances, probe.Probe(ctx).Instance)
	}
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	return core.HarnessInventorySnapshot{Node: d.Node, Instances: instances, ObservedAt: now().UTC()}, nil
}

// DiscoverHarnessInventory probes the mandatory adapters and, when requested,
// the separate OpenCode compatibility adapter. Every probe stays in inventory,
// including unavailable instances.
func DiscoverHarnessInventory(ctx context.Context, node core.NodeReference, runner CommandRunner, includeOpenCode bool) core.HarnessInventorySnapshot {
	inventory, _ := (HarnessDiscovery{Node: node, Runner: runner, IncludeOpenCode: includeOpenCode}).Discover(ctx)
	return inventory
}

func DiscoverRequiredHarnessInventory(ctx context.Context, node core.NodeReference, runner CommandRunner) core.HarnessInventorySnapshot {
	return DiscoverHarnessInventory(ctx, node, runner, false)
}

func DiscoverAllHarnessInventory(ctx context.Context, node core.NodeReference, runner CommandRunner) core.HarnessInventorySnapshot {
	return DiscoverHarnessInventory(ctx, node, runner, true)
}
