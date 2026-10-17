package ctl

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type projectlessNativeRuntime struct {
	mu      sync.Mutex
	starts  []node.StartRequest
	resumes []node.StartRequest
}

func (r *projectlessNativeRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, request)
	return canceledSteeringSession{}, nil
}

func (r *projectlessNativeRuntime) Resume(_ context.Context, request node.StartRequest, sessionID string) (node.Session, error) {
	if sessionID != "local-session" {
		return nil, errors.New("native session identity changed")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resumes = append(r.resumes, request)
	return canceledSteeringSession{}, nil
}

func TestProjectlessWorkerServiceDispatchToLocalNodeAndFollowup(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "local"); err != nil {
		t.Fatal(err)
	}
	codex := core.HarnessInstance{
		ID: "local/codex", Node: "local", Kind: core.HarnessCodex, Version: "1",
		Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}},
	}
	claude := codex
	claude.ID, claude.Kind = "local/claude", core.HarnessClaudeCode
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{codex, claude},
	}, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	native := &projectlessNativeRuntime{}
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessCodex, PreferredHarnesses: []core.HarnessKind{core.HarnessClaudeCode}},
		WorkerProfileSource: syntheticWorkerTemplateSource(),
		Runtime: NodeRuntime{Local: node.NewLocal(native)},
	}

	automatic, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "build a website", IdempotencyKey: "projectless-auto"})
	if err != nil {
		t.Fatal(err)
	}
	if automatic.Worker.ProjectID != "" || automatic.Worker.NodeID != "local" || automatic.Worker.HarnessInstanceID != "local/codex" {
		t.Fatalf("automatic dispatch binding: %#v", automatic.Worker)
	}

	override, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "review the code", IdempotencyKey: "projectless-override",
		Preferences: WorkerPreferences{NodeID: "local", HarnessKind: core.HarnessClaudeCode},
	})
	if err != nil {
		t.Fatal(err)
	}
	if override.Worker.ProjectID != "" || override.Worker.HarnessInstanceID != "local/claude" {
		t.Fatalf("owner override was lost: %#v", override.Worker)
	}
	native.mu.Lock()
	if len(native.starts) != 2 || native.starts[0].HarnessInstance.ID != codex.ID || native.starts[1].HarnessInstance.ID != claude.ID {
		native.mu.Unlock()
		t.Fatalf("Execution node did not receive selected harness binding")
	}
	originalWorkspace := native.starts[0].Workspace
	native.mu.Unlock()
	if originalWorkspace == "" {
		t.Fatal("Execution node did not create projectless Worker workspace")
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, automatic.Attempts[0].ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "What should I build next?",
	}); err != nil {
		t.Fatal(err)
	}
	followup, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef: automatic.Worker.WorkerRef, Text: "Add authentication", IdempotencyKey: "projectless-followup",
	})
	if err != nil {
		t.Fatalf("projectless Follow-up did not resume native Worker: %v", err)
	}
	if followup.Worker.ID != automatic.Worker.ID || followup.Worker.HarnessInstanceID != automatic.Worker.HarnessInstanceID {
		t.Fatalf("Follow-up migrated Worker binding: %#v", followup.Worker)
	}
	native.mu.Lock()
	defer native.mu.Unlock()
	if len(native.resumes) != 1 || native.resumes[0].Workspace != originalWorkspace || native.resumes[0].HarnessInstance.ID != codex.ID {
		t.Fatalf("Follow-up did not reuse native session and workspace: %#v", native.resumes)
	}
}
