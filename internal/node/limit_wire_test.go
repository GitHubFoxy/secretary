package node

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
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

type continuationTestSession struct {
	id      string
	mu      sync.Mutex
	prompts []string
	results chan Result
}

func (s *continuationTestSession) ID() string { return s.id }
func (s *continuationTestSession) Prompt(_ context.Context, p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, p)
	return nil
}
func (s *continuationTestSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (s *continuationTestSession) Cancel(context.Context) error                { return nil }
func (s *continuationTestSession) Activity() <-chan Activity                   { return nil }
func (s *continuationTestSession) Result() <-chan Result                       { return s.results }
func (s *continuationTestSession) Close() error                                { return nil }

type continuationTestRuntime struct {
	requests chan StartRequest
	session  *continuationTestSession
}

func (r *continuationTestRuntime) Start(_ context.Context, req StartRequest) (Session, error) {
	r.requests <- req
	return r.session, nil
}

func (r *continuationTestRuntime) Resume(_ context.Context, req StartRequest, _ string) (Session, error) {
	r.requests <- req
	return r.session, nil
}

func TestRateLimitWire_ResumeContinuationPromptSemantics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	storePath := filepath.Join(t.TempDir(), "continuation_node.json")
	store, err := OpenLocalStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sess := &continuationTestSession{
		id:      "native-sess-1",
		results: make(chan Result, 2),
	}
	rt := &continuationTestRuntime{
		requests: make(chan StartRequest, 4),
		session:  sess,
	}

	execNode := NewExecutionNode("macbook", rt, store)

	initialCmd := dispatchFixture("limit-cont-init")
	initialCmd.Dispatch.Envelope.HarnessInstance.Kind = core.HarnessCodex
	initialCmd.Dispatch.Envelope.Profile = workerTemplateFixture(initialCmd.Dispatch.Envelope.HarnessInstance, "", "")
	initialCmd.Dispatch.Envelope.OriginalUserIntent = "Initial original user task"

	outcome, err := execNode.HandleCommand(ctx, initialCmd)
	if err != nil || outcome.State != CommandAccepted {
		t.Fatalf("initial dispatch failed: outcome=%#v err=%v", outcome, err)
	}

	select {
	case req := <-rt.requests:
		if req.Task != "Initial original user task" {
			t.Fatalf("expected initial task to be original intent, got %q", req.Task)
		}
		if req.DeferInitialPrompt {
			t.Fatal("expected DeferInitialPrompt=false for fresh dispatch")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	sess.results <- Result{
		Status:          "failed",
		Summary:         "usage limit reached",
		FailureCode:     "usage_limit_exceeded",
		FailureCategory: core.FailureCategoryUsageLimit,
	}

	attempt1ID := initialCmd.Dispatch.Metadata.AttemptID
	for {
		events, err := store.PendingEvents()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, ev := range events {
			if ev.EventID == "attempt-outcome-"+attempt1ID {
				found = true
				break
			}
		}
		if found {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for initial outcome event")
		case <-time.After(5 * time.Millisecond):
		}
	}

	resumeMeta := initialCmd.Metadata()
	resumeMeta.CommandID = "cont-resume-cmd"
	resumeMeta.AttemptID = "attempt-2"

	resumeEnv := initialCmd.Dispatch.Envelope
	resumeEnv.AttemptID = "attempt-2"
	resumeEnv.PreviousAttemptID = attempt1ID
	resumeEnv.ContinuationPrompt = "Continue after rate limit resets"
	resumeEnv.OriginalUserIntent = "Initial original user task that must NOT be resent"

	resumeCmd := Command{
		Kind: CommandResume,
		Resume: &ResumeCommand{
			Metadata: resumeMeta,
			Envelope: resumeEnv,
		},
	}

	resumeOutcome, err := execNode.HandleCommand(ctx, resumeCmd)
	if err != nil || resumeOutcome.State != CommandAccepted {
		t.Fatalf("continuation resume failed: outcome=%#v err=%v", resumeOutcome, err)
	}

	select {
	case req := <-rt.requests:
		if req.Task != "Continue after rate limit resets" {
			t.Fatalf("expected StartRequest.Task to be ContinuationPrompt %q, got %q", "Continue after rate limit resets", req.Task)
		}
		if !req.DeferInitialPrompt {
			t.Fatal("expected StartRequest.DeferInitialPrompt to be true for continuing resume")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	deadline := time.Now().Add(2 * time.Second)
	var gotPrompts []string
	for time.Now().Before(deadline) {
		sess.mu.Lock()
		if len(sess.prompts) > 0 {
			gotPrompts = append([]string(nil), sess.prompts...)
			sess.mu.Unlock()
			break
		}
		sess.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	if len(gotPrompts) != 1 {
		t.Fatalf("expected exactly 1 prompt to session, got %v", gotPrompts)
	}
	if gotPrompts[0] != "Continue after rate limit resets" {
		t.Fatalf("expected prompt %q, got %q", "Continue after rate limit resets", gotPrompts[0])
	}
}
