package ctl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

const syntheticManagedWorkerInstructions = "Synthetic worker profile instructions: inspect the fixture and report its status."

// TestWorkerTemplateACPProcess is the synthetic external OpenCode-compatible
// executable used by the public Phase4 dispatch test. It reports only safe
// booleans/metadata; Profile text is never written to test output or evidence.
func TestWorkerTemplateACPProcess(t *testing.T) {
	if !testProcessHasArgument("-test.run=^TestWorkerTemplateACPProcess$") {
		return
	}
	reportPath := os.Getenv("TEST_WORKER_PROFILE_REPORT")
	if reportPath == "" {
		t.Fatal("synthetic ACP report path is required")
	}
	var encoder = json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	modeMatches, modelMatches := false, false
	sessionKind, nativeIdentityReused := "", false
	var evidence workerProfileBoundaryEvidence
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		if request.Method == "session/cancel" {
			return
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new":
			sessionKind = "new"
			var params struct {
				Meta map[string]string `json:"_meta"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				t.Fatal("synthetic ACP session parameters are invalid")
			}
			evidence = inspectWorkerProfileAtProcessBoundary(os.Getenv("OPENCODE_CONFIG"), params.Meta)
			result = map[string]any{"sessionId": "synthetic-opencode-session"}
		case "session/set_config_option":
			var params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				t.Fatal("synthetic ACP config parameters are invalid")
			}
			switch params.ConfigID {
			case "mode":
				modeMatches = params.Value == evidence.AgentName
				result = map[string]any{"configOptions": []any{map[string]any{
					"id": "mode", "currentValue": params.Value,
					"options": []any{map[string]any{"value": params.Value, "description": evidence.ModeDescription}},
				}}}
			case "model":
				separator := strings.LastIndexByte(params.Value, '/')
				selectedModel, effort := "", ""
				if separator > 0 {
					selectedModel, effort = params.Value[:separator], params.Value[separator+1:]
				}
				modelMatches = params.Value == "fixture/model/xhigh"
				result = map[string]any{"configOptions": []any{
					map[string]any{"id": "mode", "currentValue": evidence.AgentName,
						"options": []any{map[string]any{"value": evidence.AgentName, "description": evidence.ModeDescription}}},
					map[string]any{"id": "model", "currentValue": selectedModel},
					map[string]any{"id": "effort", "currentValue": effort},
				}}
				writeWorkerProfileEvidence(t, reportPath, evidence, modeMatches, modelMatches, sessionKind, nativeIdentityReused)
			}
		case "session/prompt":
			result = map[string]any{"stopReason": "end_turn", "summary": "Synthetic fixture completed."}
		case "session/load":
			sessionKind = "load"
			var params struct {
				SessionID string            `json:"sessionId"`
				Meta      map[string]string `json:"_meta"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				t.Fatal("synthetic ACP resume parameters are invalid")
			}
			nativeIdentityReused = params.SessionID == "synthetic-opencode-session"
			evidence = inspectWorkerProfileAtProcessBoundary(os.Getenv("OPENCODE_CONFIG"), params.Meta)
			result = map[string]any{}
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Fatal("synthetic ACP response failed")
		}
	}
}

func testProcessHasArgument(want string) bool {
	for _, argument := range os.Args {
		if argument == want {
			return true
		}
	}
	return false
}

type workerProfileBoundaryEvidence struct {
	ProfileName        string `json:"profile_name"`
	ProfileVersion     string `json:"profile_version"`
	ProfileHashPresent bool   `json:"profile_hash_present"`
	ProfileHash        string `json:"profile_hash"`
	ToolsMatch         bool   `json:"tools_match"`
	InstructionsMatch  bool   `json:"instructions_match"`
	ReadAllowed        bool   `json:"read_allowed"`
	ShellNotGranted    bool   `json:"shell_not_granted"`
	AgentName          string `json:"agent_name"`
	ModeDescription    string `json:"mode_description"`
}

