package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestActivityMetadataValidateRequiresTransportIdentity(t *testing.T) {
	valid := ActivityMetadata{
		EventID:           "evt-1",
		Node:              "macbook",
		HarnessInstanceID: "macbook/claude",
		AttemptID:         "attempt-1",
		Sequence:          1,
		ObservedAt:        time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		name   string
		mutate func(*ActivityMetadata)
	}{
		{name: "event id", mutate: func(metadata *ActivityMetadata) { metadata.EventID = "" }},
		{name: "Node", mutate: func(metadata *ActivityMetadata) { metadata.Node = "" }},
		{name: "HarnessInstance", mutate: func(metadata *ActivityMetadata) { metadata.HarnessInstanceID = "" }},
		{name: "Attempt", mutate: func(metadata *ActivityMetadata) { metadata.AttemptID = "" }},
		{name: "sequence", mutate: func(metadata *ActivityMetadata) { metadata.Sequence = 0 }},
		{name: "observation time", mutate: func(metadata *ActivityMetadata) { metadata.ObservedAt = time.Time{} }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			metadata := valid
			test.mutate(&metadata)
			if err := metadata.Validate(); err == nil {
				t.Fatal("missing activity metadata was accepted")
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestActivityValidateChecksMetadataAndHarnessBinding(t *testing.T) {
	instance := macbookClaudeFixture()
	activity := Activity{
		Metadata: ActivityMetadata{
			EventID:           "evt-1",
			Node:              instance.Node,
			HarnessInstanceID: instance.ID,
			AttemptID:         "attempt-1",
			Sequence:          1,
			ObservedAt:        time.Date(2026, 9, 11, 12, 0, 1, 0, time.UTC),
		},
		Kind: ActivityThinkingSummary,
		Text: "Inspecting the header",
	}
	if err := activity.Validate(instance.Capabilities); err != nil {
		t.Fatal(err)
	}
	if err := activity.ValidateFor(instance); err != nil {
		t.Fatal(err)
	}

	missing := activity
	missing.Metadata.AttemptID = ""
	if err := missing.Validate(instance.Capabilities); err == nil {
		t.Fatal("Activity.Validate accepted missing metadata")
	}

	wrongNode := activity
	wrongNode.Metadata.Node = "home-server"
	if err := wrongNode.ValidateFor(instance); err == nil {
		t.Fatal("Activity.ValidateFor accepted a different Node")
	}
	wrongHarness := activity
	wrongHarness.Metadata.HarnessInstanceID = "macbook/codex"
	if err := wrongHarness.ValidateFor(instance); err == nil {
		t.Fatal("Activity.ValidateFor accepted a different HarnessInstance")
	}
}

func TestAttemptOutcomeEnvelopeCarriesArtifactReferences(t *testing.T) {
	outcome := AttemptOutcomeEnvelope{
		EventID:           "evt-outcome-1",
		Node:              "home-server",
		HarnessInstanceID: "home-server/fx",
		WorkerRef:         "worker-1",
		TurnID:            "turn-1",
		AttemptID:         "attempt-1",
		Status:            OutcomeSucceeded,
		Classification:    OutcomeFinal,
		Summary:           "Implemented and tested the fix",
		ArtifactRefs: []ArtifactRef{
			{Kind: "commit", Ref: "abc123def"},
			{Kind: "path", Ref: "internal/core/harness_contract.go"},
		},
		OccurredAt: time.Date(2026, 9, 11, 12, 0, 2, 0, time.UTC),
	}
	if err := outcome.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(encoded)
	for _, want := range []string{"artifact_refs", "abc123def", "internal/core/harness_contract.go", "Implemented and tested the fix"} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("encoded outcome %q is missing %q", jsonText, want)
		}
	}
}
