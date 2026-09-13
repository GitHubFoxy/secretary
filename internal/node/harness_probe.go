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

const defaultProbeStepTimeout = 10 * time.Second

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

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

// HarnessProbeSpec describes direct, local observations. ModelsOptional is for
// harnesses such as Claude Code that do not currently expose a documented model
// catalog command; such a harness may be ready with an empty observed model set,
// but explicit model pins remain rejected by the server contract.
type HarnessProbeSpec struct {
	Kind                  core.HarnessKind
	Binary                string
	VersionArgs           []string
	AuthenticationArgs    []string
	HealthArgs            []string
	ModelsArgs            []string
	ReasoningArgs         []string
	AuthenticationMethod  string
	ModelsOptional        bool
	StepTimeout           time.Duration
	ExecutionCapabilities []core.ExecutionCapability
	ActivityCapabilities  []core.ActivityCapability
}

func (s HarnessProbeSpec) validate() error {
	if s.Kind == "" || strings.TrimSpace(s.Binary) == "" {
		return errors.New("harness probe: kind and executable are required")
	}
	if len(s.VersionArgs) == 0 || len(s.AuthenticationArgs) == 0 {
		return fmt.Errorf("harness probe: %s requires version and authentication commands", s.Kind)
	}
	if len(s.ModelsArgs) == 0 && !s.ModelsOptional {
		return fmt.Errorf("harness probe: %s requires a model command", s.Kind)
	}
	return nil
}

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
func (r ProbeResult) Error() error    { return r.Err }

