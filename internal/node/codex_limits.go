package node

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
)

type CodexRateLimitWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int    `json:"windowDurationMins,omitempty"`
	ResetsAt           *int64  `json:"resetsAt,omitempty"`
}

type CodexRateLimitSnapshot struct {
	LimitID              *string               `json:"limitId,omitempty"`
	LimitName            *string               `json:"limitName,omitempty"`
	NormalModelSlug      *string               `json:"normalModelSlug,omitempty"`
	Primary              *CodexRateLimitWindow `json:"primary,omitempty"`
	Secondary            *CodexRateLimitWindow `json:"secondary,omitempty"`
	SpendControlReached  *bool                 `json:"spendControlReached,omitempty"`
	RateLimitReachedType *string               `json:"rateLimitReachedType,omitempty"`
}

type CodexGetAccountRateLimitsResponse struct {
	OrdinaryUsageAllowed *bool                             `json:"ordinaryUsageAllowed,omitempty"`
	RateLimits           *CodexRateLimitSnapshot           `json:"rateLimits,omitempty"`
	RateLimitsByLimitID  map[string]CodexRateLimitSnapshot `json:"rateLimitsByLimitId,omitempty"`
	AccountID            *string                           `json:"accountId,omitempty"`
}

type RateLimitObservation struct {
	Scope         string
	ResetsAt      *time.Time
	ScheduledAt   *time.Time
	IsManualHold  bool
	BlockerReason string
	AccountID     string
}

// evaluateCodexRateLimits applies the strict scoped reset selection and blocker rules.
func evaluateCodexRateLimits(resp CodexGetAccountRateLimitsResponse, model string, limitID string, now time.Time, previousDeadline *time.Time) RateLimitObservation {
	obs := RateLimitObservation{
		IsManualHold: true,
	}
	if resp.AccountID != nil {
		obs.AccountID = *resp.AccountID
	}

	// 1. Explicit permission blockers.
	if resp.OrdinaryUsageAllowed != nil && !*resp.OrdinaryUsageAllowed {
		obs.BlockerReason = "ordinary usage not allowed by account"
		return obs
	}

	// 2. Multi-bucket or single-bucket resolution.
	var matched *CodexRateLimitSnapshot
	var matchedScope string

	limitID = strings.TrimSpace(limitID)
	model = strings.TrimSpace(model)

	if len(resp.RateLimitsByLimitID) > 0 {
		// Priority 1: Match by explicit confirmed limitID from error
		if limitID != "" {
			if snap, ok := resp.RateLimitsByLimitID[limitID]; ok {
				s := snap
				matched = &s
				matchedScope = limitID
			}
		}
		// Priority 2: Match by model slug
		if matched == nil && model != "" {
			var matching []struct {
				id   string
				snap CodexRateLimitSnapshot
			}
			for k, snap := range resp.RateLimitsByLimitID {
				if snap.NormalModelSlug != nil && *snap.NormalModelSlug == model {
					matching = append(matching, struct {
						id   string
						snap CodexRateLimitSnapshot
					}{k, snap})
				}
			}
			if len(matching) == 1 {
				s := matching[0].snap
				matched = &s
				matchedScope = matching[0].id
			}
		}
		// Priority 3: Exactly one bucket in map - only if unambiguous
		if matched == nil && len(resp.RateLimitsByLimitID) == 1 {
			for k, snap := range resp.RateLimitsByLimitID {
				// Only match if scope is unambiguous (limitID matches or is empty, and model matches or is empty)
				limitMatches := limitID == "" || (snap.LimitID != nil && *snap.LimitID == limitID) || k == limitID
				modelMatches := model == "" || (snap.NormalModelSlug != nil && *snap.NormalModelSlug == model)
				if limitMatches && modelMatches {
					s := snap
					matched = &s
					matchedScope = k
				}
			}
		}
	} else if resp.RateLimits != nil {
		// Backward-compatible single-bucket view; used ONLY if multi-bucket view is absent and single bucket is unambiguous
		snap := resp.RateLimits
		limitMatches := limitID == "" || (snap.LimitID != nil && *snap.LimitID == limitID)
		modelMatches := model == "" || (snap.NormalModelSlug != nil && *snap.NormalModelSlug == model)
		if limitMatches && modelMatches {
			matched = snap
			if snap.LimitID != nil {
				matchedScope = *snap.LimitID
			} else if snap.NormalModelSlug != nil {
				matchedScope = *snap.NormalModelSlug
			} else {
				matchedScope = "primary"
			}
		}
	}

	if matched == nil {
		obs.BlockerReason = "rate limit bucket scope is ambiguous or missing"
		return obs
	}
	obs.Scope = matchedScope

	// 3. Check spend control blockers and reached types
	if matched.SpendControlReached != nil && *matched.SpendControlReached {
		obs.BlockerReason = "spend control limit reached"
		return obs
	}
	if matched.RateLimitReachedType != nil {
		reachedType := *matched.RateLimitReachedType
		if reachedType == "workspace_owner_credits_depleted" || reachedType == "workspace_member_credits_depleted" {
			obs.BlockerReason = "workspace credits depleted"
			return obs
		}
	}

	// 4. Collect all exhausted windows (usedPercent >= 100.0)
	var exhausted []*CodexRateLimitWindow
	if matched.Primary != nil && matched.Primary.UsedPercent >= 100.0 {
		exhausted = append(exhausted, matched.Primary)
	}
	if matched.Secondary != nil && matched.Secondary.UsedPercent >= 100.0 {
		exhausted = append(exhausted, matched.Secondary)
	}

	if len(exhausted) == 0 {
		// Category is NOT changed to billing; reset time is unknown
		obs.BlockerReason = "no exhausted rate limit windows found"
		return obs
	}

	// 5. If at least one exhausted window lacks a valid resetsAt, time is unknown
	var maxResetsAt int64
	for _, w := range exhausted {
		if w.ResetsAt == nil || *w.ResetsAt <= 0 {
			obs.BlockerReason = "exhausted window missing valid resetsAt"
			return obs
		}
		if *w.ResetsAt > maxResetsAt {
			maxResetsAt = *w.ResetsAt
		}
	}

	resetsAtTime := time.Unix(maxResetsAt, 0).UTC()
	obs.ResetsAt = &resetsAtTime

	// 6. Check that reset time is in the future and later than previous deadline
	if !resetsAtTime.After(now) {
		obs.BlockerReason = "rate limit reset time is not in the future"
		return obs
	}
	if previousDeadline != nil && !resetsAtTime.After(*previousDeadline) {
		obs.BlockerReason = "rate limit reset time is not later than previous deadline"
		return obs
	}

	// 7. Add exactly 60 seconds (1 minute) for scheduled retry
	scheduledAtTime := resetsAtTime.Add(60 * time.Second)
	obs.ScheduledAt = &scheduledAtTime
	obs.IsManualHold = false
	obs.BlockerReason = ""
	return obs
}

