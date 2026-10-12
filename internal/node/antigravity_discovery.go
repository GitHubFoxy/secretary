package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

type AntigravityObserver interface {
	ObserveAntigravity(ctx context.Context, command string) (string, bool, string, []core.ObservedModelID, []core.ObservedReasoningLevel, string, error)
}

type antigravityCacheKey struct {
	node   core.NodeReference
	binary string
}

type antigravityCacheEntry struct {
	cachedAt       time.Time
	execMtime      time.Time
	companionMtime time.Time
	result         ProbeResult
}

var (
	antigravityCacheMu  sync.Mutex
	antigravityCache    = make(map[antigravityCacheKey]antigravityCacheEntry)
	antigravityCacheTTL = 5 * time.Minute
)

func ResetAntigravityProbeCache() {
	antigravityCacheMu.Lock()
	defer antigravityCacheMu.Unlock()
	antigravityCache = make(map[antigravityCacheKey]antigravityCacheEntry)
}

func DefaultAntigravityProbeSpec() HarnessProbeSpec {
	binary := "agy_acp_server"
	if runtime.GOOS == "windows" {
		binary = "agy_acp_server.exe"
	}
	return HarnessProbeSpec{
		Kind:                  core.HarnessAntigravity,
		Binary:                binary,
		VersionArgs:           []string{"--version"},
		AuthenticationArgs:    []string{"auth", "status"},
		ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel},
		ActivityCapabilities:  append([]core.ActivityCapability(nil), observedACPActivity...),
	}
}

func NewAntigravityProbe(node core.NodeReference, runner CommandRunner) HarnessProbe {
	return HarnessProbe{Node: node, Runner: runner, Spec: DefaultAntigravityProbeSpec()}
}

func (p HarnessProbe) probeAntigravity(ctx context.Context) ProbeResult {
	instance := core.HarnessInstance{
		ID:     harnessInstanceID(p.Node, p.Spec.Kind),
		Node:   p.Node,
		Kind:   p.Spec.Kind,
		Status: core.HarnessUnavailable,
	}
	result := ProbeResult{Instance: instance}

	realExec, companion, resolveErr := ResolveAntigravityExecutable(p.Spec.Binary)
	if resolveErr != nil {
		if errors.Is(resolveErr, ErrAntigravityCompanionMissing) || strings.Contains(resolveErr.Error(), "companion") {
			result.ErrorCode = "companion_missing"
			result.Err = resolveErr
			result.Instance.ErrorCode = "companion_missing"
			result.Instance.DiagnosticReason = resolveErr.Error()
			return result
		}
		result.ErrorCode = "missing"
		result.Err = ErrProbeMissing
		result.Instance.ErrorCode = "missing"
		result.Instance.DiagnosticReason = resolveErr.Error()
		return result
	}

	execStat, err1 := os.Stat(realExec)
	compStat, err2 := os.Stat(companion)
	if err1 != nil || err2 != nil {
		result.ErrorCode = "missing"
		result.Err = ErrProbeMissing
		result.Instance.ErrorCode = "missing"
		result.Instance.DiagnosticReason = "antigravity: executable or companion inaccessible"
		return result
	}

	execMtime := execStat.ModTime()
	compMtime := compStat.ModTime()

	cacheKey := antigravityCacheKey{node: p.Node, binary: realExec}
	antigravityCacheMu.Lock()
	if entry, found := antigravityCache[cacheKey]; found {
		if time.Since(entry.cachedAt) < antigravityCacheTTL &&
			entry.execMtime.Equal(execMtime) &&
			entry.companionMtime.Equal(compMtime) {
			antigravityCacheMu.Unlock()
			return entry.result
		}
	}
	antigravityCacheMu.Unlock()

	var version, authMethod, diagnosticCode string
	var authenticated bool
	var modelIDs []core.ObservedModelID
	var reasoning []core.ObservedReasoningLevel
	var probeErr error

	if observer, ok := p.Runner.(AntigravityObserver); ok {
		version, authenticated, authMethod, modelIDs, reasoning, diagnosticCode, probeErr = observer.ObserveAntigravity(ctx, p.Spec.Binary)
	} else {
		version, authenticated, authMethod, modelIDs, reasoning, diagnosticCode, probeErr = observeAntigravityACP(ctx, realExec, companion)
	}

	if probeErr != nil || !authenticated {
		if diagnosticCode == "" {
			if !authenticated {
				diagnosticCode = "unauthenticated"
			} else {
				diagnosticCode = "unhealthy"
			}
		}
		result.ErrorCode = diagnosticCode
		result.Err = probeErr
		if result.Err == nil {
			result.Err = ErrProbeUnauthenticated
		}
		result.Instance.Status = core.HarnessUnavailable
		result.Instance.ErrorCode = diagnosticCode
		result.Instance.Version = version
		result.Instance.Authentication = core.HarnessAuthentication{
			Authenticated: authenticated,
			Method:        authMethod,
		}
		if result.Err != nil {
			result.Instance.DiagnosticReason = result.Err.Error()
		}
		return result
	}

	result.Instance.Status = core.HarnessReady
	result.Instance.Version = version
	result.Instance.Authentication = core.HarnessAuthentication{
		Authenticated: true,
		Method:        authMethod,
	}
	result.Instance.ModelIDs = modelIDs
	result.Instance.ReasoningLevels = reasoning
	result.Instance.Capabilities = core.HarnessCapabilities{
		Execution: append([]core.ExecutionCapability(nil), p.Spec.ExecutionCapabilities...),
		Activity:  append([]core.ActivityCapability(nil), p.Spec.ActivityCapabilities...),
	}

	antigravityCacheMu.Lock()
	antigravityCache[cacheKey] = antigravityCacheEntry{
		cachedAt:       time.Now(),
		execMtime:      execMtime,
		companionMtime: compMtime,
		result:         result,
	}
	antigravityCacheMu.Unlock()

	return result
}

