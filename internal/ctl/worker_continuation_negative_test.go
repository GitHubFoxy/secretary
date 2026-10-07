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

func TestNodeContinuationCheckpointFailsClosed(t *testing.T) {
	for _, kind := range []node.CommandKind{node.CommandDispatch, node.CommandResume} {
		for _, name := range []string{"missing", "foreign-worker", "foreign-attempt", "foreign-turn", "foreign-harness", "foreign-native", "corrupt-native", "stale-workspace", "binding", "template", "policy", "ambiguous", "no-predecessor"} {
			t.Run(string(kind)+"/"+name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				statePath := filepath.Join(t.TempDir(), "node.json")
				local, err := node.OpenLocalStore(statePath)
				if err != nil {
					t.Fatal(err)
				}
				defer local.Close()
				runtime := node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestIdleFollowUpACPProcess$"}, DrainPromptEvents: true, TerminalMessageGrouping: true}
				execution := node.NewExecutionNode("synthetic-node", runtime, local)
				instance := core.HarnessInstance{ID: "synthetic-node/opencode", Node: "synthetic-node", Kind: core.HarnessOpenCode, Version: "2.0.22", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, ModelIDs: []core.ObservedModelID{"fixture/model"}}
				profile := node.ManagedProfile{Version: "synthetic-v1", Name: "worker", Content: "Synthetic instructions", Runtime: "opencode", Model: "fixture/model", SourceHash: "synthetic-source", Delivery: "native", Skills: []node.ManagedSkill{}, AllowTools: []string{"read"}}
				profile.Hash = profile.SnapshotHash()
				envelope := node.WorkerEnvelope{WorkerRef: "fixture-worker", TurnID: "initial-turn", AttemptID: "initial-attempt", OriginalUserIntent: "remember synthetic-private-nonce", Workspace: t.TempDir(), HarnessInstance: instance, Model: "fixture/model", Profile: profile}
				meta := func(id string, e node.WorkerEnvelope) core.CommandMetadata {
					return core.CommandMetadata{CommandID: id, Node: instance.Node, HarnessInstanceID: instance.ID, WorkerRef: e.WorkerRef, TurnID: e.TurnID, AttemptID: e.AttemptID, IssuedAt: time.Now().UTC()}
				}
				initial := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: meta("initial", envelope), Envelope: envelope}}
				outcome, err := execution.HandleCommand(ctx, initial)
				if err != nil || outcome.State != node.CommandAccepted {
					t.Fatal("synthetic initial Dispatch failed")
				}
				waitIdleFixture(t, ctx, func() bool {
					events, _ := local.PendingEvents()
					for _, event := range events {
						if event.EventID == "attempt-outcome-initial-attempt" {
							return true
						}
					}
					return false
				})
				mapping, ok := local.SessionMapping(envelope.AttemptID)
				if !ok {
					t.Fatal("checkpoint missing")
				}
				next := envelope
				next.TurnID = "followup-turn"
				next.AttemptID = "followup-attempt"
				next.PreviousAttemptID = envelope.AttemptID
				next.OriginalUserIntent = "recall"
				switch name {
				case "missing":
					next.PreviousAttemptID = "absent"
				case "foreign-worker":
					mapping.WorkerRef = "other-worker"
				case "foreign-attempt":
					mapping.AttemptID = "other-attempt"
				case "foreign-turn":
					mapping.TurnID = "other-turn"
				case "foreign-harness":
					mapping.HarnessInstanceID = "other-harness"
				case "foreign-native":
					other := mapping
					other.WorkerRef = "foreign-owner"
					other.AttemptID = "foreign-attempt"
					other.TurnID = "foreign-turn"
					if err := local.SaveSessionMapping(other); err != nil {
						t.Fatal(err)
					}
				case "corrupt-native":
					mapping.RuntimeSessionID = "nonexistent-private-native"
				case "stale-workspace":
					mapping.Workspace = t.TempDir()
				case "binding":
					next.ApprovalPolicy = "required"
				case "template":
					next.Profile.Content = "Other valid frozen profile"
					next.Profile.Hash = next.Profile.SnapshotHash()
				case "policy":
					next.Profile.AllowTools = []string{"shell"}
					next.Profile.Hash = next.Profile.SnapshotHash()
				case "ambiguous":
					conflict := initial
					copy := *initial.Dispatch
					conflict.Dispatch = &copy
					conflict.Dispatch.Metadata.CommandID = "ambiguous"
					conflict.Dispatch.Envelope.OriginalUserIntent = "conflicting checkpoint"
					if _, _, err := local.ClaimCommand(conflict); err != nil {
						t.Fatal(err)
					}
					if _, err := local.CompleteCommand("ambiguous", node.CommandOutcome{State: node.CommandAccepted}); err != nil {
						t.Fatal(err)
					}
				case "no-predecessor":
					next.PreviousAttemptID = ""
				}
				if name == "foreign-attempt" {
					data, err := os.ReadFile(statePath)
					if err != nil {
						t.Fatal(err)
					}
					var state map[string]json.RawMessage
					var mappings map[string]node.LocalSessionMapping
					if json.Unmarshal(data, &state) != nil || json.Unmarshal(state["mappings"], &mappings) != nil {
						t.Fatal("synthetic state invalid")
					}
					mappings[envelope.AttemptID] = mapping
					state["mappings"], err = json.Marshal(mappings)
					if err != nil {
						t.Fatal(err)
					}
					data, err = json.Marshal(state)
					if err != nil || os.WriteFile(statePath, data, 0600) != nil {
						t.Fatal("synthetic corruption fixture failed")
					}
					local, err = node.OpenLocalStore(statePath)
					if err != nil {
						t.Fatal(err)
					}
					execution = node.NewExecutionNode("synthetic-node", runtime, local)
				} else if err := local.SaveSessionMapping(mapping); err != nil {
					t.Fatal(err)
				}
				before, _ := filepath.Glob(filepath.Join(envelope.Workspace, "private-native-*"))
				command := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: meta("negative", next), Envelope: next}}
				if kind == node.CommandResume {
					command = node.Command{Kind: node.CommandResume, Resume: &node.ResumeCommand{Metadata: meta("negative", next), Envelope: next}}
				}
				outcome, err = execution.HandleCommand(ctx, command)
				if err != nil || outcome.State != node.CommandFailed || outcome.ErrorCode != "runtime_session_unavailable" {
					t.Fatalf("unsafe continuation: state=%s code=%s err_present=%t", outcome.State, outcome.ErrorCode, err != nil)
				}
				if _, ok := local.SessionMapping(next.AttemptID); ok {
					t.Fatal("failed continuation saved replacement mapping")
				}
				after, _ := filepath.Glob(filepath.Join(envelope.Workspace, "private-native-*"))
				if len(before) != len(after) {
					t.Fatal("failed continuation called session/new")
				}
				replay, err := execution.HandleCommand(ctx, command)
				if err != nil || replay != outcome {
					t.Fatal("failed command replay was not idempotent")
				}
			})
		}
	}
}
