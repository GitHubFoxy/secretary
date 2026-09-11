package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHarnessInventoryFixturesCaptureObservedDifferences(t *testing.T) {
	tests := []struct {
		name           string
		fixture        HarnessInstance
		wantModel      ObservedModelID
		wantReasoning  ObservedReasoningLevel
		wantExecution  ExecutionCapability
		wantActivity   ActivityCapability
		wantNoActivity ActivityCapability
	}{
		{
			name:           "macbook/claude",
			fixture:        macbookClaudeFixture(),
			wantModel:      "claude-sonnet-4-20250514",
			wantReasoning:  "extended",
			wantExecution:  CapabilityShell,
			wantActivity:   ActivityThinkingSummary,
			wantNoActivity: ActivitySubagentProgress,
		},
		{
			name:           "macbook/codex",
			fixture:        macbookCodexFixture(),
			wantModel:      "gpt-5-codex",
			wantReasoning:  "high",
			wantExecution:  CapabilitySteering,
			wantActivity:   ActivityToolCall,
			wantNoActivity: ActivityThinkingSummary,
		},
		{
			name:           "home-server/fx",
			fixture:        homeServerFXFixture(),
			wantModel:      "gpt-5.6-luna",
			wantReasoning:  "default",
			wantExecution:  CapabilityEdit,
			wantActivity:   ActivityStatus,
			wantNoActivity: ActivityToolCall,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := HarnessInventorySnapshot{
				Node:       test.fixture.Node,
				Instances:  []HarnessInstance{test.fixture},
				ObservedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
			}
			if err := inventory.Validate(); err != nil {
				t.Fatal(err)
			}
			if !contains(test.fixture.ModelIDs, test.wantModel) || !contains(test.fixture.ReasoningLevels, test.wantReasoning) {
				t.Fatalf("observed values = models=%v reasoning=%v", test.fixture.ModelIDs, test.fixture.ReasoningLevels)
			}
			if !test.fixture.Capabilities.SupportsExecution(test.wantExecution) {
				t.Fatalf("execution capabilities = %v", test.fixture.Capabilities.Execution)
			}
			if !test.fixture.Capabilities.SupportsActivity(test.wantActivity) || test.fixture.Capabilities.SupportsActivity(test.wantNoActivity) {
				t.Fatalf("activity capabilities = %v", test.fixture.Capabilities.Activity)
			}
		})
	}
}

func TestActivityRepresentationRequiresObservedCapability(t *testing.T) {
	fixture := macbookClaudeFixture()
	activity := Activity{
		Metadata: ActivityMetadata{
			EventID:           "evt-1",
			Node:              fixture.Node,
			HarnessInstanceID: fixture.ID,
			WorkerRef:         "worker-1",
			TurnID:            "turn-1",
			AttemptID:         "attempt-1",
			Sequence:          7,
			ObservedAt:        time.Date(2026, 9, 11, 12, 0, 1, 0, time.UTC),
		},
		Kind: ActivityThinkingSummary,
		Text: "Inspecting the header",
	}
	if err := activity.Validate(fixture.Capabilities); err != nil {
		t.Fatal(err)
	}

	unsupported := activity
	unsupported.Kind = ActivitySubagentProgress
	if err := unsupported.Validate(fixture.Capabilities); err == nil {
		t.Fatal("unsupported activity was accepted as synthetic")
	}
	unknown := activity
	unknown.Kind = ActivityKind("synthetic_progress")
	if err := unknown.Validate(fixture.Capabilities); err == nil {
		t.Fatal("unknown activity kind was accepted")
	}
}

func TestAttemptOutcomeAndCommandMetadataAreTransportSafe(t *testing.T) {
	outcome := AttemptOutcomeEnvelope{
		EventID:           "evt-outcome-1",
		Node:              "home-server",
		HarnessInstanceID: "home-server/fx",
		WorkerRef:         "worker-1",
		TurnID:            "turn-1",
		AttemptID:         "attempt-1",
		Status:            OutcomeSucceeded,
		Classification:    OutcomeFinal,
		Summary:           "done",
		CorrelationID:     "turn-1",
		OccurredAt:        time.Date(2026, 9, 11, 12, 0, 2, 0, time.UTC),
	}
	if err := outcome.Validate(); err != nil {
		t.Fatal(err)
	}
	command := CommandMetadata{
		CommandID:         "cmd-1",
		Node:              "macbook",
		HarnessInstanceID: "macbook/codex",
		WorkerRef:         "worker-1",
		TurnID:            "turn-1",
		AttemptID:         "attempt-1",
		CorrelationID:     "turn-1",
		IssuedAt:          time.Date(2026, 9, 11, 12, 0, 3, 0, time.UTC),
	}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(struct {
		Outcome AttemptOutcomeEnvelope `json:"outcome"`
		Command CommandMetadata        `json:"command"`
	}{outcome, command})
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(encoded)
	for _, forbidden := range []string{"runtime_session_id", "callback", "child_worker", "parent_task", "fast", "smart", "cheap"} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("transport contract contains forbidden %q: %s", forbidden, jsonText)
		}
	}
}