func observeAntigravityACP(ctx context.Context, execPath, companionPath string) (string, bool, string, []core.ObservedModelID, []core.ObservedReasoningLevel, string, error) {
	tempDir, err := os.MkdirTemp("", "secretary-antigravity-probe-")
	if err != nil {
		return "", false, "", nil, nil, "probe_failed", fmt.Errorf("antigravity: probe temp dir unavailable: %w", err)
	}
	defer os.RemoveAll(tempDir)

	profileDir := filepath.Join(tempDir, "gemini_home")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return "", false, "", nil, nil, "probe_failed", fmt.Errorf("antigravity: profile dir unavailable: %w", err)
	}

	env := BuildAntigravityEnvironment(os.Environ(), companionPath, profileDir, tempDir)
	args := BuildAntigravityPlatformArgs(nil)

	client, err := acp.StartWithLogEnvDir(ctx, nil, env, tempDir, execPath, args...)
	if err != nil {
		return "", false, "", nil, nil, "missing", fmt.Errorf("antigravity: ACP start failed: %w", err)
	}
	defer client.Close()

	var init struct {
		ProtocolVersion int `json:"protocolVersion"`
		AgentInfo       struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
		AuthMethods []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"authMethods"`
	}

	if err := client.Request(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]string{"name": "secretary-probe", "version": "1.0"},
		"clientCapabilities": acpClientCapabilities(),
	}, &init); err != nil {
		return "", false, "", nil, nil, "protocol_mismatch", fmt.Errorf("antigravity: ACP initialize failed: %w", err)
	}

	if init.ProtocolVersion != 1 {
		return init.AgentInfo.Version, false, "", nil, nil, "protocol_mismatch", fmt.Errorf("antigravity: unsupported protocol version %d", init.ProtocolVersion)
	}

	version := init.AgentInfo.Version
	if version == "" {
		version = "1.3.0"
	}

	var created struct {
		SessionID     string                `json:"sessionId"`
		ConfigOptions []nativeConfigOption `json:"configOptions"`
	}

	newParams := map[string]any{
		"cwd":        tempDir,
		"mcpServers": []MCPServer{},
	}

	if err := client.Request(ctx, "session/new", newParams, &created); err != nil || created.SessionID == "" {
		diag := "unauthenticated"
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "protocol") {
			diag = "protocol_mismatch"
		}
		return version, false, "", nil, nil, diag, fmt.Errorf("antigravity: session/new failed: %w", err)
	}

	authMethod := "oauth-personal"
	if len(init.AuthMethods) > 0 {
		authMethod = init.AuthMethods[0].ID
	}

	models := parseAntigravityConfigModels(created.ConfigOptions)
	reasoning := parseAntigravityConfigReasoning(created.ConfigOptions)

	return version, true, authMethod, models, reasoning, "", nil
}

func parseAntigravityConfigModels(options []nativeConfigOption) []core.ObservedModelID {
	seen := make(map[string]bool)
	var models []core.ObservedModelID
	for _, opt := range options {
		if strings.EqualFold(opt.ID, "model") || strings.Contains(strings.ToLower(opt.ID), "model") {
			for _, choice := range opt.Options {
				val := strings.TrimSpace(choice.Value)
				if val != "" && !seen[val] && !isModelAlias(val) {
					seen[val] = true
					models = append(models, core.ObservedModelID(val))
				}
			}
			if opt.CurrentValue != "" && !seen[opt.CurrentValue] && !isModelAlias(opt.CurrentValue) {
				seen[opt.CurrentValue] = true
				models = append(models, core.ObservedModelID(opt.CurrentValue))
			}
		}
	}
	return models
}

func parseAntigravityConfigReasoning(options []nativeConfigOption) []core.ObservedReasoningLevel {
	seen := make(map[string]bool)
	var levels []core.ObservedReasoningLevel
	for _, opt := range options {
		idLower := strings.ToLower(opt.ID)
		if strings.Contains(idLower, "reason") || strings.Contains(idLower, "thought") || strings.Contains(idLower, "thinking") {
			for _, choice := range opt.Options {
				val := strings.TrimSpace(choice.Value)
				if val != "" && !seen[val] {
					seen[val] = true
					levels = append(levels, core.ObservedReasoningLevel(val))
				}
			}
			if opt.CurrentValue != "" && !seen[opt.CurrentValue] {
				seen[opt.CurrentValue] = true
				levels = append(levels, core.ObservedReasoningLevel(opt.CurrentValue))
			}
		}
	}
	return levels
}
