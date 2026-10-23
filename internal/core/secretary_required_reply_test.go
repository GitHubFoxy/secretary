package core

import (
	"context"
 "database/sql"
 "strings"
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
		{"native model error", false, SecretaryTurnFailed, SecretaryCompletionEvidence{Branch: "invalid", NativeFailure: "The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account."}, SecretaryTurnFailed, "The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account."},
		{"untrusted native detail", false, SecretaryTurnFailed, SecretaryCompletionEvidence{Branch: "invalid", NativeFailure: "private value not a trusted model error"}, SecretaryTurnFailed, "native_terminal_invalid"},
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
			if tc.name == "missing" {
				if _, _, err := store.RecordSecretaryAcknowledgement(ctx, person.ID, capability, turn.ID, turn.InputID, "Сейчас проверю"); err != nil {
					t.Fatal(err)
				}
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
				if entry.Kind == EntrySecretary && entry.Body != "Сейчас проверю" {
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

func TestCodexMissingReplyDiagnosticReflectsAcceptedSpawnAndSurvivesRestart(t *testing.T) {
 for _, tc:=range []struct{name,input string;accepted bool;nativeError bool}{
  {name:"ordinary-answer-no-action",input:"Сколько будет два плюс два?"},
  {name:"worker-request-no-action",input:"Поручи это отдельному исполнителю"},
  {name:"worker-request-accepted-action",input:"Поручи это отдельному исполнителю",accepted:true},
  {name:"native-model-error",input:"Ответь с default моделью",nativeError:true},
 } {
  t.Run(tc.name,func(t *testing.T){
   ctx:=context.Background()
   path:=filepath.Join(t.TempDir(),"state.db")
   store,err:=Open(ctx,path)
   if err!=nil{t.Fatal(err)}
   person,conversation,err:=store.CreatePersonWithConversation(ctx)
   if err!=nil{t.Fatal(err)}
   identity,err:=store.EnsureSecretaryIdentity(ctx,person.ID,conversation.ID)
   if err!=nil{t.Fatal(err)}
   capability,err:=store.RotateSecretaryCapability(ctx,person.ID)
   if err!=nil{t.Fatal(err)}
   if _,err:=store.ReplaceSecretaryRuntime(ctx,identity.ID,"codex","default","high");err!=nil{t.Fatal(err)}
   if _,err:=store.SaveUserDocument(ctx,filepath.Join(t.TempDir(),"user.md"),"synthetic");err!=nil{t.Fatal(err)}
   if err:=store.SetSecretaryPolicySnapshot(ctx,SecretaryPolicySnapshot{Version:"v1",Harness:"codex",Model:"default",Reasoning:"high",ProfileVersion:"v1",ProfileName:"secretary",ProfileHash:"h",ProfileContent:"synthetic",ReplyContractVersion:SecretaryReplyContractAddressedV1});err!=nil{t.Fatal(err)}
   turn,err:=store.EnqueueSecretaryTurn(ctx,identity.ID,tc.input)
   if err!=nil{t.Fatal(err)}
   if _,err=store.StartSecretaryTurn(ctx,turn.ID);err!=nil{t.Fatal(err)}
   if tc.accepted {
    _,workerTurn,_,err:=store.CreateWorker(ctx,conversation.ID,WorkerSpec{WorkerRef:"accepted",Intent:"synthetic",ProjectID:"p",NodeID:"n",HarnessInstanceID:"n/fx",PolicySnapshot:"synthetic"},TurnSpec{Input:"synthetic"})
    if err!=nil{t.Fatal(err)}
    _,err=withTx(store,ctx,func(tx *sql.Tx)(bool,error){
     origin:=SecretaryOriginIdentity{PersonID:person.ID,Capability:capability,SecretaryTurnID:turn.ID,InputID:turn.InputID}
     return true,bindSecretaryWorkerCreationOriginTx(ctx,tx,origin,conversation.ID,workerTurn.ID,store.now(),true)
    })
    if err!=nil{t.Fatal(err)}
   }
   evidence:=SecretaryCompletionEvidence{Branch:"assistant_final",TerminalClass:"end_turn",TerminalValid:true,RPCSucceeded:true,DrainCompleted:true,ResponsePresent:true}
   target:=SecretaryTurnSucceeded
   wantError:="addressed_reply_missing"
   if tc.nativeError {
    target=SecretaryTurnFailed
    wantError="The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account."
    evidence=SecretaryCompletionEvidence{Branch:"invalid",NativeFailure:wantError}
   }
   completed,err:=store.FinishSecretaryTurnWithRequiredReply(ctx,turn.ID,turn.InputID,target,evidence)
   if err!=nil||completed.State!=SecretaryTurnFailed||completed.Error!=wantError{t.Fatalf("missing MCP reply state=%#v err=%v",completed,err)}
   entries,err:=store.EntriesAfter(ctx,conversation.ID,0)
   if err!=nil{t.Fatal(err)}
   count:=0
   for _,entry:=range entries {
    if entry.Kind!=EntrySecretary{continue}
    count++
    if tc.nativeError && entry.Body!=wantError {t.Fatalf("trusted model cause was lost: %q",entry.Body)}
    if tc.accepted && !strings.Contains(entry.Body,"Worker создан и принят сервером"){t.Fatalf("accepted Worker described as missing: %q",entry.Body)}
    if !tc.nativeError && !tc.accepted && (!strings.Contains(entry.Body,"не получил подтверждённый адресованный ответ") || strings.Contains(entry.Body,"Worker")){t.Fatalf("unbound reply did not receive operation-neutral failure: %q",entry.Body)}
   }
   if count!=1{t.Fatalf("expected single owner diagnostic, got %#v",entries)}
   if err:=store.Close();err!=nil{t.Fatal(err)}
   store,err=Open(ctx,path)
   if err!=nil{t.Fatal(err)}
   defer store.Close()
   if _,err=store.FinishSecretaryTurnWithRequiredReply(ctx,turn.ID,turn.InputID,target,evidence);!errors.Is(err,ErrInvalidTransition){t.Fatalf("restart duplicated completion: %v",err)}
   recovered,err:=store.EntriesAfter(ctx,conversation.ID,0)
   if err!=nil||len(recovered)!=len(entries){t.Fatalf("restart changed Conversation ordering/entries before=%#v after=%#v err=%v",entries,recovered,err)}
   for i:=range entries {if entries[i].ID!=recovered[i].ID || entries[i].Seq!=recovered[i].Seq{t.Fatalf("entry ordering changed at %d: %#v != %#v",i,entries[i],recovered[i])}}
  })
 }
}

func TestAcknowledgedFailedLegacyTurnPersistsOneOwnerExplanation(t *testing.T) {
 ctx := context.Background()
 store, err := Open(ctx,filepath.Join(t.TempDir(),"failed-secretary.db"))
 if err != nil { t.Fatal(err) }
 defer store.Close()
 person,conversation,err := store.CreatePersonWithConversation(ctx)
 if err != nil { t.Fatal(err) }
 identity,err := store.EnsureSecretaryIdentity(ctx,person.ID,conversation.ID)
 if err != nil { t.Fatal(err) }
 capability,err := store.RotateSecretaryCapability(ctx,person.ID)
 if err != nil { t.Fatal(err) }
 turn,err := store.EnqueueSecretaryTurn(ctx,identity.ID,"spawn worker")
 if err != nil { t.Fatal(err) }
 turn,err = store.StartSecretaryTurn(ctx,turn.ID)
 if err != nil { t.Fatal(err) }
 if _,_,err=store.RecordSecretaryAcknowledgement(ctx,person.ID,capability,turn.ID,turn.InputID,"Сейчас запущу Worker");err!=nil {t.Fatal(err)}
 explanation:="Ошибка: выбранный harness не поддерживает reasoning."
 done,_,err:=store.FinishSecretaryTurnWithResponse(ctx,turn.ID,SecretaryTurnFailed,explanation,"")
 if err!=nil || done.State!=SecretaryTurnFailed || done.Error!=explanation { t.Fatalf("completion=%+v err=%v",done,err) }
 entries,err:=store.EntriesAfter(ctx,conversation.ID,0)
 if err!=nil {t.Fatal(err)}
 ack,terminal:=0,0
 for _,e:=range entries {
  if e.TurnID!=turn.ID {continue}
  if e.Body=="Сейчас запущу Worker" {ack++}
  if e.Body==explanation {terminal++}
 }
 if ack!=1||terminal!=1 {t.Fatalf("ack=%d terminal=%d",ack,terminal)}
 if _,_,err:=store.FinishSecretaryTurnWithResponse(ctx,turn.ID,SecretaryTurnFailed,explanation,"");err==nil {t.Fatal("terminal completion duplicated")}
 entries,err=store.EntriesAfter(ctx,conversation.ID,0)
 if err!=nil {t.Fatal(err)}
 terminal=0
 for _,e:=range entries {if e.TurnID==turn.ID && e.Body==explanation {terminal++}}
 if terminal!=1 {t.Fatalf("terminal replay duplicated: %d",terminal)}
}