func TestHarnessPolicyIsSeparateFromObservedInventory(t *testing.T) {
	policy := HarnessPolicy{
		DefaultHarness:                HarnessFX,
		PreferredHarnesses:            []HarnessKind{HarnessFX, HarnessClaudeCode},
		RequiredExecutionCapabilities: []ExecutionCapability{CapabilityShell},
		ModelID:                       "owner-pinned-model",
		Reasoning:                     "high",
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "model_ids") || strings.Contains(string(encoded), "reasoning_levels") || strings.Contains(string(encoded), "status") {
		t.Fatalf("policy contains observed inventory fields: %s", encoded)
	}
}

func TestHarnessContractHasNoForbiddenFields(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(HarnessInstance{}),
		reflect.TypeOf(HarnessInventorySnapshot{}),
		reflect.TypeOf(Activity{}),
		reflect.TypeOf(ActivityMetadata{}),
		reflect.TypeOf(AttemptOutcomeEnvelope{}),
		reflect.TypeOf(CommandMetadata{}),
		reflect.TypeOf(HarnessPolicy{}),
	} {
		assertNoForbiddenFields(t, typ, map[reflect.Type]bool{})
	}
}

func assertNoForbiddenFields(t *testing.T, typ reflect.Type, seen map[reflect.Type]bool) {
	t.Helper()
	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.ToLower(field.Name + " " + field.Tag.Get("json"))
		for _, forbidden := range []string{"runtime_session", "callback", "child_worker", "parent_task", "fast", "smart", "cheap"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("%s contains forbidden field %s", typ, field.Name)
			}
		}
		assertNoForbiddenFields(t, field.Type, seen)
	}
}

func contains[T comparable](values []T, want T) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func macbookClaudeFixture() HarnessInstance {
	return HarnessInstance{
		ID:             "macbook/claude",
		Node:           "macbook",
		Kind:           HarnessClaudeCode,
		Version:        "1.2.3",
		Authentication: HarnessAuthentication{Authenticated: true, Method: "oauth"},
		Status:         HarnessReady,
		Capabilities: HarnessCapabilities{
			Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit, CapabilityCancel, CapabilitySteering, CapabilityApprovals},
			Activity:  []ActivityCapability{ActivitySessionStarted, ActivityThinkingSummary, ActivityAssistantTextDelta, ActivityToolCall, ActivityToolResult, ActivityPermissionRequest, ActivityStatus},
		},
		ModelIDs:        []ObservedModelID{"claude-sonnet-4-20250514"},
		ReasoningLevels: []ObservedReasoningLevel{"default", "extended"},
	}
}

func macbookCodexFixture() HarnessInstance {
	return HarnessInstance{
		ID:             "macbook/codex",
		Node:           "macbook",
		Kind:           HarnessCodex,
		Version:        "0.42.0",
		Authentication: HarnessAuthentication{Authenticated: true, Method: "api_key"},
		Status:         HarnessReady,
		Capabilities: HarnessCapabilities{
			Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit, CapabilityCancel, CapabilitySteering},
			Activity:  []ActivityCapability{ActivitySessionStarted, ActivityAssistantTextDelta, ActivityToolCall, ActivityToolResult, ActivityStatus, ActivityAttemptOutcome},
		},
		ModelIDs:        []ObservedModelID{"gpt-5-codex"},
		ReasoningLevels: []ObservedReasoningLevel{"medium", "high"},
	}
}

func homeServerFXFixture() HarnessInstance {
	return HarnessInstance{
		ID:             "home-server/fx",
		Node:           "home-server",
		Kind:           HarnessFX,
		Version:        "0.9.0",
		Authentication: HarnessAuthentication{Authenticated: false},
		Status:         HarnessReady,
		Capabilities: HarnessCapabilities{
			Execution: []ExecutionCapability{CapabilityShell, CapabilityEdit, CapabilityCancel, CapabilityApprovals},
			Activity:  []ActivityCapability{ActivitySessionStarted, ActivityAssistantTextDelta, ActivityToolResult, ActivityStatus},
		},
		ModelIDs:        []ObservedModelID{"gpt-5.6-luna"},
		ReasoningLevels: []ObservedReasoningLevel{"default"},
	}
}
