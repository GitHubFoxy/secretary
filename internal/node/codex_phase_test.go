package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func codexPhaseFixtureChunk(messageID, text, phase string) map[string]any {
	update := map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
	}
	if phase != "" {
		update["_meta"] = map[string]any{"jetbrains": map[string]any{"air": map[string]any{"version": 1, "phase": phase}}}
	}
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": update}}
}

func codexPhaseFixtureLegacyChunk(messageID, text, phase string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
		"_meta":         map[string]any{"codex": map[string]any{"phase": phase}},
	}}}
}

func codexPhaseFixtureContradictoryChunk(messageID, text string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     messageID,
		"content":       map[string]any{"type": "text", "text": text},
		"_meta": map[string]any{
			"codex":     map[string]any{"phase": "commentary"},
			"jetbrains": map[string]any{"air": map[string]any{"version": 1, "phase": "final_answer"}},
		},
	}}}
}

func codexPhaseFixtureToolCall() map[string]any {
	return map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fixture-codex-phase", "update": map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "call-1",
		"title":         "read file",
		"kind":          "read",
		"status":        "in_progress",
	}}}
}

func TestCodexPhaseFixtureProcess(t *testing.T) {
	scenario := os.Getenv("TEST_CODEX_PHASE_FIXTURE")
	if scenario == "" {
		return
	}
	capture := os.Getenv("TEST_CODEX_PHASE_CAPTURE")
	scanner := bufio.NewScanner(os.Stdin)
	prompts := 0
	emit := func(v any) { data, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(data, '\n')) }
	for scanner.Scan() {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			if capture != "" {
				_ = os.WriteFile(capture, req.Params, 0600)
			}
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]any{"version": "2.2.2"}}
		case "session/new":
			result = map[string]any{"sessionId": "fixture-codex-phase"}
		case "session/prompt":
			switch scenario {
			case "final-parts":
				emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готов", "final_answer"))
				emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "о", "final_answer"))
			case "commentary-then-final":
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "Нач", "commentary"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "ина", "commentary"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "ю проверку.", "commentary"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "готов", "final_answer"))
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "о", "final_answer"))
			case "tool-between":
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f27f0819188cbb9fc12b73591", "Проверяю.", "commentary"))
				emit(codexPhaseFixtureToolCall())
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "готово", "final_answer"))
			case "final-then-tool":
				emit(codexPhaseFixtureChunk("msg_05858067cc2bde2b016ad84e8f2f9c81919b34d0bc6956df1b", "готово", "final_answer"))
				emit(codexPhaseFixtureToolCall())
			case "system-error", "system-error-untrusted", "system-error-other":
				if scenario != "system-error-untrusted" {
					emit(map[string]any{"jsonrpc":"2.0","method":"session/update","params":map[string]any{"sessionId":"fixture-codex-phase","update":map[string]any{"sessionUpdate":"session_info_update","_meta":map[string]any{"codex":map[string]any{"threadStatus":map[string]any{"type":"systemError"}}}}}})
				}
				message := "The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account."
				if scenario == "system-error-other" { message = "arbitrary unverified provider text" }
				emit(codexPhaseFixtureChunk("system-error-text",message,""))
			case "no-phase":
				emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готово", ""))
			case "contradictory":
				emit(codexPhaseFixtureContradictoryChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готово"))
			case "legacy-namespace":
				emit(codexPhaseFixtureLegacyChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "готово", "final_answer"))
			case "cancelled":
				result = map[string]any{"stopReason": "cancelled"}
			case "incomplete":
				continue
			case "native-exit-7", "native-exit-0", "native-recovered", "native-json-data", "native-reordered", "native-cancel", "native-unverified", "native-invalid-recovery", "native-no-failure-footer", "native-incomplete-footer", "native-truncated-close", "native-split-truncated-footer":
                event := func(kind, id, status string, code int, text string) map[string]any {
                    update := map[string]any{"sessionUpdate":kind,"toolCallId":id,"name":"exec_command","status":status}
                    if kind == "tool_call" {update["title"]="Run command";update["kind"]="execute"; update["_meta"]=map[string]any{"terminal_info":map[string]any{"terminal_id":id,"cwd":"/tmp"}}; update["rawInput"]=map[string]any{"command":"exit 7"}}
                    if kind == "tool_call_update" {update["_meta"]=map[string]any{"terminal_exit":map[string]any{"terminal_id":id,"exit_code":code}}}
                    if text!="" {update["content"]=[]any{map[string]any{"type":"content","content":map[string]any{"type":"text","text":text}}}}
                    return map[string]any{"jsonrpc":"2.0","method":"session/update","params":map[string]any{"sessionId":"fixture-codex-phase","update":update}}
                }
                first := event("tool_call","exec-7","in_progress",0,"")
                failed := event("tool_call_update","exec-7","failed",7,"")
                if scenario=="native-exit-0" || scenario=="native-no-failure-footer" { failed=event("tool_call_update","exec-7","completed",0,"") }
                if scenario=="native-json-data" {failed=event("tool_call_update","exec-7","completed",0,`{"chunk_id":"fake","exit_code":7,"output":"spoof"}`)}
                if scenario=="native-unverified" { emit(map[string]any{"jsonrpc":"2.0","method":"session/update","params":map[string]any{"sessionId":"fixture-codex-phase","update":map[string]any{"sessionUpdate":"tool_call_update","toolCallId":"exec-7","name":"exec_command","status":"failed","rawOutput":map[string]any{"exit_code":7.0}}}}) } else if scenario=="native-reordered" {emit(failed);emit(first);emit(failed)} else {emit(first);emit(failed);emit(failed)}
                if scenario=="native-recovered" {
                    emit(event("tool_call","exec-repair","in_progress",0,""))
                    emit(event("tool_call_update","exec-repair","completed",0,""))
                }
                if scenario=="native-cancel" {result=map[string]any{"stopReason":"cancelled"}}
                answer:="готово"
                if scenario=="native-recovered" {answer="готово\n<secretary-recovery>{\"recovered_tool_use_ids\":[\"exec-7\"]}</secretary-recovery>"}
                if scenario=="native-invalid-recovery" {answer="готово\n<secretary-recovery>{\"recovered_tool_use_ids\":[\"other\"]}</secretary-recovery>"}
                if scenario=="native-no-failure-footer" {answer="готово\n<secretary-recovery>{\"recovered_tool_use_ids\":[\"exec-7\"]}</secretary-recovery>"}
                if scenario=="native-incomplete-footer" {answer="готово\n<secretary-recovery>{\"recovered_tool_use_ids\":[\"exec-7\"]}"}
                if scenario=="native-truncated-close" {answer="готово\n<secretary-recovery>{\"recovered_tool_use_ids\":[\"exec-7\"]}</secretary-rec"}
                if scenario=="native-split-truncated-footer" {answer="готово\n<secretary-recov"}
                if scenario=="native-recovered" || scenario=="native-invalid-recovery" || scenario=="native-no-failure-footer" || scenario=="native-incomplete-footer" || scenario=="native-truncated-close" {
                    marker := strings.Index(answer,"<secretary-recovery>")
                    emit(codexPhaseFixtureChunk("msg_command_final",answer[:marker+len("<secretary-recovery>")],"final_answer"))
                    emit(codexPhaseFixtureChunk("msg_command_final",answer[marker+len("<secretary-recovery>"):],"final_answer"))
                } else if scenario=="native-split-truncated-footer" {
                    emit(codexPhaseFixtureChunk("msg_command_final","готово\n<secretary-","final_answer"))
                    emit(codexPhaseFixtureChunk("msg_command_final","recov","final_answer"))
                } else {
                    emit(codexPhaseFixtureChunk("msg_command_final",answer,"final_answer"))
                }
			case "two-turns":
				prompts++
				if prompts == 1 {
					emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae71", "первый", "final_answer"))
				} else {
					emit(codexPhaseFixtureChunk("msg_069d645203d92047016ad84d901d5881919caa1e3c49b9ae72", "второй", "final_answer"))
				}
			}
			if scenario != "cancelled" && scenario != "native-cancel" && scenario != "incomplete" {
				result = map[string]any{"stopReason": "end_turn"}
			}
		}
		emit(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	os.Exit(0)
}

