package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestClaimedResumeWithUnknownAcceptanceIsNotRearmedOnRecovery(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil { t.Fatal(err) }
	store.SetAutoResumeOnLimit(true)
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil { t.Fatal(err) }
	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "claimed-resume", ProjectID: "project", NodeID: "node",
		HarnessInstanceID: "node/fx", Intent: "continue", PolicySnapshot: "{}",
	}, TurnSpec{Input: "continue"})
	if err != nil { t.Fatal(err) }
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil { t.Fatal(err) }
	reset := time.Now().UTC().Add(time.Minute)
	if _, _, _, err := store.RecordNodeAttemptOutcome(ctx, AttemptOutcomeEnvelope{
		EventID: "limit-event", Node: "node", HarnessInstanceID: "node/fx",
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		Status: OutcomeFailed, Classification: OutcomeRetryable,
		ErrorCode: "usage_limit_exceeded", FailureCategory: FailureCategoryUsageLimit,
		ResetsAt: &reset, QuotaScope: "fixture",
	}); err != nil { t.Fatal(err) }
	schedule, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil || !found { t.Fatalf("schedule: found=%v err=%v", found, err) }
	claimed, err := store.ClaimAttemptSchedule(ctx, schedule.ID, time.Now().UTC())
	if err != nil || !claimed { t.Fatalf("claim: %v %v", claimed, err) }
	command, _, err := store.ClaimWorkerCommand(ctx, "resume", "attempt", worker.ID, schedule.AttemptID)
	if err != nil { t.Fatal(err) }
	if command.LeaseUntil.IsZero() { t.Fatal("test did not lease the Resume command") }
	if err := store.Close(); err != nil { t.Fatal(err) }
	reopened, err := Open(ctx, path)
	if err != nil { t.Fatal(err) }
	defer reopened.Close()
	reopened.SetAutoResumeOnLimit(true)
	resolver := Phase4AttemptRecoveryFunc(func(context.Context, Phase4Attempt) (Phase4RecoveryDecision, error) {
		return Phase4RecoveryUnknown, nil
	})
	if err := reopened.RecoverPhase4Attempts(ctx, resolver); err != nil { t.Fatal(err) }
	recovered, found, err := reopened.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil || !found || recovered.State != ScheduleManualHold {
		t.Fatalf("leased Resume rearmed: found=%v schedule=%#v err=%v", found, recovered, err)
	}
	persisted, found, err := reopened.FindWorkerCommand(ctx, "resume", "attempt", worker.ID, schedule.AttemptID)
	if err != nil || !found || persisted.State != WorkerCommandUncertain {
		t.Fatalf("unproven Resume lost: found=%v command=%#v err=%v", found, persisted, err)
	}
	if err := reopened.RecoverPhase4Attempts(ctx, resolver); err != nil { t.Fatal(err) }
	if due, err := reopened.DueAttemptSchedules(ctx, reset.Add(2*time.Hour)); err != nil || len(due) != 0 {
		t.Fatalf("uncertain Resume became due: schedules=%#v err=%v", due, err)
	}
}
