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

func TestNodeContinuationReadinessReplayIsIdempotent(t *testing.T) {
	for _, kind := range []node.CommandKind{node.CommandDispatch, node.CommandResume} {
		for _, restart := range []bool{false, true} {
			name := string(kind) + "/terminal"
			if restart {
				name = string(kind) + "/uncertain-restart"
			}
			t.Run(name, func(t *testing.T) { runReadinessReplayFixture(t, kind, restart) })
		}
	}
}

func runReadinessReplayFixture(t *testing.T, kind node.CommandKind, restart bool) {
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
	envelope := node.WorkerEnvelope{WorkerRef: "fixture-worker", TurnID: "initial-turn", AttemptID: "initial-attempt", OriginalUserIntent: "remember synthetic-private-nonce", Workspace: workspace, HarnessInstance: instance, Model: "fixture/model", Profile: profile}
	meta := func(id string, e node.WorkerEnvelope) core.CommandMetadata {
		return core.CommandMetadata{CommandID: id, Node: instance.Node, HarnessInstanceID: instance.ID, WorkerRef: e.WorkerRef, TurnID: e.TurnID, AttemptID: e.AttemptID, IssuedAt: time.Now().UTC()}
	}
	first := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: meta("initial", envelope), Envelope: envelope}}
	if o, e := execution.HandleCommand(ctx, first); e != nil || o.State != node.CommandAccepted {
		t.Fatal("initial Dispatch rejected")
	}
	await := func(id string) {
		waitIdleFixture(t, ctx, func() bool {
			events, _ := local.PendingEvents()
			for _, event := range events {
				if event.EventID == "attempt-outcome-"+id {
					return true
				}
			}
			return false
		})
	}
	await(envelope.AttemptID)
	envelope.PreviousAttemptID = envelope.AttemptID
	envelope.TurnID = "held-turn"
	envelope.AttemptID = "held-attempt"
	envelope.OriginalUserIntent = "hold"
	command := node.Command{Kind: kind}
	if kind == node.CommandDispatch {
		command.Dispatch = &node.DispatchCommand{Metadata: meta("held", envelope), Envelope: envelope}
	} else {
		command.Resume = &node.ResumeCommand{Metadata: meta("held", envelope), Envelope: envelope}
	}
	readiness, err := execution.HandleCommand(ctx, command)
	if err != nil || readiness.State != node.CommandAccepted {
		t.Fatal("Follow-up readiness rejected")
	}
	waitIdleFixture(t, ctx, func() bool { _, err := os.Stat(filepath.Join(workspace, "private-hold-ready")); return err == nil })
	record, err := local.Command("held")
	if err != nil || record.State != node.CommandProcessing || record.Outcome != readiness {
		t.Fatal("durable ACK was not separated from execution state")
	}
	for i := 0; i < 3; i++ {
		replay, err := execution.HandleCommand(ctx, command)
		if err != nil || replay != readiness {
			t.Fatal("live duplicate lost its accepted ACK")
		}
	}
	// Exercise real filesystem failure at the public durable-store seam.
	probeEnvelope := envelope
	probeEnvelope.AttemptID = "store-failure-attempt"
	probeEnvelope.TurnID = "store-failure-turn"
	probe := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: meta("store-failure", probeEnvelope), Envelope: probeEnvelope}}
	before, _, err := local.ClaimCommand(probe)
	if err != nil {
		t.Fatal(err)
	}
	if os.Mkdir(path+".tmp", 0700) != nil {
		t.Fatal("store failure fixture unavailable")
	}
	ack := node.CommandOutcome{CommandID: probe.Metadata().CommandID, Kind: probe.Kind, State: node.CommandAccepted, TurnID: probeEnvelope.TurnID, AttemptID: probeEnvelope.AttemptID}
	if _, err := local.SaveCommandReadiness(probe.Metadata().CommandID, ack); err == nil {
		t.Fatal("store failure accepted volatile ACK")
	}
	after, err := local.Command(probe.Metadata().CommandID)
	if err != nil || after.State != before.State || after.Outcome != before.Outcome || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("failed readiness write leaked into replay state")
	}
	replay, err := execution.HandleCommand(ctx, command)
	if err != nil || replay != readiness {
		t.Fatal("live ACK replay required another store write")
	}
	if os.Remove(path+".tmp") != nil {
		t.Fatal("store fixture cleanup failed")
	}
	mapping, ok := local.SessionMapping(envelope.AttemptID)
	if !ok {
		t.Fatal("live mapping missing")
	}
	corrupt := mapping
	corrupt.WorkerRef = "foreign-worker"
	if local.SaveSessionMapping(corrupt) != nil {
		t.Fatal("foreign fixture failed")
	}
	rejected, err := execution.HandleCommand(ctx, command)
	if err != nil || rejected.State != node.CommandFailed || rejected.ErrorCode != "runtime_session_unavailable" {
		t.Fatal("stored ACK bypassed foreign checkpoint guard")
	}
	if local.SaveSessionMapping(mapping) != nil {
		t.Fatal("mapping fixture restore failed")
	}
	// A fresh Node must not turn even a saved accepted ACK into execution proof.
	reopened, err := node.OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	uncertain := node.NewExecutionNode(instance.Node, runtime, reopened)
	replay, err = uncertain.HandleCommand(ctx, command)
	if err != nil || replay.State == node.CommandAccepted {
		t.Fatal("restart reused live ACK without execution proof")
	}
	if restart {
		// Retain the durable crash image while the real prompt is live.
		// Stop the fixture process, discard observations that a crashed
		// Node could not persist, and restore only that private image.
		image, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		cleanup := node.Command{Kind: node.CommandCancel, Cancel: &node.CancelCommand{Metadata: meta("crash-cleanup", envelope)}}
		if o, e := execution.HandleCommand(ctx, cleanup); e != nil || o.State != node.CommandAccepted {
			t.Fatal("fixture process cleanup failed")
		}
		await(envelope.AttemptID)
		if os.WriteFile(path, image, 0600) != nil {
			t.Fatal("crash image restore failed")
		}
		local, err = node.OpenLocalStore(path)
		if err != nil {
			t.Fatal(err)
		}
		execution = node.NewExecutionNode(instance.Node, runtime, local)
		if execution.Restore(ctx) != nil {
			t.Fatal("uncertain restart failed")
		}
		record, err := local.Command("held")
		if err != nil || record.State != node.CommandInterrupted || record.Outcome.State != node.CommandInterrupted {
			t.Fatal("accepted ACK became execution proof after crash")
		}
		replay, err = execution.HandleCommand(ctx, command)
		if err != nil || replay != record.Outcome {
			t.Fatal("uncertain replay lost explicit interruption")
		}
	} else {
		// The live process can finish once; terminal replay retains its ACK.
		finish := node.Command{Kind: node.CommandSteering, Steering: &node.SteeringCommand{Metadata: meta("finish", envelope), Text: "finish-held"}}
		if o, e := execution.HandleCommand(ctx, finish); e != nil || o.State != node.CommandAccepted {
			t.Fatal("held fixture finish failed")
		}
		await(envelope.AttemptID)
		replay, err = execution.HandleCommand(ctx, command)
		if err != nil || replay != readiness {
			t.Fatal("terminal duplicate changed readiness")
		}
	}
	ops, err := os.ReadFile(filepath.Join(workspace, "private-session-operations"))
	if err != nil || string(ops) != "NL" {
		t.Fatal("duplicate created another process/session")
	}
	prompts, err := os.ReadFile(filepath.Join(workspace, "private-held-prompts"))
	if err != nil || string(prompts) != "H" {
		t.Fatal("duplicate sent another Prompt")
	}
	pending, _ := local.PendingEvents()
	results := 0
	outcomeID := "attempt-outcome-" + envelope.AttemptID
	if restart {
		outcomeID = "interrupted-" + command.Metadata().CommandID
	}
	for _, event := range pending {
		if event.EventID == outcomeID {
			results++
		}
	}
	if results != 1 {
		t.Fatal("duplicate produced another outcome")
	}
}