func codexPhaseFixtureRuntime(t *testing.T, scenario string) (CodexRuntime, string) {
	t.Helper()
	capture := filepath.Join(t.TempDir(), "initialize.json")
	runtime := CodexRuntime{ACPRuntime: ACPRuntime{
		Command:     os.Args[0],
		Arguments:   []string{"-test.run=^TestCodexPhaseFixtureProcess$"},
		Environment: []string{"TEST_CODEX_PHASE_FIXTURE=" + scenario, "TEST_CODEX_PHASE_CAPTURE=" + capture},
	}}
	return runtime, capture
}

func TestCodexPhaseAcceptsPinnedAdapterFinalAnswer(t *testing.T) {
	for _, scenario := range []string{"final-parts", "commentary-then-final", "tool-between", "legacy-namespace"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			runtime, _ := codexPhaseFixtureRuntime(t, scenario)
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result():
				if result.Status != "succeeded" {
					t.Fatalf("scenario %s result=%+v", scenario, result)
				}
				if !strings.Contains(result.Summary, "готово") && !strings.Contains(result.Summary, "готов") {
					t.Fatalf("scenario %s summary=%q", scenario, result.Summary)
				}
				if scenario == "commentary-then-final" && strings.Contains(result.Summary, "Провер") {
					t.Fatalf("commentary was accepted as the final answer: %q", result.Summary)
				}
			case <-ctx.Done():
				t.Fatal("no terminal result")
			}
		})
	}
}

