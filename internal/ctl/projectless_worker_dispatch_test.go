package ctl

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
 "strings"
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

type projectlessNativeSession struct {
	node.Session
	nativeID string
}

func (s projectlessNativeSession) ID() string { return s.nativeID }

func (r *projectlessNativeRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, request)
	return projectlessNativeSession{Session: canceledSteeringSession{}, nativeID: "native-" + request.WorkerRef}, nil
}

func (r *projectlessNativeRuntime) Resume(_ context.Context, request node.StartRequest, sessionID string) (node.Session, error) {
	if sessionID != "native-"+request.WorkerRef {
		return nil, errors.New("native session identity changed")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resumes = append(r.resumes, request)
	return projectlessNativeSession{Session: canceledSteeringSession{}, nativeID: sessionID}, nil
}

func TestProjectlessWorkerServiceDispatchThroughExecutionNodeAndFollowup(t *testing.T) {
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
	localStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
	if err != nil { t.Fatal(err) }
	defer localStore.Close()
	execution := node.NewExecutionNode("local", native, localStore)
	if err := execution.SetInventory(core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{codex, claude},
	}); err != nil { t.Fatal(err) }
	service := WorkerService{
		Store: store, PersonID: person.ID, Capability: capability,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessCodex, PreferredHarnesses: []core.HarnessKind{core.HarnessClaudeCode}},
		WorkerProfileSource: syntheticWorkerTemplateSource(),
		Runtime: &projectlessExecutionRuntime{executor: execution},
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
	if len(native.starts) != 2 || len(native.resumes) != 1 ||
		native.resumes[0].WorkerRef != automatic.Worker.WorkerRef ||
		native.resumes[0].Workspace != originalWorkspace ||
		native.resumes[0].HarnessInstance.ID != codex.ID {
		t.Fatalf("Follow-up must Resume exactly once without an extra native Start: starts=%#v resumes=%#v", native.starts, native.resumes)
	}
}


func TestWorkerServiceMergesLegacyProjectAndPartialOwnerPreferences(t *testing.T) {
	ctx, _, service, project := newWorkerService(t)
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "inspect", ProjectID: project.ID,
		Preferences: WorkerPreferences{NodeID: "node"},
		IdempotencyKey: "mixed-legacy-project-preferences",
	})
	if err != nil || details.Worker.ProjectID != project.ID || details.Worker.NodeID != "node" || details.Worker.HarnessInstanceID != "node/fx" {
		t.Fatalf("partial preferences discarded legacy Project selection: worker=%#v err=%v", details.Worker, err)
	}
}


func TestWorkerServiceRejectsProjectlessWorkspaceAtSpawnBoundary(t *testing.T) {
	ctx, _, service, _ := newWorkerService(t)
	runtime := service.Runtime.(*lifecycleRuntime)
	for _, attempt := range []SpawnWorkerRequest{
		{Intent: "inspect", Preferences: WorkerPreferences{Workspace: t.TempDir()}},
		{Intent: "inspect", Workspace: "../outside"},
		{Intent: "inspect", Preferences: WorkerPreferences{NodeID: "node", Workspace: "/"}},
	} {
		if _, err := service.SpawnWorker(ctx, attempt); !errors.Is(err, core.ErrWorkspaceOutsideRoot) {
			t.Fatalf("workspace override was accepted: request=%#v err=%v", attempt, err)
		}
	}
	if runtime.count("dispatch") != 0 {
		t.Fatal("untrusted projectless Workspace reached runtime")
	}
}

func TestT13AutomaticTelegramVideoRequiresImageInputCapability(t *testing.T) {
 ctx:=context.Background()
 store,err:=core.Open(ctx,filepath.Join(t.TempDir(),"t13.db"));if err!=nil{t.Fatal(err)}
 defer store.Close()
 person,conversation,err:=store.CreatePersonWithConversation(ctx);if err!=nil{t.Fatal(err)}
 cap,err:=store.RotateSecretaryCapability(ctx,person.ID);if err!=nil{t.Fatal(err)}
 if _,err=store.EnrollNode(ctx,"local");err!=nil{t.Fatal(err)}
 instance:=core.HarnessInstance{ID:"local/fx",Node:"local",Kind:core.HarnessFX,Version:"test",Status:core.HarnessReady,
  Authentication:core.HarnessAuthentication{Authenticated:true},
  Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityShell}}}
 if err=store.UpdateNodeHeartbeat(ctx,"local",core.HarnessInventorySnapshot{
  Node:"local",ObservedAt:time.Now().UTC(),Instances:[]core.HarnessInstance{instance},
 },core.NodeHeartbeat{Capacity:2});err!=nil{t.Fatal(err)}
 native:=&projectlessNativeRuntime{}
 svc:=WorkerService{Store:store,PersonID:person.ID,Capability:cap,
  WorkerPolicy:core.HarnessPolicy{DefaultHarness:core.HarnessFX},
  WorkerProfileSource:syntheticWorkerTemplateSource(),
  Runtime:legacyLocalTestRuntime{Local:node.NewLocal(native)}}
 prompt:="Describe the sampled video\n\nTelegram video (update 42, chat 100, message 8, file_unique_id video): artifact://art_0123456789abcdef0123456789abcdef"
 _,err=svc.SpawnWorker(ctx,SpawnWorkerRequest{Intent:prompt,IdempotencyKey:"telegram-video-no-capability"})
 if !errors.Is(err,core.ErrInvalidDispatchPin){t.Fatalf("unsupported automatic Worker selection not rejected: %v",err)}
 workers,err:=store.WorkersForConversation(ctx,conversation.ID)
 if err!=nil||len(workers)!=0{t.Fatalf("unsupported video created Worker: workers=%+v err=%v",workers,err)}
 entries,err:=store.EntriesTail(ctx,conversation.ID,0,20)
 if err!=nil{t.Fatal(err)}
 warned:=false
 for _,entry:=range entries {
  if entry.Kind==core.EntrySecretary && strings.Contains(entry.Body,"CapabilityImageInput") { warned=true }
 }
 if !warned{t.Fatal("unsupported DM video did not publish a durable Telegram-visible rejection")}
 native.mu.Lock();defer native.mu.Unlock()
 if len(native.starts)!=0{t.Fatal("unsupported native Worker received Telegram video")}
}
