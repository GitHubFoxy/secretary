package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// An explicit owner remember request is persisted, the Server DB is reopened,
// and the next Worker receives the memory in a real synthetic ACP subprocess's
// native system instructions. The subprocess also receives session/prompt and
// produces a Result. No real Codex/Claude credentials are used in this test.
func TestT07RememberRestartWorkerACPReceivesMemory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secretary.db")
	userPath := filepath.Join(dir, "user.md")
	store, err := core.Open(ctx, dbPath)
	if err != nil { t.Fatal(err) }
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil { t.Fatal(err) }
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil { t.Fatal(err) }
	if _, err := store.SaveUserDocument(ctx, userPath, "# Legacy context\nOwns a cat.\n"); err != nil { t.Fatal(err) }
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy",
	}); err != nil { t.Fatal(err) }
	facts, err := store.ReadMemoryDocument(ctx, userPath, core.MemoryFacts)
	if err != nil { t.Fatal(err) }
	prefs, err := store.ReadMemoryDocument(ctx, userPath, core.MemoryPreferences)
	if err != nil { t.Fatal(err) }
	ownerTurn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Remember that I prefer concise Go examples")
	if err != nil { t.Fatal(err) }
	if _, err := store.StartSecretaryTurn(ctx, ownerTurn.ID); err != nil { t.Fatal(err) }
	if _, err := store.RememberOwnerMemory(ctx, userPath, core.MemoryFacts, "Prefers Go examples", facts.Revision, person.ID, capability, ownerTurn.ID, ownerTurn.InputID); err != nil { t.Fatal(err) }
	if _, err := store.RememberOwnerMemory(ctx, userPath, core.MemoryPreferences, "Concise replies", prefs.Revision, person.ID, capability, ownerTurn.ID, ownerTurn.InputID); err != nil { t.Fatal(err) }
	if _, err := store.FinishSecretaryTurn(ctx, ownerTurn.ID, core.SecretaryTurnSucceeded, ""); err != nil { t.Fatal(err) }
	if err := store.Close(); err != nil { t.Fatal(err) }
	store, err = core.Open(ctx, dbPath)
	if err != nil { t.Fatal(err) }
	defer store.Close()
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID: "t07-memory", Name: "Memory ACP fixture",
		Mappings: []core.ProjectPathMapping{{Node: "local", Path: t.TempDir()}},
		Policy: core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessOpenCode}},
	})
	if err != nil { t.Fatal(err) }
	if _, err := store.EnrollNode(ctx, "local"); err != nil && !errors.Is(err, core.ErrNodeAlreadyEnrolled) { t.Fatal(err) }
	instance := core.HarnessInstance{
		ID: "local/opencode", Node: "local", Kind: core.HarnessOpenCode, Version: "2.0.22",
		Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}},
		ModelIDs: []core.ObservedModelID{"fixture/model"}, ReasoningLevels: []core.ObservedReasoningLevel{"xhigh"},
	}
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance},
	}, core.NodeHeartbeat{Capacity: 1}); err != nil { t.Fatal(err) }
	reportPath := filepath.Join(t.TempDir(), "memory-native-evidence.jsonl")
	t.Setenv("TEST_WORKER_PROFILE_REPORT", reportPath)
	t.Setenv("TEST_WORKER_PROFILE_TOOLS", "empty")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWorkerTemplateACPProcess$")
	local := node.NewLocal(node.OpenCodeRuntime{
		Command: cmd.Path, Arguments: cmd.Args[1:], DataHome: t.TempDir(), LegacyDataHome: true,
	})
	defer local.Close()
	sourceProfile := node.ManagedProfile{
		Version: "synthetic-config-v1", Name: "worker",
		Content: syntheticManagedWorkerInstructions, AllowTools: []string{},
		Hash: "synthetic-source-hash", Runtime: "opencode", Model: "fixture/model", Reasoning: "xhigh",
	}
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability, MemoryUserPath: userPath,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessOpenCode, ModelID: "fixture/model", Reasoning: "xhigh"},
		WorkerProfileSource: func() (node.ManagedProfile, error) { return sourceProfile, nil },
		Runtime: legacyLocalTestRuntime{Local: local},
	}
	expected, err := service.withOwnerMemory(ctx, sourceProfile)
	if err != nil { t.Fatal(err) }
	for _, item := range []string{"Prefers Go examples", "Concise replies", "Owns a cat.", "memory.md", "user_preferences.md", "user.md (legacy)"} {
		if !strings.Contains(expected.Content, item) {
			t.Fatalf("missing %q from new Worker owner context", item)
		}
	}
	t.Setenv("TEST_WORKER_PROFILE_EXPECTED_PROMPT", strings.TrimSpace(expected.Content))
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "Explain a Go function", ProjectID: project.ID, IdempotencyKey: "t07-native-acp",
	})
	if err != nil { t.Fatalf("spawn memory-aware Worker: %v", err) }
	evidence := readWorkerProfileEvidence(t, reportPath)
	if len(evidence) != 1 || !validWorkerProfileEvidence(evidence[0]) {
		t.Fatalf("ACP process did not receive the owner's exact memory-aware instructions: %#v", evidence)
	}
	var profile node.ManagedProfile
	if err := json.Unmarshal([]byte(details.Worker.ProfileSnapshot), &profile); err != nil { t.Fatal(err) }
	if profile.Content != expected.Content || details.Worker.Intent != "Explain a Go function" {
		t.Fatal("Worker profile/intent was modified or lost memory")
	}
	session, ok := local.Session(details.Worker.WorkerRef)
	if !ok { t.Fatal("no native ACP session") }
	select {
	case result, ok := <-session.Result():
		if !ok || result.Status == "" {
			t.Fatalf("ACP session returned no terminal Result: %#v", result)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("ACP subprocess did not answer session/prompt")
	}
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil { t.Fatal(err) }
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "Memory-aware ACP result",
	}); err != nil { t.Fatal(err) }
}
