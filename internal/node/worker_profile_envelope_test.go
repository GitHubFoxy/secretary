package node

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

const syntheticEnvelopeProfile = "Synthetic private profile text for envelope validation."

type profileGateRuntime struct{ starts int }

func (r *profileGateRuntime) Start(context.Context, StartRequest) (Session, error) {
	r.starts++
	return profileGateSession{}, nil
}

type profileGateSession struct{}

func (profileGateSession) ID() string                                  { return "synthetic-native-id" }
func (profileGateSession) Prompt(context.Context, string) error        { return nil }
func (profileGateSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (profileGateSession) Cancel(context.Context) error                { return nil }
func (profileGateSession) Activity() <-chan Activity                   { return nil }
func (profileGateSession) Result() <-chan Result                       { return nil }
func (profileGateSession) Close() error                                { return nil }

func TestPhase4NodeCommandFailsClosedForInvalidWorkerTemplate(t *testing.T) {
	ctx := context.Background()
	localStore, err := OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = localStore.Close() })
	runtime := &profileGateRuntime{}
	execution := NewExecutionNode("profile-node", runtime, localStore)

	opencode := core.HarnessInstance{
		ID: "profile-node/opencode", Node: "profile-node", Kind: core.HarnessOpenCode, Version: "2.0.22",
		Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}},
		ModelIDs:     []core.ObservedModelID{"fixture/model"}, ReasoningLevels: []core.ObservedReasoningLevel{"xhigh"},
	}
	baseProfile := ManagedProfile{
		Version: "synthetic-v1", Name: "worker", Content: syntheticEnvelopeProfile, SourceHash: "synthetic-source-hash",
		Runtime: "opencode", Model: "fixture/model", Reasoning: "xhigh", Delivery: "native", AllowTools: []string{"read"},
	}
	baseProfile.Hash = baseProfile.SnapshotHash()
	root := t.TempDir()
	policy := core.ProjectPolicy{
		AllowedHarnessKinds: []core.HarnessKind{core.HarnessOpenCode}, ModelID: "fixture/model", Reasoning: "xhigh",
	}
	projectSnapshot := core.ProjectSnapshot{
		ID: "profile-project", Name: "Profile project", Mappings: []core.ProjectPathMapping{{Node: "profile-node", Path: root}},
		Policy: policy, Revision: 1, Node: "profile-node", Workspace: root, HarnessInstance: opencode,
	}
	baseEnvelope := WorkerEnvelope{
		WorkerRef: "synthetic-worker", TurnID: "synthetic-turn", AttemptID: "synthetic-attempt",
		OriginalUserIntent: "inspect synthetic fixture", ProjectID: projectSnapshot.ID, ProjectSnapshot: projectSnapshot,
		Workspace: root, HarnessInstance: opencode, Model: "fixture/model", Reasoning: "xhigh", Profile: baseProfile,
	}
	cases := []struct {
		name     string
		envelope WorkerEnvelope
		code     string
	}{
		{name: "missing", envelope: func() WorkerEnvelope { value := baseEnvelope; value.Profile = ManagedProfile{}; return value }(), code: "profile_missing"},
		{name: "invalid hash", envelope: func() WorkerEnvelope { value := baseEnvelope; value.Profile.Hash = "stale"; return value }(), code: "profile_invalid"},
		{name: "model binding mismatch", envelope: func() WorkerEnvelope {
			value := baseEnvelope
			value.Profile.Model = "other/model"
			value.Profile.Hash = value.Profile.SnapshotHash()
			return value
		}(), code: "profile_invalid"},
		{name: "project denies profile permission", envelope: func() WorkerEnvelope {
			value := baseEnvelope
			value.Profile.AllowTools = []string{"bash"}
			value.Profile.Hash = value.Profile.SnapshotHash()
			value.ProjectSnapshot.Policy.Execution.DeniedCapabilities = []core.ExecutionCapability{core.CapabilityShell}
			return value
		}(), code: "profile_invalid"},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			command := profileDispatchCommand(test.envelope, "profile-invalid-"+string(rune('a'+index)))
			outcome, err := execution.HandleCommand(ctx, command)
			if err != nil || outcome.State != CommandFailed || outcome.ErrorCode != test.code || outcome.ErrorMessage == "" {
				t.Fatalf("safe profile outcome code=%q state=%q err_present=%t", outcome.ErrorCode, outcome.State, err != nil)
			}
			if strings.Contains(outcome.ErrorMessage, syntheticEnvelopeProfile) {
				t.Fatal("profile instructions escaped through safe Node error")
			}
		})
	}
	missingProfile := baseEnvelope
	missingProfile.Profile = ManagedProfile{}
	dispatchMetadata := profileDispatchCommand(missingProfile, "profile-missing-resume").Dispatch.Metadata
	resumeCommand := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: dispatchMetadata, Envelope: missingProfile}}
	outcome, err := execution.HandleCommand(ctx, resumeCommand)
	if err != nil || outcome.State != CommandFailed || outcome.ErrorCode != "profile_missing" {
		t.Fatalf("missing-Profile Resume safe outcome code=%q state=%q err_present=%t", outcome.ErrorCode, outcome.State, err != nil)
	}
	invalidLegacyFlag := baseEnvelope
	invalidLegacyFlag.Profile = ManagedProfile{}
	invalidLegacyFlag.LegacyWorkerTemplate = true
	outcome, err = execution.HandleCommand(ctx, profileDispatchCommand(invalidLegacyFlag, "profile-invalid-legacy-flag"))
	if err != nil || outcome.State != CommandFailed || outcome.ErrorCode != "profile_invalid" {
		t.Fatalf("legacy marker crossed harness boundary state=%q code=%q err_present=%t", outcome.State, outcome.ErrorCode, err != nil)
	}
	if runtime.starts != 0 {
		t.Fatalf("invalid managed template reached external runtime: starts=%d", runtime.starts)
	}

	fx := core.HarnessInstance{
		ID: "profile-node/fx", Node: "profile-node", Kind: core.HarnessFX, Version: "fixture-v1",
		Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
	}
	legacy := WorkerEnvelope{
		WorkerRef: "legacy-fx-worker", TurnID: "legacy-fx-turn", AttemptID: "legacy-fx-attempt",
		OriginalUserIntent: "legacy FX compatibility", Workspace: root, HarnessInstance: fx,
	}
	partialLegacyTemplate := legacy
	partialLegacyTemplate.WorkerRef, partialLegacyTemplate.TurnID, partialLegacyTemplate.AttemptID = "legacy-fx-partial", "legacy-fx-partial-turn", "legacy-fx-partial-attempt"
	partialLegacyTemplate.LegacyWorkerTemplate = true
	partialLegacyTemplate.Profile.AllowTools = []string{"shell"}
	outcome, err = execution.HandleCommand(ctx, profileDispatchCommand(partialLegacyTemplate, "partial-legacy-template"))
	if err != nil || outcome.State != CommandFailed || outcome.ErrorCode != "profile_invalid" {
		t.Fatalf("partial legacy template crossed boundary state=%q code=%q err_present=%t", outcome.State, outcome.ErrorCode, err != nil)
	}
	outcome, err = execution.HandleCommand(ctx, profileDispatchCommand(legacy, "legacy-fx-dispatch"))
	if err != nil || outcome.State != CommandFailed || outcome.ErrorCode != "profile_missing" || runtime.starts != 0 {
		t.Fatalf("FX without durable legacy evidence state=%q code=%q starts=%d err_present=%t", outcome.State, outcome.ErrorCode, runtime.starts, err != nil)
	}
	provenLegacyFX := legacy
	provenLegacyFX.WorkerRef, provenLegacyFX.TurnID, provenLegacyFX.AttemptID = "legacy-fx-worker-proven", "legacy-fx-turn-proven", "legacy-fx-attempt-proven"
	provenLegacyFX.LegacyWorkerTemplate = true
	outcome, err = execution.HandleCommand(ctx, profileDispatchCommand(provenLegacyFX, "legacy-fx-template-marker"))
	if err != nil || outcome.State != CommandAccepted || runtime.starts != 1 {
		t.Fatalf("explicit legacy FX template compatibility state=%q starts=%d err_present=%t", outcome.State, runtime.starts, err != nil)
	}
	managedFX := legacy
	managedFX.WorkerRef, managedFX.TurnID, managedFX.AttemptID = "managed-fx-worker", "managed-fx-turn", "managed-fx-attempt"
	managedFX.Profile = ManagedProfile{Version: "synthetic-fx-v1", Name: "worker", Content: syntheticEnvelopeProfile,
		SourceHash: "synthetic-fx-source", Runtime: string(core.HarnessFX), Delivery: "workspace_instructions", AllowTools: []string{"fx-only-tool"}}
	managedFX.Profile.Hash = managedFX.Profile.SnapshotHash()
	outcome, err = execution.HandleCommand(ctx, profileDispatchCommand(managedFX, "managed-fx-dispatch"))
	if err != nil || outcome.State != CommandAccepted || runtime.starts != 2 {
		t.Fatalf("managed FX Worker template state=%q starts=%d err_present=%t", outcome.State, runtime.starts, err != nil)
	}
}

