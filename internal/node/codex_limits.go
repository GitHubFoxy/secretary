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

func evaluateCodexRateLimits(resp CodexGetAccountRateLimitsResponse, model string, limitID string, now time.Time, previousDeadline *time.Time) RateLimitObservation {
	obs := RateLimitObservation{
		IsManualHold: true,
	}
	if resp.AccountID != nil {
		obs.AccountID = *resp.AccountID
	}

	if resp.OrdinaryUsageAllowed != nil && !*resp.OrdinaryUsageAllowed {
		obs.BlockerReason = "ordinary usage not allowed by account"
		return obs
	}

	var matched *CodexRateLimitSnapshot
	var matchedScope string

	limitID = strings.TrimSpace(limitID)
	model = strings.TrimSpace(model)

	if len(resp.RateLimitsByLimitID) > 0 {
		if limitID != "" {
			if snap, ok := resp.RateLimitsByLimitID[limitID]; ok {
				if snap.LimitID != nil && *snap.LimitID != "" && *snap.LimitID != limitID {
					obs.BlockerReason = "rate limit bucket identity conflicts with limitID"
					return obs
				}
				if model != "" && snap.NormalModelSlug != nil && *snap.NormalModelSlug != "" && *snap.NormalModelSlug != model {
					obs.BlockerReason = fmt.Sprintf("rate limit bucket model %q conflicts with worker model %q", *snap.NormalModelSlug, model)
					return obs
				}
				s := snap
				matched = &s
				matchedScope = limitID
			} else {
				var matching []struct {
					id   string
					snap CodexRateLimitSnapshot
				}
				for k, snap := range resp.RateLimitsByLimitID {
					if snap.LimitID != nil && *snap.LimitID == limitID {
						if model != "" && snap.NormalModelSlug != nil && *snap.NormalModelSlug != "" && *snap.NormalModelSlug != model {
							obs.BlockerReason = fmt.Sprintf("rate limit bucket model %q conflicts with worker model %q", *snap.NormalModelSlug, model)
							return obs
						}
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
				} else if len(matching) > 1 {
					obs.BlockerReason = "ambiguous rate limit buckets matching limitID"
					return obs
				}
			}
		}
		if matched == nil && model != "" {
			var matching []struct {
				id   string
				snap CodexRateLimitSnapshot
			}
			for k, snap := range resp.RateLimitsByLimitID {
				if snap.NormalModelSlug != nil && *snap.NormalModelSlug == model {
					if limitID != "" && snap.LimitID != nil && *snap.LimitID != "" && *snap.LimitID != limitID {
						continue
					}
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
		if matched == nil && len(resp.RateLimitsByLimitID) == 1 {
			for k, snap := range resp.RateLimitsByLimitID {
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

	var exhausted []*CodexRateLimitWindow
	if matched.Primary != nil && matched.Primary.UsedPercent >= 100.0 {
		exhausted = append(exhausted, matched.Primary)
	}
	if matched.Secondary != nil && matched.Secondary.UsedPercent >= 100.0 {
		exhausted = append(exhausted, matched.Secondary)
	}

	if len(exhausted) == 0 {
		obs.BlockerReason = "no exhausted rate limit windows found"
		return obs
	}

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

	if !resetsAtTime.After(now) {
		obs.BlockerReason = "rate limit reset time is not in the future"
		return obs
	}
	if previousDeadline != nil && !resetsAtTime.After(*previousDeadline) {
		obs.BlockerReason = "rate limit reset time is not later than previous deadline"
		return obs
	}

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

func readCodexRateLimits(ctx context.Context, command string, environment []string, dir string) (CodexGetAccountRateLimitsResponse, error) {
	if command == "" {
		command = "codex"
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	configArgs := codexConfigArguments(environment)
	arguments := append(configArgs, "app-server", "--listen", "stdio://")

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

	go func() {
		for range client.Events() {
		}
	}()

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

	if err := client.Notify("initialized", map[string]any{}); err != nil {
		return CodexGetAccountRateLimitsResponse{}, fmt.Errorf("codex rate limits initialized notification: %w", err)
	}

	var resp CodexGetAccountRateLimitsResponse
	if err := client.Request(queryCtx, "account/rateLimits/read", map[string]any{}, &resp); err != nil {
		return CodexGetAccountRateLimitsResponse{}, fmt.Errorf("codex rate limits read: %w", err)
	}

	return resp, nil
}