func inspectWorkerProfileAtProcessBoundary(path string, metadata map[string]string) workerProfileBoundaryEvidence {
	var config struct {
		DefaultAgent string `json:"default_agent"`
		Agents       map[string]struct {
			Description string `json:"description"`
			System      string `json:"system"`
			Permissions []struct {
				Action   string `json:"action"`
				Effect   string `json:"effect"`
				Resource string `json:"resource"`
			} `json:"permissions"`
		} `json:"agents"`
		Permissions []struct {
			Action string `json:"action"`
			Effect string `json:"effect"`
		} `json:"permissions"`
	}
	content, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(content, &config) != nil {
		return workerProfileBoundaryEvidence{}
	}
	agent, found := config.Agents[config.DefaultAgent]
	readAllowed, shellAllowed := false, false
	for _, permission := range agent.Permissions {
		if permission.Resource != "*" || permission.Effect != "allow" {
			continue
		}
		readAllowed = readAllowed || permission.Action == "read"
		shellAllowed = shellAllowed || permission.Action == "shell"
	}
	globalDenyAll := false
	for _, permission := range config.Permissions {
		globalDenyAll = globalDenyAll || permission.Action == "*" && permission.Effect == "deny"
	}
	expectedPrompt := syntheticManagedWorkerInstructions
	if os.Getenv("TEST_WORKER_PROFILE_SKILL") == "nonempty" {
		expectedPrompt += "\n\n## Managed skill: synthetic.md\n\nSynthetic managed skill."
	}
	return workerProfileBoundaryEvidence{
		ProfileName: metadata["secretaryProfile"], ProfileVersion: metadata["secretaryProfileVersion"],
		ProfileHashPresent: metadata["secretaryProfileHash"] != "" && agent.Description != "",
		ProfileHash:        metadata["secretaryProfileHash"],
		ToolsMatch:         readAllowed == (os.Getenv("TEST_WORKER_PROFILE_TOOLS") != "empty"),
		InstructionsMatch:  found && agent.System == expectedPrompt,
		ReadAllowed:        readAllowed, ShellNotGranted: globalDenyAll && !shellAllowed,
		AgentName: config.DefaultAgent, ModeDescription: agent.Description,
	}
}

func writeWorkerProfileEvidence(t *testing.T, path string, evidence workerProfileBoundaryEvidence, modeMatches, modelMatches bool, sessionKind string, nativeIdentityReused bool) {
	t.Helper()
	result := workerProfileProcessReport{workerProfileBoundaryEvidence: evidence, ModeSelectionMatches: modeMatches,
		ModelSelectionMatches: modelMatches, SessionKind: sessionKind, NativeIdentityReused: nativeIdentityReused}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal("synthetic ACP evidence encoding failed")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("synthetic ACP evidence file unavailable")
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		t.Fatal("synthetic ACP evidence write failed")
	}
	if err := file.Close(); err != nil {
		t.Fatal("synthetic ACP evidence close failed")
	}
}

type workerProfileProcessReport struct {
	workerProfileBoundaryEvidence
	ModeSelectionMatches  bool   `json:"mode_selection_matches"`
	ModelSelectionMatches bool   `json:"model_selection_matches"`
	SessionKind           string `json:"session_kind"`
	NativeIdentityReused  bool   `json:"native_identity_reused"`
}

func readWorkerProfileEvidence(t *testing.T, path string) []workerProfileProcessReport {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("synthetic ACP evidence was not created")
	}
	defer file.Close()
	var result []workerProfileProcessReport
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item workerProfileProcessReport
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatal("synthetic ACP evidence was invalid")
		}
		result = append(result, item)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal("synthetic ACP evidence could not be read")
	}
	return result
}