func profileDispatchCommand(envelope WorkerEnvelope, commandID string) Command {
	return Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{
		Metadata: core.CommandMetadata{
			CommandID: commandID, Node: envelope.HarnessInstance.Node, HarnessInstanceID: envelope.HarnessInstance.ID,
			WorkerRef: envelope.WorkerRef, TurnID: envelope.TurnID, AttemptID: envelope.AttemptID,
			IssuedAt: time.Now().UTC(),
		},
		Envelope: envelope,
	}}
}

func TestOpenCodeRuntimeStillRejectsMissingWorkerTemplate(t *testing.T) {
	workspace := t.TempDir()
	runtime := OpenCodeRuntime{Command: filepath.Join(workspace, "must-not-launch"), DataHome: filepath.Join(workspace, "native"), LegacyDataHome: true}
	_, err := runtime.Start(context.Background(), StartRequest{WorkerRef: "synthetic-worker", Workspace: workspace})
	if err == nil || !strings.Contains(err.Error(), "managed Profile is required") {
		t.Fatal("direct OpenCode Start must retain the managed Profile guard")
	}
}

func TestWorkerTemplateFailureSentinelsRemainSafeAndTyped(t *testing.T) {
	for _, err := range []error{ErrManagedProfileRequired, ErrManagedProfileInvalid} {
		if _, _, ok := managedProfileFailure(err); !ok {
			t.Fatalf("managed Profile error was not classified")
		}
	}
	if errors.Is(ErrManagedProfileRequired, ErrManagedProfileInvalid) {
		t.Fatal("missing and invalid Profile categories must remain distinct")
	}
}
