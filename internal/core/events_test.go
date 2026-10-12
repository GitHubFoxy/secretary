package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventsAreOrderedAndPrunable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	store.now = func() time.Time { return time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC) }
	first, err := store.RecordEvent(ctx, "worker.started", "wrk-1", "att-1", "ses-1", map[string]string{"source": "test"})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) }
	if _, err := store.RecordEvent(ctx, "worker.finished", "wrk-1", "att-1", "ses-1", map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.EventsAfter(ctx, time.Time{}, 10)
	if err != nil || len(events) != 2 || events[0].ID != first.ID {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	recent, err := store.EventsRecent(ctx, 1)
	if err != nil || len(recent) != 1 || recent[0].Kind != "worker.finished" {
		t.Fatalf("recent=%#v err=%v", recent, err)
	}
	deleted, err := store.PruneEvents(ctx, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
}

func TestFailureEventPayloadRejectsMalformedTypes(t *testing.T) {
	for _, kind := range []string{"result.accepted", "attempt.outcome_recorded"} {
		for _, status := range []string{`"failed"`, `123`, `{"message":"Digest username=owner, response=privatevalue https://unknown.invalid/private"}`, `"unsupported-privatevalue"`} {
			raw := json.RawMessage(`{"status":` + status + `,"id":"result","attempt_id":"attempt","extra":{"message":"privatevalue"},"text":"Digest username=owner, response=privatevalue https://unknown.invalid/private","summary":{"message":"privatevalue"},"failure":{"category":123,"message":"privatevalue"},"failure_category":["privatevalue"],"error_code":["privatevalue"],"error_message":{"message":"privatevalue"},"diagnostics":["privatevalue"],"failure_code":{"message":"privatevalue"}}`)
			got := sanitizeFailureEventPayload(kind, raw)
			if strings.Contains(string(got), "privatevalue") {
				t.Fatalf("%s leaked malformed fields: %s", kind, got)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(got, &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["id"]) != `"result"` || string(payload["attempt_id"]) != `"attempt"` {
				t.Fatalf("identity changed: %s", got)
			}
		}
		raw := json.RawMessage(`{"status":"failed","failure":{"category":"usage_limit","provider_code":"usage_limit_exceeded","message":"Usage limit exceeded"}}`)
		var payload struct {
			Failure WorkerFailure `json:"failure"`
		}
		if err := json.Unmarshal(sanitizeFailureEventPayload(kind, raw), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Failure.Category != FailureCategoryUsageLimit || payload.Failure.ProviderCode != "usage_limit_exceeded" || payload.Failure.Message != "Usage limit exceeded" {
			t.Fatalf("typed failure changed: %#v", payload)
		}
		success := json.RawMessage(`{"status":"succeeded","summary":"privatevalue","failure":{"category":123}}`)
		if got := sanitizeFailureEventPayload(kind, success); string(got) != string(success) {
			t.Fatalf("successful payload changed: %s", got)
		}
	}
}

func TestFailureEventPayloadAllowlistAndBounds(t *testing.T) {
	for _, kind := range []string{"result.accepted", "attempt.outcome_recorded"} {
		raw := json.RawMessage(`{"status":"failed","classification":"final","id":"result","created_at":"2026-10-13T00:00:00Z","artifact_refs":{"message":"privatevalue"},"text":"privatevalue","extra":{"message":"privatevalue"},"failure_category":"unknown-privatevalue","failure":{"category":"unknown-privatevalue","provider_code":"unknown-privatevalue","message":"` + strings.Repeat("я", 1200) + `","technical":"Digest username=owner, response=privatevalue https://unknown.invalid/private","extra":"privatevalue"}}`)
		got := sanitizeFailureEventPayload(kind, raw)
		if strings.Contains(string(got), "privatevalue") || strings.Contains(string(got), "unknown.invalid") {
			t.Fatalf("%s leaked: %s", kind, got)
		}
		var payload struct {
			Failure        WorkerFailure `json:"failure"`
			Status         string        `json:"status"`
			Classification string        `json:"classification"`
		}
		if err := json.Unmarshal(got, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Status != "failed" || payload.Classification != "final" || payload.Failure.Category != FailureCategoryInternalError || len([]rune(payload.Failure.Message)) > 1000 {
			t.Fatalf("metadata/bound: %#v", payload)
		}
	}
	for _, raw := range []string{`[]`, `null`, `{`} {
		if got := sanitizeFailureEventPayload("result.accepted", json.RawMessage(raw)); string(got) != `{"status":"failed"}` {
			t.Fatalf("unsupported payload retained: %s", got)
		}
	}
}
