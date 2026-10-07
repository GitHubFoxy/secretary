package ctl

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// These regressions integrate the independent Spec probes through public
// ExecutionNode/LocalStore and a real external ACP executable.
func TestNodeRecoveryDoesNotAcceptUnprovenFollowUp(t *testing.T) {
	for _, kind := range []node.CommandKind{node.CommandDispatch, node.CommandResume} {
		for _, fault := range []string{"", "ack-only", "foreign"} {
			t.Run(string(kind)+"/unsent-"+fault, func(t *testing.T) {
				runRecoveryCheckpointFixture(t, kind, fault, true)
			})
		}
	}
}

func TestNodeRecoveryCommandsRejectForeignCheckpoint(t *testing.T) {
	for _, fault := range []string{"foreign", "stale-workspace", "ambiguous"} {
		t.Run(fault, func(t *testing.T) { runRecoveryCheckpointFixture(t, node.CommandSteering, fault, false) })
	}
}

func runRecoveryCheckpointFixture(t *testing.T, kind node.CommandKind, fault string, processing bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.json")
	local, err := node.OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	runtime := node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestIdleFollowUpACPProcess$"}, DrainPromptEvents: true, TerminalMessageGrouping: true}
	execution := node.NewExecutionNode("synthetic-node", runtime, local)
	instance := core.HarnessInstance{ID: "synthetic-node/opencode", Node: "synthetic-node", Kind: core.HarnessOpenCode, Version: "2.0.22", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, ModelIDs: []core.ObservedModelID{"fixture/model"}}
	profile := node.ManagedProfile{Version: "synthetic-v1", Name: "worker", Content: "Synthetic instructions", Runtime: "opencode", Model: "fixture/model", SourceHash: "synthetic-source", Delivery: "native", Skills: []node.ManagedSkill{}, AllowTools: []string{"read"}}
	profile.Hash = profile.SnapshotHash()
	workspace := t.TempDir()
	os.WriteFile(filepath.Join(workspace, "private-nonce"), []byte("synthetic-private-nonce"), 0600)
	original := node.WorkerEnvelope{WorkerRef: "fixture-worker", TurnID: "initial-turn", AttemptID: "initial-attempt", OriginalUserIntent: "remember synthetic-private-nonce", Workspace: workspace, HarnessInstance: instance, Model: "fixture/model", Profile: profile}
	metadata := func(id string, e node.WorkerEnvelope) core.CommandMetadata {
		return core.CommandMetadata{CommandID: id, Node: instance.Node, HarnessInstanceID: instance.ID, WorkerRef: e.WorkerRef, TurnID: e.TurnID, AttemptID: e.AttemptID, IssuedAt: time.Now().UTC()}
	}
	dispatch := func(id string, e node.WorkerEnvelope) node.Command {
		return node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: metadata(id, e), Envelope: e}}
	}
	execute := func(command node.Command) {
		t.Helper()
		o, e := execution.HandleCommand(ctx, command)
		if e != nil || o.State != node.CommandAccepted {
			t.Fatal("synthetic checkpoint setup rejected")
		}
	}
	await := func(attempt string) {
		t.Helper()
		waitIdleFixture(t, ctx, func() bool {
			events, _ := local.PendingEvents()
			for _, event := range events {
				if event.EventID == "attempt-outcome-"+attempt {
					return true
				}
			}
			return false
		})
	}
	execute(dispatch("initial", original))
	await(original.AttemptID)
	mapping, ok := local.SessionMapping(original.AttemptID)
	if !ok {
		t.Fatal("initial checkpoint missing")
	}
	other := original
	other.WorkerRef = "other-worker"
	other.AttemptID = "other-attempt"
	other.TurnID = "other-turn"
	other.OriginalUserIntent = "remember other-nonce"
	execute(dispatch("other", other))
	await(other.AttemptID)
	otherMapping, ok := local.SessionMapping(other.AttemptID)
	if !ok {
		t.Fatal("foreign checkpoint missing")
	}
	next := original
	if processing {
		next.AttemptID = "unsent-attempt"
		next.TurnID = "unsent-turn"
		next.PreviousAttemptID = original.AttemptID
		next.OriginalUserIntent = "recall"
	}
	if fault == "foreign" {
		mapping.RuntimeSessionID = otherMapping.RuntimeSessionID
	}
	mapping.AttemptID, mapping.TurnID = next.AttemptID, next.TurnID
	if fault == "stale-workspace" {
		mapping.Workspace = t.TempDir()
	}
	if fault == "ambiguous" {
		conflicting := original
		conflicting.OriginalUserIntent = "different input"
		c := dispatch("ambiguous-checkpoint", conflicting)
		if _, _, err := local.ClaimCommand(c); err != nil {
			t.Fatal(err)
		}
		if _, err := local.CompleteCommand(c.Metadata().CommandID, node.CommandOutcome{State: node.CommandAccepted}); err != nil {
			t.Fatal(err)
		}
	}
	if err := local.SaveSessionMapping(mapping); err != nil {
		t.Fatal(err)
	}
	command := dispatch("recover", next)
	if kind == node.CommandResume {
		command = node.Command{Kind: kind, Resume: &node.ResumeCommand{Metadata: metadata("recover", next), Envelope: next}}
	}
	if processing {
		if _, _, err := local.ClaimCommand(command); err != nil {
			t.Fatal(err)
		}
		if fault == "ack-only" {
			if _, err := local.SaveCommandReadiness(command.Metadata().CommandID, node.CommandOutcome{CommandID: command.Metadata().CommandID, Kind: kind, State: node.CommandAccepted, TurnID: next.TurnID, AttemptID: next.AttemptID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	reopened, err := node.OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored := node.NewExecutionNode(instance.Node, runtime, reopened)
	before, _ := filepath.Glob(filepath.Join(workspace, "private-native-*"))
	if err := restored.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	if processing {
		record, err := reopened.Command("recover")
		if err != nil {
			t.Fatal(err)
		}
		if record.State != node.CommandInterrupted || record.Outcome.ErrorCode != "execution_state_unknown" {
			t.Fatalf("unproven recovery state=%s code=%s", record.State, record.Outcome.ErrorCode)
		}
		outcome, err := restored.HandleCommand(ctx, command)
		if err != nil || outcome != record.Outcome {
			t.Fatal("uncertain command replay changed its outcome")
		}
		pending, _ := reopened.PendingEvents()
		interruptions := 0
		for _, event := range pending {
			if event.EventID == "interrupted-recover" {
				interruptions++
			}
		}
		if interruptions != 1 || len(restored.ActiveAttempts()) != 0 {
			t.Fatal("unproven prompt was resumed instead of explicitly interrupted")
		}
	} else {
		command = node.Command{Kind: node.CommandSteering, Steering: &node.SteeringCommand{Metadata: metadata("foreign-steering", next), Text: "recall"}}
		outcome, err := restored.HandleCommand(ctx, command)
		if err != nil || outcome.State != node.CommandFailed || outcome.ErrorCode != "runtime_session_unavailable" {
			t.Fatal("recovery command accepted a foreign native checkpoint")
		}
	}
	after, _ := filepath.Glob(filepath.Join(workspace, "private-native-*"))
	if len(before) != len(after) {
		t.Fatal("recovery created a replacement native session")
	}
	if _, err := os.Stat(filepath.Join(workspace, "private-recall-sent")); !os.IsNotExist(err) {
		t.Fatal("recovery automatically replayed an uncertain prompt")
	}
}
