package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexRateLimits_EvaluationWindowsAndPlusOneMinute(t *testing.T) {
	now := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	futureReset := now.Add(20 * time.Minute).Unix()
	weeklyReset := now.Add(48 * time.Hour).Unix()

	limitID := "premium"
	modelSlug := "gpt-5-codex"

	resp := CodexGetAccountRateLimitsResponse{
		RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
			"premium": {
				LimitID:         &limitID,
				NormalModelSlug: &modelSlug,
				Primary: &CodexRateLimitWindow{
					UsedPercent: 100.0,
					ResetsAt:    &futureReset,
				},
				Secondary: &CodexRateLimitWindow{
					UsedPercent: 100.0,
					ResetsAt:    &weeklyReset,
				},
			},
		},
	}

	obs := evaluateCodexRateLimits(resp, modelSlug, limitID, now, nil)
	if obs.IsManualHold {
		t.Fatalf("expected auto-schedule, got manual hold: %s", obs.BlockerReason)
	}
	if obs.ResetsAt == nil || obs.ScheduledAt == nil {
		t.Fatalf("expected non-nil ResetsAt and ScheduledAt")
	}

	expectedReset := time.Unix(weeklyReset, 0).UTC()
	if !obs.ResetsAt.Equal(expectedReset) {
		t.Fatalf("expected ResetsAt %v (weekly max), got %v", expectedReset, *obs.ResetsAt)
	}

	expectedScheduled := expectedReset.Add(60 * time.Second)
	if !obs.ScheduledAt.Equal(expectedScheduled) {
		t.Fatalf("expected ScheduledAt exactly ResetsAt + 60s (%v), got %v", expectedScheduled, *obs.ScheduledAt)
	}
	if obs.ScheduledAt.Sub(*obs.ResetsAt) != 60*time.Second {
		t.Fatalf("expected delta exactly 60 seconds, got %v", obs.ScheduledAt.Sub(*obs.ResetsAt))
	}
}

func TestCodexRateLimits_MultiBucketAndLegacySingleFallback(t *testing.T) {
	now := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	futureReset := now.Add(10 * time.Minute).Unix()

	t.Run("matches by limit id", func(t *testing.T) {
		limitID := "codex-fast"
		otherID := "codex-slow"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
				"codex-fast": {
					LimitID: &limitID,
					Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
				},
				"codex-slow": {
					LimitID: &otherID,
					Primary: &CodexRateLimitWindow{UsedPercent: 50.0},
				},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "codex-fast", now, nil)
		if obs.IsManualHold || obs.Scope != "codex-fast" {
			t.Fatalf("expected match on codex-fast, got %#v", obs)
		}
	})

	t.Run("matches by normalModelSlug when limitID omitted", func(t *testing.T) {
		slug := "o3-mini"
		limitID := "bucket-o3"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
				"bucket-o3": {
					LimitID:         &limitID,
					NormalModelSlug: &slug,
					Primary:         &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
				},
			},
		}
		obs := evaluateCodexRateLimits(resp, "o3-mini", "", now, nil)
		if obs.IsManualHold || obs.Scope != "bucket-o3" {
			t.Fatalf("expected match on o3-mini slug, got %#v", obs)
		}
	})

	t.Run("ambiguous multi-bucket without match falls back to manual hold", func(t *testing.T) {
		slug1, slug2 := "model-1", "model-2"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
				"b1": {NormalModelSlug: &slug1, Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset}},
				"b2": {NormalModelSlug: &slug2, Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset}},
			},
		}
		obs := evaluateCodexRateLimits(resp, "unknown-model", "", now, nil)
		if !obs.IsManualHold {
			t.Fatalf("expected manual hold for ambiguous buckets, got %#v", obs)
		}
	})

	t.Run("legacy single-bucket fallback when multi-bucket absent", func(t *testing.T) {
		limitID := "single-legacy"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				LimitID: &limitID,
				Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "single-legacy", now, nil)
		if obs.IsManualHold || obs.Scope != "single-legacy" {
			t.Fatalf("expected legacy match, got %#v", obs)
		}
	})

	t.Run("conflict when snapshot limitID does not match requested limitID", func(t *testing.T) {
		wrongID := "mismatched-id"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
				"codex-fast": {
					LimitID: &wrongID,
					Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
				},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "codex-fast", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "rate limit bucket identity conflicts with limitID" {
			t.Fatalf("expected conflict on limitID identity, got %#v", obs)
		}
	})

	t.Run("conflict when snapshot normalModelSlug does not match worker model", func(t *testing.T) {
		slug := "o3-mini"
		limitID := "codex-fast"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
				"codex-fast": {
					LimitID:         &limitID,
					NormalModelSlug: &slug,
					Primary:         &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
				},
			},
		}
		obs := evaluateCodexRateLimits(resp, "gpt-4o", "codex-fast", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != `rate limit bucket model "o3-mini" conflicts with worker model "gpt-4o"` {
			t.Fatalf("expected conflict on model slug, got %#v", obs)
		}
	})

	t.Run("matches by snapshot LimitID when map key differs", func(t *testing.T) {
		limitID := "confirmed-limit"
		slug := "o3-mini"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimitsByLimitID: map[string]CodexRateLimitSnapshot{
				"bucket-uuid-1": {
					LimitID:         &limitID,
					NormalModelSlug: &slug,
					Primary:         &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
				},
			},
		}
		obs := evaluateCodexRateLimits(resp, "o3-mini", "confirmed-limit", now, nil)
		if obs.IsManualHold || obs.Scope != "bucket-uuid-1" {
			t.Fatalf("expected match by LimitID field on bucket-uuid-1, got %#v", obs)
		}
	})
}

