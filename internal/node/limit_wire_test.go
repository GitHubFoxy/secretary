package node

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestRateLimitWire_EnvelopeEncodingAndCompatibility(t *testing.T) {
	now := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	resetTime := now.Add(30 * time.Minute)

	t.Run("encodes and decodes with resets_at and quota_scope", func(t *testing.T) {
		env := core.AttemptOutcomeEnvelope{
			EventID:           "outcome-1",
			Node:              "node-1",
			HarnessInstanceID: "inst-1",
			WorkerRef:         "worker-1",
			TurnID:            "turn-1",
			AttemptID:         "att-1",
			Status:            core.OutcomeFailed,
			Classification:    core.OutcomeRetryable,
			Summary:           "Usage limit reached",
			FailureCode:       "usage_limit_exceeded",
			FailureCategory:   core.FailureCategoryUsageLimit,
			ResetsAt:          &resetTime,
			QuotaScope:        "premium",
			OccurredAt:        now,
		}

		if err := env.Validate(); err != nil {
			t.Fatalf("validation failed: %v", err)
		}

		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var decoded core.AttemptOutcomeEnvelope
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if decoded.ResetsAt == nil || !decoded.ResetsAt.Equal(resetTime) {
			t.Fatalf("expected resets_at %v, got %v", resetTime, decoded.ResetsAt)
		}
		if decoded.QuotaScope != "premium" {
			t.Fatalf("expected quota_scope premium, got %s", decoded.QuotaScope)
		}
	})

	t.Run("compatible with old envelopes omitting resets_at and quota_scope", func(t *testing.T) {
		oldJSON := `{"event_id":"outcome-old","node":"node-1","harness_instance_id":"inst-1","worker_ref":"w","turn_id":"t","attempt_id":"a","status":"failed","classification":"final","summary":"Old failure","occurred_at":"2026-10-13T03:00:00Z"}`

		var decoded core.AttemptOutcomeEnvelope
		if err := json.Unmarshal([]byte(oldJSON), &decoded); err != nil {
			t.Fatalf("unmarshal old JSON failed: %v", err)
		}

		if err := decoded.Validate(); err != nil {
			t.Fatalf("validation of old envelope failed: %v", err)
		}
		if decoded.ResetsAt != nil {
			t.Fatalf("expected nil ResetsAt on old envelope, got %v", decoded.ResetsAt)
		}
		if decoded.QuotaScope != "" {
			t.Fatalf("expected empty QuotaScope on old envelope, got %q", decoded.QuotaScope)
		}
	})
}

func TestRateLimitWire_LocalStoreQueueAndReplay(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenLocalStore(filepath.Join(dir, "node.json"))
	if err != nil {
		t.Fatalf("OpenLocalStore: %v", err)
	}

	now := time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC)
	resetTime := now.Add(45 * time.Minute)

	envelope := core.AttemptOutcomeEnvelope{
		EventID:           "outcome-queue-test",
		Node:              "node-1",
		HarnessInstanceID: "inst-1",
		WorkerRef:         "worker-1",
		TurnID:            "turn-1",
		AttemptID:         "att-1",
		Status:            core.OutcomeFailed,
		Classification:    core.OutcomeRetryable,
		Summary:           "Usage limit reached",
		FailureCode:       "usage_limit_exceeded",
		FailureCategory:   core.FailureCategoryUsageLimit,
		ResetsAt:          &resetTime,
		QuotaScope:        "codex-primary",
		OccurredAt:        now,
	}

	pending, err := store.QueueOutcome(envelope)
	if err != nil {
		t.Fatalf("QueueOutcome: %v", err)
	}

	var storedEvent NodeEvent
	if err := json.Unmarshal(pending.Payload, &storedEvent); err != nil {
		t.Fatalf("unmarshal pending payload: %v", err)
	}

	if storedEvent.Outcome == nil {
		t.Fatalf("expected non-nil outcome in pending event")
	}
	stored := *storedEvent.Outcome

	if stored.ResetsAt == nil || !stored.ResetsAt.Equal(resetTime) {
		t.Fatalf("expected stored ResetsAt %v, got %v", resetTime, stored.ResetsAt)
	}
	if stored.QuotaScope != "codex-primary" {
		t.Fatalf("expected stored QuotaScope codex-primary, got %s", stored.QuotaScope)
	}
}
