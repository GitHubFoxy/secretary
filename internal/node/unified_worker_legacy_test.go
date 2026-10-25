package node

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCoLocatedLegacyWorkerResumeRequiresDurableNodeMapping(t *testing.T) {
	ctx := context.Background()
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node-state.json"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	runtime := &daemonCountingRuntime{}
	execution := NewExecutionNode("local", runtime, store)
	original := daemonDispatchFixture("local", "legacy-original")
	metadata := original.Dispatch.Metadata
	metadata.CommandID = "legacy-resume"
	metadata.AttemptID = "new-attempt"
	metadata.TurnID = "new-turn"
	envelope := original.Dispatch.Envelope
	envelope.PreviousAttemptID = original.Dispatch.Metadata.AttemptID
	envelope.AttemptID = metadata.AttemptID
	envelope.TurnID = metadata.TurnID
	command := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: metadata, Envelope: envelope}}
	outcome, err := execution.HandleCommand(ctx, command)
	if err != nil { t.Fatal(err) }
	if outcome.State != CommandFailed || outcome.ErrorCode != "runtime_session_unavailable" {
		t.Fatalf("unverified historical local Session was resumed: %#v", outcome)
	}
	if runtime.starts.Load() != 0 {
		t.Fatal("missing native mapping silently started a replacement Session")
	}
	if _, ok := store.SessionMapping(metadata.AttemptID); ok {
		t.Fatal("missing native mapping was synthesized")
	}
}