func TestCodexRateLimits_WindowsSelectionAndBlockers(t *testing.T) {
	now := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	futureReset := now.Add(15 * time.Minute).Unix()
	pastReset := now.Add(-5 * time.Minute).Unix()

	t.Run("ordinaryUsageAllowed false blocks auto retry", func(t *testing.T) {
		allowed := false
		resp := CodexGetAccountRateLimitsResponse{
			OrdinaryUsageAllowed: &allowed,
			RateLimits: &CodexRateLimitSnapshot{
				Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "ordinary usage not allowed by account" {
			t.Fatalf("expected ordinary usage blocker, got %#v", obs)
		}
	})

	t.Run("spendControlReached blocks auto retry", func(t *testing.T) {
		reached := true
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				SpendControlReached: &reached,
				Primary:             &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "spend control limit reached" {
			t.Fatalf("expected spend control blocker, got %#v", obs)
		}
	})

	t.Run("workspace_credits_depleted blocks auto retry", func(t *testing.T) {
		reachedType := "workspace_owner_credits_depleted"
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				RateLimitReachedType: &reachedType,
				Primary:              &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &futureReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "workspace credits depleted" {
			t.Fatalf("expected workspace credits blocker, got %#v", obs)
		}
	})

	t.Run("no exhausted windows gives manual hold without altering category to billing", func(t *testing.T) {
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				Primary:   &CodexRateLimitWindow{UsedPercent: 80.0, ResetsAt: &futureReset},
				Secondary: &CodexRateLimitWindow{UsedPercent: 95.0, ResetsAt: &futureReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "no exhausted rate limit windows found" {
			t.Fatalf("expected manual hold when no exhausted windows, got %#v", obs)
		}
	})

	t.Run("exhausted window missing resetsAt gives manual hold", func(t *testing.T) {
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: nil},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "exhausted window missing valid resetsAt" {
			t.Fatalf("expected manual hold for nil resetsAt, got %#v", obs)
		}
	})

	t.Run("past resetsAt gives manual hold", func(t *testing.T) {
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &pastReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, nil)
		if !obs.IsManualHold || obs.BlockerReason != "rate limit reset time is not in the future" {
			t.Fatalf("expected manual hold for past resetsAt, got %#v", obs)
		}
	})

	t.Run("resetsAt not later than previous deadline gives manual hold", func(t *testing.T) {
		prev := now.Add(20 * time.Minute)
		earlierFutureReset := now.Add(15 * time.Minute).Unix()
		resp := CodexGetAccountRateLimitsResponse{
			RateLimits: &CodexRateLimitSnapshot{
				Primary: &CodexRateLimitWindow{UsedPercent: 100.0, ResetsAt: &earlierFutureReset},
			},
		}
		obs := evaluateCodexRateLimits(resp, "", "", now, &prev)
		if !obs.IsManualHold || obs.BlockerReason != "rate limit reset time is not later than previous deadline" {
			t.Fatalf("expected manual hold when resetsAt <= previousDeadline, got %#v", obs)
		}
	})
}

func TestCodexRateLimits_SubprocessHandshakeAndReadFixture(t *testing.T) {
	dir := t.TempDir()
	mockBin := filepath.Join(dir, "mock-codex")

	script := `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"method":"initialize"'*)
      echo '{"jsonrpc":"2.0","id":1,"result":{"userAgent":"mock","codexHome":"/tmp","platformFamily":"unix","platformOs":"darwin"}}'
      ;;
    *'"method":"initialized"'*)
      # notification, no response
      ;;
    *'"method":"account/rateLimits/read"'*)
      echo '{"jsonrpc":"2.0","id":2,"result":{"ordinaryUsageAllowed":true,"accountId":"acc-123","rateLimits":{"limitId":"premium","primary":{"usedPercent":100.0,"resetsAt":1893456000}}}}'
      ;;
    *)
      ;;
  esac
done
`
	if err := os.WriteFile(mockBin, []byte(script), 0755); err != nil {
		t.Fatalf("write mock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := readCodexRateLimits(ctx, mockBin, []string{"CODEX_CONFIG={\"model\":\"o3\"}"}, dir)
	if err != nil {
		t.Fatalf("readCodexRateLimits failed: %v", err)
	}

	if resp.AccountID == nil || *resp.AccountID != "acc-123" {
		t.Fatalf("expected accountId acc-123, got %#v", resp.AccountID)
	}
	if resp.RateLimits == nil || resp.RateLimits.Primary == nil || resp.RateLimits.Primary.UsedPercent != 100.0 {
		t.Fatalf("expected 100.0 usedPercent, got %#v", resp.RateLimits)
	}
}

func TestCodexRateLimits_ProcessCleanupOnTimeout(t *testing.T) {
	dir := t.TempDir()
	hangBin := filepath.Join(dir, "hang-codex")

	script := `#!/bin/sh
trap 'exit 0' TERM INT HUP
while true; do
  sleep 1
done
`
	if err := os.WriteFile(hangBin, []byte(script), 0755); err != nil {
		t.Fatalf("write mock: %v", err)
	}

	start := time.Now()
	shortCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := readCodexRateLimits(shortCtx, hangBin, nil, dir)
	duration := time.Since(start)

	if err == nil {
		t.Fatalf("expected error from hanging process, got nil")
	}
	if duration > 3*time.Second {
		t.Fatalf("expected cleanup within bounded time, took %v", duration)
	}
}