func (p HarnessProbe) Probe(ctx context.Context) ProbeResult {
	instance := core.HarnessInstance{ID: harnessInstanceID(p.Node, p.Spec.Kind), Node: p.Node, Kind: p.Spec.Kind, Status: core.HarnessUnavailable}
	result := ProbeResult{Instance: instance}
	if err := p.Spec.validate(); err != nil {
		result.Err, result.ErrorCode = err, "invalid_probe"
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
		result.Err, result.ErrorCode = ErrProbeMetadata, "version_unavailable"
		result.Instance = instance
		return result
	}

	auth, err := p.run(ctx, p.Spec.AuthenticationArgs)
	if err != nil || auth.ExitCode != 0 {
		result.Err, result.ErrorCode = probeError(ErrProbeUnauthenticated, err, "unauthenticated")
		result.Instance = instance
		return result
	}
	ok, method := parseAuthentication(p.Spec.Kind, auth.Stdout+"\n"+auth.Stderr, p.Spec.AuthenticationMethod)
	instance.Authentication = core.HarnessAuthentication{Authenticated: ok, Method: method}
	if !ok {
		result.Err, result.ErrorCode = ErrProbeUnauthenticated, "unauthenticated"
		result.Instance = instance
		return result
	}

	if len(p.Spec.HealthArgs) > 0 {
		health, healthErr := p.run(ctx, p.Spec.HealthArgs)
		if healthErr != nil || health.ExitCode != 0 || !healthOutputSaysReady(health.Stdout+"\n"+health.Stderr) {
			result.Err, result.ErrorCode = probeError(ErrProbeUnhealthy, healthErr, "unhealthy")
			result.Instance = instance
			return result
		}
	}

	var modelOutput string
	if len(p.Spec.ModelsArgs) > 0 {
		models, modelErr := p.run(ctx, p.Spec.ModelsArgs)
		if modelErr != nil || models.ExitCode != 0 {
			result.Err, result.ErrorCode = probeError(ErrProbeMetadata, modelErr, "models_probe_failed")
			instance.Status = core.HarnessDegraded
			result.Instance = instance
			return result
		}
		modelOutput = models.Stdout
		instance.ModelIDs = parseObservedModels(modelOutput)
		if len(instance.ModelIDs) == 0 && !p.Spec.ModelsOptional {
			result.Err, result.ErrorCode = ErrProbeMetadata, "models_unavailable"
			instance.Status = core.HarnessDegraded
			result.Instance = instance
			return result
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
			result.Err, result.ErrorCode = ErrProbeMetadata, "reasoning_unavailable"
			instance.Status = core.HarnessDegraded
			result.Instance = instance
			return result
		}
	} else if modelOutput != "" {
		instance.ReasoningLevels = parseObservedReasoning(modelOutput)
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
	timeout := p.Spec.StepTimeout
	if timeout <= 0 {
		timeout = defaultProbeStepTimeout
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := p.Runner.Run(stepCtx, p.Spec.Binary, args...)
	if errors.Is(stepCtx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("harness probe: %s %s timed out: %w", p.Spec.Binary, strings.Join(args, " "), context.DeadlineExceeded)
	}
	return result, err
}

func probeError(defaultErr, commandErr error, code string) (error, string) {
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

func parseAuthentication(kind core.HarnessKind, output, fallbackMethod string) (bool, string) {
	trimmed := strings.TrimSpace(output)
	lower := strings.ToLower(trimmed)
	if trimmed == "" || hasNegativeAuthMarker(lower) {
		return false, fallbackMethod
	}

	if kind == core.HarnessClaudeCode {
		var status map[string]any
		if json.Unmarshal([]byte(trimmed), &status) == nil {
			for _, key := range []string{"loggedIn", "authenticated", "isAuthenticated"} {
				if value, exists := status[key]; exists {
					loggedIn, ok := value.(bool)
					if !ok || !loggedIn {
						return false, observedAuthMethod(status, fallbackMethod)
					}
					return true, observedAuthMethod(status, fallbackMethod)
				}
			}
		}
	}

	if kind == core.HarnessCodex {
		switch {
		case strings.Contains(lower, "logged in using chatgpt"):
			return true, "chatgpt"
		case strings.Contains(lower, "logged in using an api key"), strings.Contains(lower, "logged in using api key"):
			return true, "api_key"
		case strings.Contains(lower, "logged in using agent identity"):
			return true, "agent_identity"
		}
	}

	if kind == core.HarnessOpenCode {
		return true, "provider"
	}
	if kind == core.HarnessFX {
		return true, "local"
	}
	return true, fallbackMethod
}

func observedAuthMethod(status map[string]any, fallback string) string {
	for _, key := range []string{"authMethod", "method", "subscriptionType", "accountType", "authType"} {
		if value, ok := status[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallback
}

func hasNegativeAuthMarker(lower string) bool {
	for _, marker := range []string{"not authenticated", "unauthenticated", "not logged", "login required", "no credentials", "logged out"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func authenticationOutputSaysReady(output string) bool {
	return strings.TrimSpace(output) != "" && !hasNegativeAuthMarker(strings.ToLower(output))
}

func healthOutputSaysReady(output string) bool {
	lower := strings.ToLower(strings.TrimSpace(output))
	if lower == "" {
		return false
	}
	for _, marker := range []string{"unhealthy", "degraded", "not ready", "failed", "offline"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func parseObservedModels(output string) []core.ObservedModelID {
	if records := decodeModelRecords(output); len(records) > 0 {
		seen := map[core.ObservedModelID]struct{}{}
		models := make([]core.ObservedModelID, 0, len(records))
		for _, record := range records {
			if !modelRecordSelectable(record) {
				continue
			}
			value, _ := record["slug"].(string)
			if strings.TrimSpace(value) == "" {
				value, _ = record["id"].(string)
			}
			observed := core.ObservedModelID(strings.TrimSpace(value))
			if observed == "" || isModelAlias(string(observed)) {
				continue
			}
			if _, ok := seen[observed]; !ok {
				seen[observed] = struct{}{}
				models = append(models, observed)
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
	if records := decodeModelRecords(output); len(records) > 0 {
		seen := map[core.ObservedReasoningLevel]struct{}{}
		levels := make([]core.ObservedReasoningLevel, 0)
		add := func(value string) {
			level := core.ObservedReasoningLevel(strings.TrimSpace(value))
			if level == "" {
				return
			}
			if _, ok := seen[level]; !ok {
				seen[level] = struct{}{}
				levels = append(levels, level)
			}
		}
		for _, record := range records {
			if !modelRecordSelectable(record) {
				continue
			}
			if value, ok := record["default_reasoning_level"].(string); ok {
				add(value)
			}
			if raw, ok := record["supported_reasoning_levels"].([]any); ok {
				for _, item := range raw {
					switch value := item.(type) {
					case string:
						add(value)
					case map[string]any:
						if effort, ok := value["effort"].(string); ok {
							add(effort)
						}
					}
				}
			}
		}
		return levels
	}

	seen := map[core.ObservedReasoningLevel]struct{}{}
	levels := make([]core.ObservedReasoningLevel, 0)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		matched := false
		for _, prefix := range []string{"reasoning", "levels"} {
			if strings.HasPrefix(lower, prefix+":") {
				line = strings.TrimSpace(line[len(prefix)+1:])
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, value := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ';' || r == '|' }) {
			level := core.ObservedReasoningLevel(strings.Trim(strings.TrimSpace(value), "\"'"))
			if level == "" {
				continue
			}
			if _, ok := seen[level]; !ok {
				seen[level] = struct{}{}
				levels = append(levels, level)
			}
		}
	}
	return levels
}

func decodeModelRecords(output string) []map[string]any {
	var root any
	if json.Unmarshal([]byte(output), &root) != nil {
		return nil
	}
	var items []any
	switch value := root.(type) {
	case []any:
		items = value
	case map[string]any:
		if models, ok := value["models"].([]any); ok {
			items = models
		}
	}
	records := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record, ok := item.(map[string]any); ok {
			records = append(records, record)
		}
	}
	return records
}

func modelRecordSelectable(record map[string]any) bool {
	for _, key := range []string{"available", "enabled", "selectable"} {
		if value, ok := record[key].(bool); ok && !value {
			return false
		}
	}
	if value, ok := record["hidden"].(bool); ok && value {
		return false
	}
	if value, ok := record["visibility"].(string); ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "hidden", "unavailable", "disabled", "internal":
			return false
		}
	}
	return true
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

func NewDefaultHarnessProbes(node core.NodeReference, runner CommandRunner) []HarnessProbe {
	return []HarnessProbe{
		{Node: node, Runner: runner, Spec: DefaultFXProbeSpec()},
		{Node: node, Runner: runner, Spec: DefaultClaudeCodeProbeSpec()},
		{Node: node, Runner: runner, Spec: DefaultCodexProbeSpec()},
	}
}

func NewOpenCodeCompatibilityProbe(node core.NodeReference, runner CommandRunner) HarnessProbe {
	return HarnessProbe{Node: node, Runner: runner, Spec: DefaultOpenCodeProbeSpec()}
}

var observedRuntimeActivity = []core.ActivityCapability{
	core.ActivityAssistantTextDelta,
	core.ActivityToolCall,
	core.ActivityStatus,
	core.ActivityAttemptOutcome,
}

var observedACPActivity = append(append([]core.ActivityCapability(nil), observedRuntimeActivity...), core.ActivityThinkingSummary, core.ActivityToolResult)

func DefaultFXProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{
		Kind: core.HarnessFX, Binary: "fx", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"models"}, ModelsArgs: []string{"models"}, AuthenticationMethod: "local",
		ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel},
		ActivityCapabilities:  append([]core.ActivityCapability(nil), observedACPActivity...),
	}
}

func DefaultClaudeCodeProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{
		Kind: core.HarnessClaudeCode, Binary: "claude", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"auth", "status"}, ModelsOptional: true,
		ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel},
		ActivityCapabilities:  append([]core.ActivityCapability(nil), observedRuntimeActivity...),
	}
}

func DefaultCodexProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{
		Kind: core.HarnessCodex, Binary: "codex", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"login", "status"}, HealthArgs: []string{"doctor"}, ModelsArgs: []string{"debug", "models"}, ReasoningArgs: []string{"debug", "models"},
		ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel, core.CapabilitySteering},
		ActivityCapabilities:  append([]core.ActivityCapability(nil), observedACPActivity...),
	}
}

func DefaultOpenCodeProbeSpec() HarnessProbeSpec {
	return HarnessProbeSpec{
		Kind: core.HarnessOpenCode, Binary: "opencode", VersionArgs: []string{"--version"}, AuthenticationArgs: []string{"auth", "list"}, ModelsArgs: []string{"models"},
		ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel, core.CapabilitySteering},
		ActivityCapabilities:  append([]core.ActivityCapability(nil), observedRuntimeActivity...),
	}
}

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