func TestPhase4DispatchPassesAuthoritativeWorkerTemplateToRuntime(t *testing.T) {
	for _, skills := range []struct {
		name  string
		value []node.ManagedSkill
	}{
		{"nil", nil}, {"empty", []node.ManagedSkill{}},
		{"nonempty", []node.ManagedSkill{{Path: "synthetic.md", Content: "Synthetic managed skill.", Hash: "synthetic-skill-hash"}}},
	} {
		for _, tools := range []struct {
			name  string
			value []string
		}{{"nil", nil}, {"empty", []string{}}, {"read", []string{"read"}}} {
			t.Run(skills.name+"-skills/"+tools.name+"-tools", func(t *testing.T) {
				t.Setenv("TEST_WORKER_PROFILE_SKILL", skills.name)
				if len(tools.value) == 0 {
					t.Setenv("TEST_WORKER_PROFILE_TOOLS", "empty")
				}
				workerTemplateRoundTrip(t, skills.value, tools.value)
			})
		}
	}
}

func workerTemplateRoundTrip(t *testing.T, skills []node.ManagedSkill, tools []string) {
	t.Helper()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "secretary.db")
	store, err := core.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID: "profile-dispatch", Name: "Profile dispatch",
		Mappings: []core.ProjectPathMapping{{Node: "local", Path: t.TempDir()}},
		Policy:   core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessOpenCode}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "local"); err != nil && !errors.Is(err, core.ErrNodeAlreadyEnrolled) {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{
		ID: "local/opencode", Node: "local", Kind: core.HarnessOpenCode, Version: "2.0.22",
		Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}},
		ModelIDs:     []core.ObservedModelID{"fixture/model"}, ReasoningLevels: []core.ObservedReasoningLevel{"xhigh"},
	}
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance},
	}, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(t.TempDir(), "profile-evidence.jsonl")
	t.Setenv("TEST_WORKER_PROFILE_REPORT", reportPath)
	command := exec.Command(os.Args[0], "-test.run=^TestWorkerTemplateACPProcess$")
	runtime := node.OpenCodeRuntime{Command: command.Path, Arguments: command.Args[1:], DataHome: t.TempDir(), LegacyDataHome: true}
	local := node.NewLocal(runtime)
	sourceProfile := node.ManagedProfile{
		Version: "synthetic-config-v1", Name: "worker", Content: syntheticManagedWorkerInstructions,
		Skills: skills, AllowTools: tools, Hash: "synthetic-source-hash", Runtime: "opencode",
		Model: "fixture/model", Reasoning: "xhigh",
	}
	// The source template is valid before the actual persistence boundary.
	beforeSave := sourceProfile
	beforeSave.SourceHash, beforeSave.Delivery = sourceProfile.Hash, "native"
	beforeSave.Hash = beforeSave.SnapshotHash()
	if err := beforeSave.ValidateWorkerBinding("opencode", "fixture/model", "xhigh", core.ProjectPolicy{}); err != nil {
		t.Fatal("synthetic template was invalid before save")
	}
	profileSourceCalls := 0
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessOpenCode, ModelID: "fixture/model", Reasoning: "xhigh"},
		WorkerProfileSource: func() (node.ManagedProfile, error) {
			profileSourceCalls++
			return sourceProfile, nil
		},
		Runtime: NodeRuntime{Local: local},
	}
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "inspect the synthetic fixture", ProjectID: project.ID, IdempotencyKey: "profile-dispatch-test",
	})
	if err != nil {
		t.Fatalf("Phase4 dispatch did not deliver the configured managed profile: %v", err)
	}
	evidence := readWorkerProfileEvidence(t, reportPath)
	if len(evidence) != 1 || !validWorkerProfileEvidence(evidence[0]) {
		t.Fatal("external runtime did not observe the exact managed Profile, model, or permission policy")
	}
	var storedProfile node.ManagedProfile
	if err := json.Unmarshal([]byte(details.Worker.ProfileSnapshot), &storedProfile); err != nil ||
		storedProfile.Content != syntheticManagedWorkerInstructions || storedProfile.Version != "synthetic-config-v1" ||
		storedProfile.Model != "fixture/model" || storedProfile.Reasoning != "xhigh" || storedProfile.Runtime != "opencode" {
		t.Fatal("Worker did not retain the exact bound Profile snapshot")
	}
	if !reflect.DeepEqual(storedProfile, beforeSave) || storedProfile.Hash != storedProfile.SnapshotHash() || evidence[0].ProfileHash != beforeSave.Hash {
		t.Fatal("storage changed hash, source identity, instructions, or collection representation")
	}
	publicWorker, err := json.Marshal(details.Worker)
	if err != nil || bytes.Contains(publicWorker, []byte(syntheticManagedWorkerInstructions)) ||
		bytes.Contains(publicWorker, []byte(`"profile_snapshot"`)) || bytes.Contains(publicWorker, []byte("synthetic-config-v1")) {
		t.Fatal("private Profile snapshot escaped through the public Worker DTO")
	}
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "synthetic completion"}); err != nil {
		t.Fatal(err)
	}
	if err := local.Remove(details.Worker.WorkerRef); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = core.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	service.Store = store
	reopened, err := store.Worker(ctx, details.Worker.ID)
	if err != nil || reopened.ProfileSnapshot != details.Worker.ProfileSnapshot {
		t.Fatal("Core reopen changed the frozen template")
	}
	// Editing the current source must not alter the Profile frozen on the Worker.
	sourceProfile.Version = "synthetic-config-v2"
	sourceProfile.Content = "Edited synthetic profile; must not reach the bound Worker."
	sourceProfile.Hash = "synthetic-source-hash-v2"
	followUp, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "continue with the same managed Profile", IdempotencyKey: "profile-follow-up"})
	if err != nil || followUp.ActionTurnID == "" || followUp.ActionTurnID == details.Turns[0].ID {
		t.Fatalf("frozen-profile Follow-up failed: action_turn=%t err=%v", followUp.ActionTurnID != "", err)
	}
	evidence = readWorkerProfileEvidence(t, reportPath)
	if len(evidence) != 2 || !validWorkerProfileEvidence(evidence[1]) || evidence[1].ProfileVersion != "synthetic-config-v1" || evidence[1].ProfileHash != beforeSave.Hash || profileSourceCalls != 1 {
		t.Fatal("Follow-up did not reuse the immutable Profile snapshot")
	}
	replayedWorker, _, _, _, found, err := store.ReplayWorkerCreation(ctx, "profile-dispatch-test")
	if err != nil || !found || replayedWorker.ProfileSnapshot != details.Worker.ProfileSnapshot || profileSourceCalls != 1 {
		t.Fatalf("idempotent replay preserved_snapshot=%t found=%t source_calls=%d err_present=%t",
			replayedWorker.ProfileSnapshot == details.Worker.ProfileSnapshot, found, profileSourceCalls, err != nil)
	}

	binding, err := store.ResolveWorkerBinding(ctx, details.Worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumeEnvelope := node.WorkerEnvelope{
		WorkerRef: details.Worker.WorkerRef, TurnID: details.Turns[0].ID, AttemptID: details.Attempts[0].ID,
		OriginalUserIntent: details.Worker.Intent, ProjectID: details.Worker.ProjectID, ProjectSnapshot: binding.Snapshot,
		Workspace: binding.Workspace, HarnessInstance: binding.HarnessInstance, Model: binding.Snapshot.Policy.ModelPin(),
		Reasoning: binding.Snapshot.Policy.Reasoning, Profile: storedProfile,
	}
	nodeStatePath := filepath.Join(t.TempDir(), "node-state.json")
	nodeState, err := node.OpenLocalStore(nodeStatePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = nodeState.Close() })
	inventory := core.HarnessInventorySnapshot{Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}
	firstExecution := node.NewExecutionNode("local", runtime, nodeState)
	if err := firstExecution.SetInventory(inventory); err != nil {
		t.Fatal(err)
	}
	metadata := func(commandID string) core.CommandMetadata {
		return core.CommandMetadata{CommandID: commandID, Node: "local", HarnessInstanceID: binding.HarnessInstance.ID,
			WorkerRef: details.Worker.WorkerRef, TurnID: resumeEnvelope.TurnID, AttemptID: resumeEnvelope.AttemptID, IssuedAt: time.Now().UTC()}
	}
	startCommand := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: metadata("profile-native-start"), Envelope: resumeEnvelope}}
	assertWorkerTemplateTamperingRejected(t, ctx, service, details, binding, firstExecution, startCommand, reportPath)
	startCommand = roundTripWorkerTemplateCommand(t, startCommand)
	if outcome, err := firstExecution.HandleCommand(ctx, startCommand); err != nil || outcome.State != node.CommandAccepted {
		t.Fatalf("Node did not start the bound Worker: accepted=%t err_present=%t", outcome.State == node.CommandAccepted, err != nil)
	}
	cancelCommand := node.Command{Kind: node.CommandCancel, Cancel: &node.CancelCommand{Metadata: metadata("profile-native-stop"), Reason: "synthetic restart boundary"}}
	if outcome, err := firstExecution.HandleCommand(ctx, cancelCommand); err != nil || outcome.State != node.CommandAccepted {
		t.Fatalf("synthetic Node restart boundary failed: accepted=%t err_present=%t", outcome.State == node.CommandAccepted, err != nil)
	}
	if err := nodeState.Close(); err != nil {
		t.Fatal("Node store close failed")
	}
	nodeState, err = node.OpenLocalStore(nodeStatePath)
	if err != nil {
		t.Fatal("Node store reopen failed")
	}
	resumedExecution := node.NewExecutionNode("local", runtime, nodeState)
	if err := resumedExecution.SetInventory(inventory); err != nil {
		t.Fatal(err)
	}
	resumeCommand := node.Command{Kind: node.CommandResume, Resume: &node.ResumeCommand{Metadata: metadata("profile-native-resume"), Envelope: resumeEnvelope}}
	resumeCommand = roundTripWorkerTemplateCommand(t, resumeCommand)
	if outcome, err := resumedExecution.HandleCommand(ctx, resumeCommand); err != nil || outcome.State != node.CommandAccepted {
		t.Fatalf("Node did not resume the original Worker session: accepted=%t err_present=%t", outcome.State == node.CommandAccepted, err != nil)
	}
	evidence = readWorkerProfileEvidence(t, reportPath)
	resumeEvidenceValid := len(evidence) >= 4 && evidence[2].SessionKind == "new" && validWorkerProfileEvidence(evidence[2])
	for index := 3; index < len(evidence); index++ {
		resumeEvidenceValid = resumeEvidenceValid && evidence[index].SessionKind == "load" && evidence[index].NativeIdentityReused &&
			validWorkerProfileEvidence(evidence[index]) && evidence[index].ProfileVersion == "synthetic-config-v1" && evidence[index].ProfileHash == beforeSave.Hash
	}
	if !resumeEvidenceValid {
		phase2, phase3, version3 := "", "", ""
		nativeReused, profile2, profile3 := false, false, false
		if len(evidence) > 2 {
			phase2, version3, profile2 = evidence[2].SessionKind, evidence[2].ProfileVersion, validWorkerProfileEvidence(evidence[2])
		}
		if len(evidence) > 3 {
			phase3, version3, nativeReused, profile3 = evidence[3].SessionKind, evidence[3].ProfileVersion, evidence[3].NativeIdentityReused, validWorkerProfileEvidence(evidence[3])
		}
		phases := make([]string, len(evidence))
		for index := range evidence {
			phases[index] = evidence[index].SessionKind
		}
		t.Fatalf("Resume profile evidence count=%d sequence=%q phases=%q/%q same_native=%t profile_ok=%t/%t version=%q",
			len(evidence), strings.Join(phases, ","), phase2, phase3, nativeReused, profile2, profile3, version3)
	}
	stopResumed := node.Command{Kind: node.CommandCancel, Cancel: &node.CancelCommand{Metadata: metadata("profile-native-resume-stop"), Reason: "synthetic fixture cleanup"}}
	if _, err := resumedExecution.HandleCommand(ctx, stopResumed); err != nil {
		t.Fatal("synthetic resumed process cleanup failed")
	}
}

