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
	node       core.NodeReference
	binary     string
	profileDir string
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
		ProfileDir:            DefaultAntigravityProfileDir(),
		VersionArgs:           []string{"--version"},
		AuthenticationArgs:    []string{"auth", "status"},
		ExecutionCapabilities: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit, core.CapabilityCancel},
		ActivityCapabilities:  append([]core.ActivityCapability(nil), observedACPActivity...),
	}
}

func NewAntigravityProbe(node core.NodeReference, runner CommandRunner) HarnessProbe {
	spec := DefaultAntigravityProbeSpec()
	spec.ProfileDir = ResolveAntigravityProfileDir()
	return HarnessProbe{Node: node, Runner: runner, Spec: spec}
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

	profileDir := ResolveAntigravityProfileDir(p.Spec.ProfileDir)
	cacheKey := antigravityCacheKey{
		node:       p.Node,
		binary:     realExec,
		profileDir: profileDir,
	}

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
		version, authenticated, authMethod, modelIDs, reasoning, diagnosticCode, probeErr = observeAntigravityACP(ctx, realExec, companion, profileDir)
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

func observeAntigravityACP(ctx context.Context, execPath, companionPath, profileDir string) (string, bool, string, []core.ObservedModelID, []core.ObservedReasoningLevel, string, error) {
	profileDir = ResolveAntigravityProfileDir(profileDir)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return "", false, "", nil, nil, "probe_failed", fmt.Errorf("antigravity: profile dir unavailable: %w", err)
	}

	tempDir := filepath.Join(profileDir, "tmp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return "", false, "", nil, nil, "probe_failed", fmt.Errorf("antigravity: probe temp dir unavailable: %w", err)
	}

	env := BuildAntigravityEnvironment(os.Environ(), companionPath, profileDir, tempDir)
	args := BuildAntigravityPlatformArgs(nil)

	client, err := acp.StartWithLogEnvDir(ctx, nil, env, tempDir, execPath, args...)
	if err != nil {
		return "", false, "", nil, nil, "missing", fmt.Errorf("antigravity: ACP start failed: %w", err)
	}
	defer client.Close()

	var init struct {
		ProtocolVersion   int             `json:"protocolVersion"`
		AgentInfo         struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"agentInfo"`
		AgentCapabilities map[string]any  `json:"agentCapabilities"`
		AuthMethods       []acpAuthMethod `json:"authMethods"`
	}

	if err := client.Request(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]string{"name": "secretary-probe", "version": "1.0"},
		"clientCapabilities": acpClientCapabilities(),
	}, &init); err != nil {
		code, diagErr := classifyAntigravityError(err)
		return "", false, "", nil, nil, code, diagErr
	}

	if init.ProtocolVersion != 1 {
		return init.AgentInfo.Version, false, "", nil, nil, "protocol_mismatch", fmt.Errorf("antigravity: unsupported protocol version %d", init.ProtocolVersion)
	}

	version := init.AgentInfo.Version
	if version == "" {
		version = "1.3.0"
	}

	var confirmedAuthMethod string
	var created struct {
		SessionID     string                `json:"sessionId"`
		ConfigOptions []nativeConfigOption `json:"configOptions"`
	}

	newParams := map[string]any{
		"cwd":        tempDir,
		"mcpServers": []MCPServer{},
	}

	newErr := client.Request(ctx, "session/new", newParams, &created)
	if newErr != nil && isACPAuthRequired(newErr) {
		method, selectErr := selectAuthMethod(init.AuthMethods)
		if selectErr != nil {
			return version, false, "", nil, nil, "unauthenticated", fmt.Errorf("antigravity: authentication required: %w", selectErr)
		}
		if isTerminalAuthMethod(*method) {
			return version, false, "", nil, nil, "unauthenticated", fmt.Errorf("antigravity: login required via interactive terminal (%s); run interactive login or complete onboarding", method.ID)
		}
		var authResp map[string]any
		authErr := client.Request(ctx, "authenticate", map[string]any{
			"methodId": method.ID,
		}, &authResp)
		if authErr != nil {
			return version, false, "", nil, nil, "unauthenticated", fmt.Errorf("antigravity: authentication required for %s: %w", method.ID, authErr)
		}
		confirmedAuthMethod = method.ID
		newErr = client.Request(ctx, "session/new", newParams, &created)
	}

	if newErr != nil {
		code, diagErr := classifyAntigravityError(newErr)
		return version, false, "", nil, nil, code, diagErr
	}

	if created.SessionID == "" {
		return version, false, "", nil, nil, "session_setup_failed", fmt.Errorf("antigravity: session/new returned empty session ID")
	}

	models := parseAntigravityConfigModels(created.ConfigOptions)
	reasoning := parseAntigravityConfigReasoning(created.ConfigOptions)

	return version, true, confirmedAuthMethod, models, reasoning, "", nil
}

func classifyAntigravityError(err error) (string, error) {
	if err == nil {
		return "", nil
	}
	var rpcErr *acp.RPCError
	if errors.As(err, &rpcErr) {
		if isACPAuthRequired(rpcErr) {
			return "unauthenticated", fmt.Errorf("antigravity: authentication required: %s", rpcErr.Message)
		}
		switch rpcErr.Code {
		case -32600, -32601, -32602:
			return "protocol_mismatch", fmt.Errorf("antigravity: protocol mismatch (code %d): %s", rpcErr.Code, rpcErr.Message)
		}
		if rpcErr.Code == -32000 {
			return "session_setup_failed", fmt.Errorf("antigravity: ACP request failed (code %d): %s", rpcErr.Code, rpcErr.Message)
		}
		msgLower := strings.ToLower(rpcErr.Message)
		switch {
		case strings.Contains(msgLower, "auth") || strings.Contains(msgLower, "login") || strings.Contains(msgLower, "token") || strings.Contains(msgLower, "credential") || strings.Contains(msgLower, "unauthorized"):
			return "unauthenticated", fmt.Errorf("antigravity: authentication required: %s", rpcErr.Message)
		case strings.Contains(msgLower, "protocol") || strings.Contains(msgLower, "unsupported version") || strings.Contains(msgLower, "unknown method"):
			return "protocol_mismatch", fmt.Errorf("antigravity: protocol mismatch: %s", rpcErr.Message)
		case strings.Contains(msgLower, "mcp"):
			return "mcp_setup_failed", fmt.Errorf("antigravity: MCP configuration failed: %s", rpcErr.Message)
		case strings.Contains(msgLower, "config") || strings.Contains(msgLower, "option") || strings.Contains(msgLower, "catalog"):
			return "config_failed", fmt.Errorf("antigravity: configuration catalog failed: %s", rpcErr.Message)
		default:
			return "session_setup_failed", fmt.Errorf("antigravity: ACP request failed (code %d): %s", rpcErr.Code, rpcErr.Message)
		}
	}

	msgLower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msgLower, "auth") || strings.Contains(msgLower, "login") || strings.Contains(msgLower, "token") || strings.Contains(msgLower, "credential") || strings.Contains(msgLower, "unauthorized"):
		return "unauthenticated", fmt.Errorf("antigravity: authentication required: %w", err)
	case strings.Contains(msgLower, "protocol") || strings.Contains(msgLower, "unsupported"):
		return "protocol_mismatch", fmt.Errorf("antigravity: protocol mismatch: %w", err)
	case strings.Contains(msgLower, "mcp"):
		return "mcp_setup_failed", fmt.Errorf("antigravity: MCP setup failed: %w", err)
	case strings.Contains(msgLower, "config") || strings.Contains(msgLower, "catalog"):
		return "config_failed", fmt.Errorf("antigravity: configuration failed: %w", err)
	case errors.Is(err, context.DeadlineExceeded):
		return "probe_timeout", fmt.Errorf("antigravity: probe timed out: %w", err)
	case errors.Is(err, context.Canceled):
		return "probe_canceled", fmt.Errorf("antigravity: probe canceled: %w", err)
	default:
		return "session_setup_failed", fmt.Errorf("antigravity: session setup failed: %w", err)
	}
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