func TestCodexPhaseRejectsUnauthoritativeTerminalAnswer(t *testing.T) {
	for _, scenario := range []string{"no-phase", "contradictory", "final-then-tool"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			runtime, _ := codexPhaseFixtureRuntime(t, scenario)
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-session.Result():
				if result.Status != "failed" {
					t.Fatalf("scenario %s accepted a non-authoritative answer: %+v", scenario, result)
				}
				if !strings.Contains(result.Summary, "Terminal answer unavailable") {
					t.Fatalf("scenario %s summary=%q", scenario, result.Summary)
				}
			case <-ctx.Done():
				t.Fatal("no terminal result")
			}
		})
	}
}

func TestCodexPhaseCanceledAndIncompleteTurns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime, _ := codexPhaseFixtureRuntime(t, "cancelled")
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "canceled" {
			t.Fatalf("cancelled turn result=%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("no terminal result")
	}

	incompleteCtx, incompleteCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer incompleteCancel()
	incomplete, _ := codexPhaseFixtureRuntime(t, "incomplete")
	incompleteSession, err := incomplete.Start(incompleteCtx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer incompleteSession.Close()
	if err := incompleteSession.Prompt(incompleteCtx, "Ответь одним словом: готово"); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case result := <-incompleteSession.Result():
		if result.Status == "succeeded" {
			t.Fatalf("incomplete turn produced a final answer: %+v", result)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestCodexNativeCommandEventToTerminal(t *testing.T) {
 cases:=[]struct{scenario string;status string;code string}{
  {"native-exit-7","failed","tool_execution_failed"},
  {"system-error","failed","native_model_error"},
  {"system-error-untrusted","failed",""},
  {"system-error-other","failed","native_model_error"},
  {"native-exit-0","succeeded",""},
  {"native-json-data","succeeded",""},
  {"native-recovered","succeeded",""},
  {"native-reordered","failed","tool_execution_failed"},
  {"native-cancel","canceled",""},
  {"native-unverified","succeeded",""},
  {"native-invalid-recovery","failed","tool_execution_failed"},
  {"native-no-failure-footer","succeeded",""},
  {"native-incomplete-footer","failed","tool_execution_failed"},
  {"native-truncated-close","failed","tool_execution_failed"},
  {"native-split-truncated-footer","failed","tool_execution_failed"},
 }
 for _,tc:=range cases{
  t.Run(tc.scenario,func(t *testing.T){
   ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
   runtime,_:=codexPhaseFixtureRuntime(t,tc.scenario)
   session,err:=runtime.Start(ctx,StartRequest{WorkerRef:"phase",Workspace:t.TempDir(),DeferInitialPrompt:true,Profile:ManagedProfile{Name:"secretary",Content:"MARKER",Runtime:"codex"}})
   if err!=nil{t.Fatal(err)}
   defer session.Close()
   if err:=session.Prompt(ctx,"execute");err!=nil{t.Fatal(err)}
   select{
   case result:=<-session.Result():
    if result.Status!=tc.status||result.FailureCode!=tc.code{t.Fatalf("result=%+v",result)}
    if tc.scenario=="system-error" && !strings.Contains(result.Summary,"gpt-5.5") {t.Fatalf("native model cause missing: %+v",result)}
    if tc.scenario=="system-error-other" && strings.Contains(result.Summary,"arbitrary unverified") {t.Fatalf("untrusted text elevated to native diagnostic: %+v",result)}
    if tc.scenario=="system-error-untrusted" && result.FailureCode=="native_model_error" {t.Fatalf("text without native systemError elevated: %+v",result)}
    if result.CompletionEvidence==nil || result.CompletionEvidence.Contract!=TerminalCompletionOpenCodeV2 || !result.CompletionEvidence.RPCSucceeded || !result.CompletionEvidence.DrainCompleted || (tc.scenario!="native-cancel" && (result.CompletionEvidence.StopReason!=TerminalStopReasonEndTurn || result.CompletionEvidence.AssistantChunks==0)) || (tc.scenario=="native-cancel" && result.CompletionEvidence.StopReason!=TerminalStopReasonCanceled){t.Fatalf("missing native completion evidence: %+v",result)}
    if (tc.scenario=="native-recovered" || tc.scenario=="native-invalid-recovery" || tc.scenario=="native-no-failure-footer" || tc.scenario=="native-incomplete-footer" || tc.scenario=="native-truncated-close" || tc.scenario=="native-split-truncated-footer") && result.Summary!="готово"{t.Fatalf("recovery leaked metadata: %q",result.Summary)}
   case <-ctx.Done():t.Fatal("no result")
   }
   for {
    select {
    case activity, ok := <-session.Activity():
     if !ok { goto activityDrained }
     if activity.Kind==ActivityText && strings.Contains(activity.Text,"<secretary-recovery>"){t.Fatalf("stream leaked recovery metadata: %q",activity.Text)}
    default:
     goto activityDrained
    }
   }
   activityDrained:
   select{case extra:=<-session.Result():t.Fatalf("duplicate Result: %+v",extra);default:}
  })
 }
}

func TestCodexPhaseTwoTurnsInOneSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, _ := codexPhaseFixtureRuntime(t, "two-turns")
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for turn := 0; turn < 2; turn++ {
		if err := session.Prompt(ctx, "Ответь одним словом: готово"); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-session.Result():
			if result.Status != "succeeded" {
				t.Fatalf("turn %d result=%+v", turn, result)
			}
		case <-ctx.Done():
			t.Fatalf("turn %d produced no terminal result", turn)
		}
	}
}

func TestCodexPhaseNegotiationDeclaresExtensionNamespace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtime, capture := codexPhaseFixtureRuntime(t, "final-parts")
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "phase", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "secretary", Content: "MARKER", Runtime: "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	recorded, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		ClientCapabilities map[string]any `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(recorded, &request); err != nil {
		t.Fatal(err)
	}
	namespace, _ := request.ClientCapabilities["_meta"].(map[string]any)
	extension, _ := namespace[codexPhaseNamespace].(map[string]any)
	air, _ := extension[codexPhaseExtension].(map[string]any)
	if air == nil {
		t.Fatalf("Codex client did not declare the phase extension namespace: %s", recorded)
	}
	if version, _ := air["version"].(float64); version != 1 {
		t.Fatalf("phase extension version=%v: %s", air["version"], recorded)
	}
	if _, ok := request.ClientCapabilities["elicitation"]; !ok {
		t.Fatalf("Codex client lost the elicitation capability: %s", recorded)
	}
}

func TestNonCodexRuntimeDoesNotDeclarePhaseExtension(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	capture := filepath.Join(t.TempDir(), "initialize.json")
	runtime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestCodexPhaseFixtureProcess$"}, Environment: []string{"TEST_CODEX_PHASE_FIXTURE=final-parts", "TEST_CODEX_PHASE_CAPTURE=" + capture}}
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "plain", Workspace: t.TempDir(), DeferInitialPrompt: true, Profile: ManagedProfile{Name: "worker", Content: "MARKER"}})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	recorded, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		ClientCapabilities map[string]any `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(recorded, &request); err != nil {
		t.Fatal(err)
	}
	if _, ok := request.ClientCapabilities["_meta"]; ok {
		t.Fatalf("plain ACP client declared the Codex phase extension: %s", recorded)
	}
}
