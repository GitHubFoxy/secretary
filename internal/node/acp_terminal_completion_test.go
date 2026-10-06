package node

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestAddressedReplyV1RequiresVerifiedNativeACPContract(t *testing.T) {
	profile := ManagedProfile{ReplyContractVersion: "addressed-reply-v1"}
	request := StartRequest{WorkerRef: "secretary", Workspace: t.TempDir(), Profile: profile, DeferInitialPrompt: true}
	if _, err := (ACPRuntime{Command: os.Args[0]}).Start(context.Background(), request); err == nil {
		t.Fatal("legacy ACP runtime accepted the opt-in completion contract")
	}
	if _, err := (ACPRuntime{Command: os.Args[0]}).Resume(context.Background(), request, "existing-session"); err == nil {
		t.Fatal("legacy ACP Resume accepted the opt-in completion contract")
	}
}

func TestACPNativeTerminalOutcomeCarriesTypedFailClosedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		wantStatus        string
		wantSummary       string
		wantEvidence      bool
		wantReason        TerminalStopReason
		wantAssistantSize uint64
		wantRPCError      bool
	}{
		{name: "mcp-only", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonEndTurn},
		{name: "missing-stop", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonUnspecified},
		{name: "malformed-stop", wantStatus: "failed", wantEvidence: false, wantRPCError: true},
		{name: "max-tokens", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonMaxTokens},
		{name: "refusal", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonRefusal},
		{name: "progress-only", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonEndTurn, wantAssistantSize: 1},
		{name: "empty-chunk", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonEndTurn, wantAssistantSize: 1},
		{name: "unknown-stop", wantStatus: "failed", wantEvidence: true, wantReason: TerminalStopReasonOther},
		{name: "canceled", wantStatus: "canceled", wantEvidence: true, wantReason: TerminalStopReasonCanceled},
		{name: "rpc-error", wantStatus: "failed", wantEvidence: false, wantRPCError: true},
		{name: "assistant-final", wantStatus: "succeeded", wantSummary: "actual assistant final text", wantEvidence: true, wantReason: TerminalStopReasonEndTurn, wantAssistantSize: 1},
		{name: "summary-only", wantStatus: "failed", wantSummary: "Terminal answer unavailable: ACP did not provide authoritative final-answer metadata.", wantEvidence: true, wantReason: TerminalStopReasonEndTurn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runtime := ACPRuntime{
				Command: os.Args[0], Arguments: []string{"-test.run=^TestACPAnswerFixtureProcess$"},
				Environment:             []string{"TEST_ACP_ANSWER=" + tc.name},
				TerminalMessageGrouping: true, DrainPromptEvents: true,
			}
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "terminal-evidence", Workspace: t.TempDir(), DeferInitialPrompt: true})
			if err != nil {
				t.Fatal("ACP fixture session did not start")
			}
			defer session.Close()

			promptDone := make(chan error, 1)
			go func() { promptDone <- session.Prompt(ctx, "synthetic turn") }()
			var result Result
			var promptErr error
			promptFinished := false
			for result.Summary == "" || !promptFinished {
				select {
				case <-session.Activity():
				case result = <-session.Result():
				case promptErr = <-promptDone:
					promptFinished = true
				case <-ctx.Done():
					t.Fatal("ACP terminal fixture timed out")
				}
			}
			if (promptErr != nil) != tc.wantRPCError {
				t.Fatalf("ACP RPC/metadata error presence=%t, want=%t", promptErr != nil, tc.wantRPCError)
			}
			if result.Status != tc.wantStatus || (tc.wantSummary != "" && result.Summary != tc.wantSummary) {
				t.Fatalf("terminal result mismatch: status=%s expected_status=%s exact_final=%t", result.Status, tc.wantStatus, result.Summary == tc.wantSummary)
			}
			if (result.CompletionEvidence != nil) != tc.wantEvidence {
				t.Fatalf("typed completion evidence presence=%t, want=%t", result.CompletionEvidence != nil, tc.wantEvidence)
			}
			if result.CompletionEvidence != nil {
				evidence := result.CompletionEvidence
				if evidence.Contract != TerminalCompletionOpenCodeV2 || evidence.StopReason != tc.wantReason || evidence.RPCSucceeded != true || evidence.DrainCompleted != true || evidence.AssistantChunks != tc.wantAssistantSize {
					t.Fatalf("typed terminal metadata mismatch: native_contract=%t end_turn=%t rpc_succeeded=%t drain_completed=%t assistant_chunks=%d", evidence.Contract == TerminalCompletionOpenCodeV2, evidence.StopReason == tc.wantReason, evidence.RPCSucceeded, evidence.DrainCompleted, evidence.AssistantChunks)
				}
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal("runtime Result could not be serialized")
			}
			var publicResult map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &publicResult); err != nil {
				t.Fatal("serialized Result was not valid JSON")
			}
			if len(publicResult) != 2 || publicResult["status"] == nil || publicResult["summary"] == nil {
				t.Fatal("internal terminal evidence leaked into the serialized Result shape")
			}
		})
	}
}
