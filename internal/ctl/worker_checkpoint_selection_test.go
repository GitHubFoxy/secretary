package ctl

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestNodeContinuationSelectsExactCheckpoint(t *testing.T) {
	runNodeCheckpointSelection(t, false)
}

func TestNodeLegacyFXContinuationRetainsCheckpoint(t *testing.T) {
	runNodeCheckpointSelection(t, true)
}

func runNodeCheckpointSelection(t *testing.T, legacyFX bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "node.json")
	local, err := node.OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { local.Close() }()
	acp := node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestIdleFollowUpACPProcess$"}, DrainPromptEvents: true, TerminalMessageGrouping: true}
	var runtime node.Runtime = acp
	if legacyFX {
		runtime = node.FXRuntime{ACPRuntime: acp}
	}
	execution := node.NewExecutionNode("synthetic-node", runtime, local)
	instance := core.HarnessInstance{ID: "synthetic-node/opencode", Node: "synthetic-node", Kind: core.HarnessOpenCode, Version: "2.0.22", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, ModelIDs: []core.ObservedModelID{"fixture/model"}}
	profile := node.ManagedProfile{Version: "synthetic-v1", Name: "worker", Content: "Synthetic instructions", Runtime: "opencode", Model: "fixture/model", SourceHash: "synthetic-source", Delivery: "native", Skills: []node.ManagedSkill{}, AllowTools: []string{"read"}}
	profile.Hash = profile.SnapshotHash()
	workspace := t.TempDir()
	if os.WriteFile(filepath.Join(workspace, "private-nonce"), []byte("synthetic-private-nonce"), 0600) != nil {
		t.Fatal("private fixture unavailable")
	}
	original := node.WorkerEnvelope{WorkerRef: "fixture-worker", TurnID: "initial-turn", AttemptID: "initial-attempt", OriginalUserIntent: "remember synthetic-private-nonce", Workspace: workspace, HarnessInstance: instance, Model: "fixture/model", Profile: profile}
	if legacyFX {
		original.HarnessInstance.Kind = core.HarnessFX
		original.HarnessInstance.ID = "synthetic-node/fx"
		original.Profile = node.ManagedProfile{}
		original.Model = ""
		original.LegacyWorkerTemplate = true
		instance = original.HarnessInstance
	}
	metadata := func(id string, e node.WorkerEnvelope) core.CommandMetadata {
		return core.CommandMetadata{CommandID: id, Node: instance.Node, HarnessInstanceID: instance.ID, WorkerRef: e.WorkerRef, TurnID: e.TurnID, AttemptID: e.AttemptID, IssuedAt: time.Now().UTC()}
	}
	dispatch := func(id string, e node.WorkerEnvelope) node.Command {
		return node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: metadata(id, e), Envelope: e}}
	}
	execute := func(command node.Command) {
		t.Helper()
		outcome, err := execution.HandleCommand(ctx, command)
		if err != nil || outcome.State != node.CommandAccepted {
			t.Fatalf("checkpoint execution rejected: state=%s code=%s err_present=%t", outcome.State, outcome.ErrorCode, err != nil)
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
	first := dispatch("initial", original)
	execute(first)
	await(original.AttemptID)
	initialMapping, ok := local.SessionMapping(original.AttemptID)
	if !ok {
		t.Fatal("initial checkpoint missing")
	}
	if legacyFX {
		// Model a pre-marker Node record while retaining the native history
		// established by the real external executable above.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]json.RawMessage
		var commands map[string]node.CommandRecord
		if json.Unmarshal(data, &state) != nil || json.Unmarshal(state["commands"], &commands) != nil {
			t.Fatal("legacy synthetic state invalid")
		}
		record := commands["initial"]
		var historic node.Command
		if json.Unmarshal(record.CommandJSON, &historic) != nil {
			t.Fatal("legacy command invalid")
		}
		historic.Dispatch.Envelope.LegacyWorkerTemplate = false
		record.CommandJSON, err = json.Marshal(historic)
		if err != nil {
			t.Fatal(err)
		}
		commands["initial"] = record
		state["commands"], err = json.Marshal(commands)
		if err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(state)
		if err != nil || os.WriteFile(path, data, 0600) != nil {
			t.Fatal("legacy synthetic record failed")
		}
		local, err = node.OpenLocalStore(path)
		if err != nil {
			t.Fatal(err)
		}
		execution = node.NewExecutionNode("synthetic-node", runtime, local)
	}
	// The newest mapping deliberately belongs to another real external session.
	// Following map iteration order or a latest-session heuristic loses the nonce.
	foreign := original
	foreign.WorkerRef = "other-worker"
	foreign.AttemptID = "other-attempt"
	foreign.TurnID = "other-turn"
	foreign.OriginalUserIntent = "remember other-nonce"
	execute(dispatch("foreign", foreign))
	await(foreign.AttemptID)
	foreignMapping, ok := local.SessionMapping(foreign.AttemptID)
	if !ok || foreignMapping.RuntimeSessionID == initialMapping.RuntimeSessionID {
		t.Fatal("fixture did not establish distinct native identities")
	}
	next := original
	next.AttemptID = "followup-attempt"
	next.TurnID = "followup-turn"
	next.PreviousAttemptID = original.AttemptID
	next.OriginalUserIntent = "recall"
	followup := dispatch("followup", next)
	execute(followup)
	await(next.AttemptID)
	mapping, ok := local.SessionMapping(next.AttemptID)
	if !ok || mapping.RuntimeSessionID != initialMapping.RuntimeSessionID {
		t.Fatal("continuation selected a foreign/latest mapping")
	}
	events, _ := local.PendingEvents()
	results := 0
	for _, event := range events {
		var decoded node.NodeEvent
		if json.Unmarshal(event.Payload, &decoded) == nil && decoded.Outcome != nil && decoded.Outcome.AttemptID == next.AttemptID {
			results++
			if decoded.Outcome.Summary != "remembered=true" {
				t.Fatal("exact checkpoint lost history")
			}
		}
	}
	if results != 1 {
		t.Fatal("continuation did not publish one canonical outcome")
	}
	execute(followup)
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}
	local, err = node.OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	execution = node.NewExecutionNode("synthetic-node", runtime, local)
	resume := node.Command{Kind: node.CommandResume, Resume: &node.ResumeCommand{Metadata: metadata("explicit-checkpoint-resume", next), Envelope: next}}
	execute(resume)
	reopened, ok := local.SessionMapping(next.AttemptID)
	if !ok || reopened.RuntimeSessionID != initialMapping.RuntimeSessionID {
		t.Fatal("explicit checkpoint Resume replaced native identity")
	}
	if legacyFX {
		record, err := local.Command("initial")
		if err != nil {
			t.Fatal(err)
		}
		var historic node.Command
		if json.Unmarshal(record.CommandJSON, &historic) != nil || historic.Dispatch.Envelope.LegacyWorkerTemplate {
			t.Fatal("continuation rewrote legacy FX command history")
		}
	}
	files, _ := filepath.Glob(filepath.Join(workspace, "private-native-*"))
	if len(files) != 2 {
		t.Fatal("Follow-up or explicit Resume created a replacement native session")
	}
	execute(node.Command{Kind: node.CommandCancel, Cancel: &node.CancelCommand{Metadata: metadata("cleanup", next)}})
}
