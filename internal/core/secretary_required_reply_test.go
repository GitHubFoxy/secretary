package core

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestRequiredAddressedReplyCompletionCommitsExactEvidenceAndSurvivesReopen(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reply     bool
		state     SecretaryTurnState
		evidence  SecretaryCompletionEvidence
		wantState SecretaryTurnState
		wantCode  string
	}{
		{"missing", false, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "assistant_final", TerminalClass: "end_turn", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true, ResponsePresent: true}, SecretaryTurnFailed, "addressed_reply_missing"},
		{"exact final", true, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "assistant_final", TerminalClass: "end_turn", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true, ResponsePresent: true}, SecretaryTurnSucceeded, "completed"},
		{"exact MCP only", true, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "addressed_reply_only", TerminalClass: "end_turn", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true}, SecretaryTurnSucceeded, "completed"},
		{"unknown terminal with reply", true, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "addressed_reply_only", TerminalClass: "other", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true}, SecretaryTurnFailed, "native_terminal_invalid"},
		{"empty assistant response with reply", true, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "assistant_final", TerminalClass: "end_turn", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true}, SecretaryTurnFailed, "native_terminal_invalid"},
		{"RPC error with reply", true, SecretaryTurnFailed, SecretaryCompletionEvidence{Branch: "invalid"}, SecretaryTurnFailed, "native_terminal_invalid"},
		{"undrained with reply", true, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "assistant_final", TerminalValid: true, RPCSucceeded: true}, SecretaryTurnFailed, "native_terminal_invalid"},
		{"progress with reply", true, SecretaryTurnSucceeded, SecretaryCompletionEvidence{Branch: "progress", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true}, SecretaryTurnFailed, "native_terminal_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "secretary.db")
			store, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { store.Close() }()
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
			if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic"); err != nil {
				t.Fatal(err)
			}
			if err := store.SetSecretaryPolicySnapshot(ctx, SecretaryPolicySnapshot{Version: "v1", Harness: "opencode", Model: "fixture/model", Reasoning: "xhigh", ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "h", ProfileContent: "synthetic", ReplyContractVersion: SecretaryReplyContractAddressedV1}); err != nil {
				t.Fatal(err)
			}
			turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			turn, err = store.StartSecretaryTurn(ctx, turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reply {
				if _, _, err := store.RecordSecretaryReply(ctx, person.ID, capability, turn.ID, turn.InputID, "typed reply"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.FinishSecretaryTurnWithRequiredReply(ctx, turn.ID, "foreign-input", tc.state, tc.evidence); !errors.Is(err, ErrInvalidSecretaryOrigin) {
				t.Fatal("foreign input completed turn")
			}
			if _, err := store.FinishSecretaryTurnWithRequiredReply(ctx, turn.ID, turn.InputID, SecretaryTurnState("unknown"), tc.evidence); err == nil {
				t.Fatal("unknown status became success")
			}
			completed, err := store.FinishSecretaryTurnWithRequiredReply(ctx, turn.ID, turn.InputID, tc.state, tc.evidence)
			if err != nil || completed.State != tc.wantState || completed.Error != "" && completed.Error != tc.wantCode {
				t.Fatalf("completion mismatch state=%s code=%s", completed.State, completed.Error)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := store.SecretaryTurn(ctx, turn.ID)
			if err != nil || persisted.State != tc.wantState {
				t.Fatal("terminal state was not durable")
			}
			events, err := store.SecretaryEvents(ctx, turn.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			var recorded SecretaryCompletionEvidence
			finished := 0
			for _, event := range events {
				if event.Kind == SecretaryTurnFinishedEvent {
					finished++
					var payload struct {
						Completion SecretaryCompletionEvidence `json:"completion"`
					}
					if json.Unmarshal(event.Payload, &payload) != nil {
						t.Fatal("invalid public completion evidence")
					}
					recorded = payload.Completion
				}
			}
			wantCount := 0
			if tc.reply {
				wantCount = 1
			}
			if finished != 1 || !recorded.Committed || recorded.Code != tc.wantCode || recorded.ReplyCount != wantCount || recorded.EntryPresent != tc.reply || recorded.Branch != tc.evidence.Branch {
				t.Fatal("committed evidence does not describe actual completion")
			}
			// Terminal replay cannot mutate state or invent a second entry/event.
			if _, err := store.FinishSecretaryTurnWithRequiredReply(ctx, turn.ID, turn.InputID, tc.state, tc.evidence); !errors.Is(err, ErrInvalidTransition) {
				t.Fatal("terminal completion replay changed state")
			}
			if tc.reply {
				if _, duplicate, err := store.RecordSecretaryReply(ctx, person.ID, capability, turn.ID, turn.InputID, "typed reply"); err != nil || !duplicate {
					t.Fatal("durable addressed reply replay failed")
				}
			}
			entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, entry := range entries {
				if entry.Kind == EntrySecretary {
					if entry.TurnID != turn.ID {
						t.Fatalf("canonical reply lost turn identity: %#v", entry)
					}
					count++
					if entry.Body != "typed reply" {
						t.Fatal("assistant fallback leaked")
					}
				}
			}
			if count != wantCount {
				t.Fatal("missing or duplicate canonical entry")
			}
		})
	}
}
