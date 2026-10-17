package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

type conversationalWorkerRuntime struct {
	mcpReceiptGapRuntime
	completions map[string]int
}

func (r *conversationalWorkerRuntime) Dispatch(ctx context.Context, _ string, worker core.Worker, _ core.Turn, attempt core.Phase4Attempt, _ core.DispatchResolution) error {
	if _, err := r.store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		return err
	}
	if r.completions == nil {
		r.completions = make(map[string]int)
	}
	r.completions[worker.ID]++
	summary := "Independent Worker Result"
	if worker.Intent == "Create a website" {
		if r.completions[worker.ID] == 1 {
			summary = "What should the website be about?"
		} else {
			summary = "A clothing website. Do you need a checkout?"
		}
	}
	_, _, _, err := r.store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: summary,
	})
	return err
}

func TestMCPWorkerResultQuestionAndOwnerAnswerReusePersistentWorker(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(t.TempDir(), "user.md")
	if _, err := store.SaveUserDocument(ctx, userPath, "owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "test", Harness: "fx", Model: "m", Reasoning: "high",
		ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash",
		ProfileContent: "Route answers to the existing Worker.",
	}); err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID: "workspace", Name: "Workspace",
		Mappings: []core.ProjectPathMapping{{Node: "node", Path: t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNode(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	inventory := core.HarnessInventorySnapshot{
		Node: "node", ObservedAt: time.Now().UTC(),
		Instances: []core.HarnessInstance{{
			ID: "node/fx", Node: "node", Kind: core.HarnessFX,
			Version: "1", Status: core.HarnessReady,
			Authentication: core.HarnessAuthentication{Authenticated: true},
			Capabilities: core.HarnessCapabilities{
				Execution: []core.ExecutionCapability{core.CapabilityShell, core.CapabilityEdit},
			},
		}},
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", inventory, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	runtime := &conversationalWorkerRuntime{mcpReceiptGapRuntime: mcpReceiptGapRuntime{store: store}}
	handler := Secretary{
		Workers: ctl.WorkerService{
			Store: store, PersonID: person.ID, Capability: capability, Runtime: runtime,
			WorkerProfileSource: func() (node.ManagedProfile, error) {
				return node.ManagedProfile{
					Version: "test-v1", Name: "worker", Content: "Synthetic Worker",
					AllowTools: []string{"read"}, Hash: "synthetic-source", Runtime: "fx",
				}, nil
			},
		},
		ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
	}
	createInput, duplicate, err := store.AppendInbound(ctx, conversation.ID, "web", "create-website", "Create a website")
	if err != nil || duplicate {
		t.Fatalf("initial input: duplicate=%v err=%v", duplicate, err)
	}
	createTurn, duplicate, err := store.EnqueueSecretaryTurnForEntry(ctx, identity.ID, createInput.Body, createInput, nil)
	if err != nil || duplicate {
		t.Fatalf("initial Secretary turn: duplicate=%v err=%v", duplicate, err)
	}
	if _, err := store.StartSecretaryTurn(ctx, createTurn.ID); err != nil {
		t.Fatal(err)
	}
	spawn, _ := json.Marshal(map[string]any{
		"intent": "Create a website",
		"preferences": map[string]string{
			"project_id": project.ID, "node_id": "node",
			"harness_instance_id": "node/fx", "harness_kind": "fx",
		},
		"idempotency_key": "create-website",
		"secretary_turn_id": createTurn.ID, "input_id": createTurn.InputID,
	})
	firstResult, err := handler.Call(ctx, "spawn_worker", spawn)
	if err != nil {
		t.Fatalf("spawn Worker: %v", err)
	}
	firstWorker := firstResult.(core.WorkerDetails)
	if firstWorker.Worker.Status != core.WorkerIdle || len(firstWorker.Results) != 1 {
		t.Fatalf("Worker question did not complete only the Attempt: %#v", firstWorker)
	}
	if firstWorker.Results[0].Summary != "What should the website be about?" {
		t.Fatalf("first Result=%#v", firstWorker.Results[0])
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, createTurn.ID, createTurn.InputID, core.SecretaryTurnSucceeded, "", "Do not echo the Worker question.", nil); err != nil {
		t.Fatal(err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	questions := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult && entry.WorkerRef == firstWorker.Worker.WorkerRef && entry.Body == "What should the website be about?" {
			questions++
		}
		if entry.Kind == core.EntrySecretary && entry.Body == "Do not echo the Worker question." {
			t.Fatal("Secretary duplicated an attributed Worker Result")
		}
	}
	if questions != 1 {
		t.Fatalf("attributed Worker question count=%d, want 1", questions)
	}

	answer, duplicate, err := store.AppendInbound(ctx, conversation.ID, "web", "answer-website", "An online clothing shop")
	if err != nil || duplicate {
		t.Fatalf("owner answer: duplicate=%v err=%v", duplicate, err)
	}
	contextBeforeAnswer, err := store.ReconstructSecretaryContext(ctx, identity.ID, userPath, 20)
	if err != nil {
		t.Fatal(err)
	}
	seenQuestion := false
	for _, entry := range contextBeforeAnswer.RecentEntries {
		if entry.Kind == core.EntryWorkerResult && entry.WorkerRef == firstWorker.Worker.WorkerRef && entry.Body == "What should the website be about?" {
			seenQuestion = true
		}
	}
	if !seenQuestion || len(contextBeforeAnswer.OpenWorkers) != 1 {
		t.Fatalf("Secretary cannot correlate the owner's answer with the attributed Worker Result: %#v", contextBeforeAnswer)
	}
	answerTurn, duplicate, err := store.EnqueueSecretaryTurnForEntry(ctx, identity.ID, answer.Body, answer, nil)
	if err != nil || duplicate {
		t.Fatalf("answer Secretary turn: duplicate=%v err=%v", duplicate, err)
	}
	if _, err := store.StartSecretaryTurn(ctx, answerTurn.ID); err != nil {
		t.Fatal(err)
	}
	followUp, _ := json.Marshal(map[string]string{
		"worker_ref": firstWorker.Worker.WorkerRef, "text": answer.Body,
		"idempotency_key": "answer-website",
		"secretary_turn_id": answerTurn.ID, "input_id": answerTurn.InputID,
	})
	secondResult, err := handler.Call(ctx, "message_worker", followUp)
	if err != nil {
		t.Fatalf("follow-up via existing Worker: %v", err)
	}
	secondWorker := secondResult.(core.WorkerDetails)
	if secondWorker.Worker.ID != firstWorker.Worker.ID || secondWorker.Worker.WorkerRef != firstWorker.Worker.WorkerRef {
		t.Fatalf("follow-up changed Worker binding: old=%#v new=%#v", firstWorker.Worker, secondWorker.Worker)
	}
	if len(secondWorker.Turns) != 2 || len(secondWorker.Results) != 2 {
		t.Fatalf("follow-up did not continue Task: %#v", secondWorker)
	}
	if secondWorker.Results[1].Summary != "A clothing website. Do you need a checkout?" {
		t.Fatalf("follow-up Result=%#v", secondWorker.Results[1])
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, answerTurn.ID, answerTurn.InputID, core.SecretaryTurnSucceeded, "", "Do not echo the follow-up Result.", nil); err != nil {
		t.Fatal(err)
	}
	repeated, duplicatedInput, err := store.AppendInbound(ctx, conversation.ID, "web", "answer-website", "An online clothing shop")
	if err != nil || !duplicatedInput || repeated.ID != answer.ID {
		t.Fatalf("owner answer replay duplicated input: entry=%#v duplicate=%v err=%v", repeated, duplicatedInput, err)
	}
	repeatedTurn, duplicatedTurn, err := store.EnqueueSecretaryTurnForEntry(ctx, identity.ID, repeated.Body, repeated, nil)
	if err != nil || !duplicatedTurn || repeatedTurn.ID != answerTurn.ID {
		t.Fatalf("owner answer replay duplicated Secretary turn: turn=%#v duplicate=%v err=%v", repeatedTurn, duplicatedTurn, err)
	}
	entries, err = store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	results := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult && entry.WorkerRef == firstWorker.Worker.WorkerRef {
			results++
		}
		if entry.Kind == core.EntrySecretary && entry.Body == "Do not echo the follow-up Result." {
			t.Fatal("Secretary repeated a Worker follow-up Result")
		}
	}
	if results != 2 {
		t.Fatalf("follow-up emitted %d Worker Results, want 2", results)
	}
}