func validWorkerProfileEvidence(evidence workerProfileProcessReport) bool {
	return evidence.ProfileName == "worker" && evidence.ProfileVersion == "synthetic-config-v1" && evidence.ProfileHashPresent &&
		evidence.InstructionsMatch && evidence.ToolsMatch && evidence.ShellNotGranted && evidence.ModeSelectionMatches && evidence.ModelSelectionMatches
}

func assertWorkerTemplateTamperingRejected(t *testing.T, ctx context.Context, service WorkerService, details core.WorkerDetails, binding core.ProjectDispatch, execution *node.ExecutionNode, base node.Command, reportPath string) {
	t.Helper()
	before := len(readWorkerProfileEvidence(t, reportPath))
	for index, mutation := range []struct {
		name  string
		apply func(*node.WorkerEnvelope)
	}{
		{"instructions", func(e *node.WorkerEnvelope) { e.Profile.Content += " changed" }},
		{"skills", func(e *node.WorkerEnvelope) {
			e.Profile.Skills = []node.ManagedSkill{{Path: "tampered.md", Content: "tampered", Hash: "tampered"}}
		}},
		{"tools", func(e *node.WorkerEnvelope) { e.Profile.AllowTools = []string{"shell"} }},
		{"hash", func(e *node.WorkerEnvelope) { e.Profile.Hash = "tampered" }},
		{"source", func(e *node.WorkerEnvelope) { e.Profile.SourceHash += " changed" }},
		{"version", func(e *node.WorkerEnvelope) { e.Profile.Version += " changed" }},
		{"reply-contract", func(e *node.WorkerEnvelope) {
			e.Profile.ReplyContractVersion = "unsupported"
			e.Profile.Hash = e.Profile.SnapshotHash()
		}},
		{"model", func(e *node.WorkerEnvelope) {
			e.Profile.Model = "fixture/other"
			e.Profile.Hash = e.Profile.SnapshotHash()
		}},
		{"policy", func(e *node.WorkerEnvelope) {
			e.Profile.AllowTools = []string{"shell"}
			e.Profile.Hash = e.Profile.SnapshotHash()
			e.ProjectSnapshot.Policy.Execution.DeniedCapabilities = []core.ExecutionCapability{core.CapabilityShell}
		}},
	} {
		t.Run("reject-"+mutation.name, func(t *testing.T) {
			command := roundTripWorkerTemplateCommand(t, base)
			command.Dispatch.Metadata.CommandID = "tampered-template-" + string(rune('a'+index))
			mutation.apply(&command.Dispatch.Envelope)
			command = roundTripWorkerTemplateCommand(t, command)
			worker := details.Worker
			encoded, err := json.Marshal(command.Dispatch.Envelope.Profile)
			if err != nil {
				t.Fatal("tampered synthetic encoding failed")
			}
			worker.ProfileSnapshot = string(encoded)
			changedBinding := binding
			changedBinding.Snapshot = command.Dispatch.Envelope.ProjectSnapshot
			if err := service.Runtime.Dispatch(ctx, command.Dispatch.Metadata.CommandID, worker, details.Turns[0], details.Attempts[0], core.DispatchResolution{ProjectDispatch: changedBinding}); !errors.Is(err, ErrWorkerProfileInvalid) {
				t.Fatal("Core/NodeRuntime accepted a tampered stored template")
			}
			outcome, err := execution.HandleCommand(ctx, command)
			if err != nil || outcome.State != node.CommandFailed || outcome.ErrorCode != "profile_invalid" {
				t.Fatalf("Node tamper rejection state=%q code=%q err_present=%t", outcome.State, outcome.ErrorCode, err != nil)
			}
		})
	}
	if len(readWorkerProfileEvidence(t, reportPath)) != before {
		t.Fatal("tampered template reached the external ACP process")
	}
}

func roundTripWorkerTemplateCommand(t *testing.T, command node.Command) node.Command {
	t.Helper()
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal("Node command encoding failed")
	}
	var decoded node.Command
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal("Node command decoding failed")
	}
	return decoded
}
