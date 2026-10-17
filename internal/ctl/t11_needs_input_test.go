package ctl

import (
	"context"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestT11NeedsInputImageNeverAcknowledgesTextOnlyRespond(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	worker := spawnLifecycleWorker(t, ctx, service, project)
	attempt := worker.Attempts[0]
	if _,err:=store.SetPhase4AttemptActive(ctx,attempt.ID);err!=nil {t.Fatal(err)}
	if _,err:=store.SetPhase4AttemptNeedsInput(ctx,attempt.ID);err!=nil {t.Fatal(err)}
	conversation,err:=store.ConversationForPerson(ctx,service.PersonID)
	if err!=nil {t.Fatal(err)}
	picture:=t10PNGFixture(t)
	artifact,_,err:=store.SaveArtifact(ctx,service.PersonID,conversation.ID,"t11-needs-input-original","question.png","image/png",picture)
	if err!=nil {t.Fatal(err)}
	runtime:=service.Runtime.(*lifecycleRuntime)
	_,err=service.MessageWorker(ctx,MessageWorkerRequest{
		WorkerRef:worker.Worker.WorkerRef,RequestID:"question-1",
		Text:"Look at artifact://"+artifact.ID,
		IdempotencyKey:"t11-needs-input",
	})
	if err==nil || !strings.Contains(err.Error(),"needs_input") {t.Fatalf("image response was accepted: %v",err)}
	if runtime.count("respond")!=0 {t.Fatal("native Respond received a text-only image reference")}
	next,err:=store.WorkerDetailsForConversation(ctx,conversation.ID,worker.Worker.WorkerRef)
	if err!=nil || next.Worker.Status!=core.WorkerNeedsInput {t.Fatalf("needs_input was acknowledged: %+v %v",next.Worker,err)}
	if _,err=service.MessageWorker(ctx,MessageWorkerRequest{
		WorkerRef:worker.Worker.WorkerRef,RequestID:"question-1",Text:"Answer in text",
		IdempotencyKey:"t11-needs-input",
	});err!=nil {t.Fatalf("valid text retry incorrectly blocked: %v",err)}
	if runtime.count("respond")!=1 {t.Fatal("valid text answer was not delivered exactly once")}
}

var _ = context.Background