func codexConfigArguments(environment []string) []string {
	var args []string
	overrides := map[string]any{}
	for _, value := range environment {
		if strings.HasPrefix(value, "CODEX_CONFIG=") {
			_ = json.Unmarshal([]byte(strings.TrimPrefix(value, "CODEX_CONFIG=")), &overrides)
		}
	}
	// Sort keys for deterministic argument ordering
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		if k == "developer_instructions" || k == "prompt" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := overrides[k]
		if encoded, err := json.Marshal(val); err == nil {
			args = append(args, "-c", fmt.Sprintf("%s=%s", k, string(encoded)))
		}
	}
	return args
}

// readCodexRateLimits launches a short-lived Codex app-server client on stdio
// using the selected binary and launch environment, queries account/rateLimits/read,
// and ensures full cleanup within 5 seconds.
func readCodexRateLimits(ctx context.Context, command string, environment []string, dir string) (CodexGetAccountRateLimitsResponse, error) {
	if command == "" {
		command = "codex"
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	configArgs := codexConfigArguments(environment)
	arguments := append(configArgs, "app-server", "--listen", "stdio://")

	// Filter out MCP-related environment variables so query client stays lightweight
	var cleanEnv []string
	for _, env := range environment {
		if strings.HasPrefix(env, "SECRETARY_MCP_") || strings.HasPrefix(env, "CODEX_CONFIG=") {
			continue
		}
		cleanEnv = append(cleanEnv, env)
	}

	client, err := acp.StartWithLogExactEnvDir(queryCtx, nil, cleanEnv, dir, command, arguments...)
	if err != nil {
		return CodexGetAccountRateLimitsResponse{}, err
	}
	defer func() {
		_ = client.Close()
	}()

	// Drain incoming notifications concurrently so reader loop never stalls
	go func() {
		for range client.Events() {
		}
	}()

	// Handshake: initialize
	var initResp json.RawMessage
	initParams := map[string]any{
		"clientInfo": map[string]string{
			"name":    "secretary",
			"title":   "Secretary",
			"version": "1.0.0",
		},
		"capabilities": nil,
	}
	if err := client.Request(queryCtx, "initialize", initParams, &initResp); err != nil {
		return CodexGetAccountRateLimitsResponse{}, fmt.Errorf("codex rate limits initialize: %w", err)
	}

	// Handshake: initialized notification
	if err := client.Notify("initialized", map[string]any{}); err != nil {
		return CodexGetAccountRateLimitsResponse{}, fmt.Errorf("codex rate limits initialized notification: %w", err)
	}

	// Read account rate limits
	var resp CodexGetAccountRateLimitsResponse
	if err := client.Request(queryCtx, "account/rateLimits/read", map[string]any{}, &resp); err != nil {
		return CodexGetAccountRateLimitsResponse{}, fmt.Errorf("codex rate limits read: %w", err)
	}

	return resp, nil
}
