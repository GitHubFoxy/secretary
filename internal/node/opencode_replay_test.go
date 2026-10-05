package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCodeResumeDrainsHistoryBeforeFreshResult(t *testing.T) {
	t.Setenv("TEST_NATIVE_CONFIG_SCENARIO", "long-replay")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := OpenCodeRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestOpenCodeConfigCatalogProcess$"}, DataHome: filepath.Join(t.TempDir(), "native-data")}
	profile := ManagedProfile{Name: "worker", Content: "managed system", AllowTools: []string{"read"}, Model: "fixture/fixture-model", Reasoning: "low"}
	session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "private-replay", Workspace: t.TempDir(), Profile: profile, DeferInitialPrompt: true}, "private-original-session")
	if err != nil {
		t.Fatal("large native replay blocked Resume")
	}
	defer session.Close()
	select {
	case <-session.Activity():
		t.Fatal("historical replay emitted new Attempt activity")
	default:
	}
	result := runNativeFixtureTurn(t, ctx, session, "new private prompt")
	if result.Status != "succeeded" || result.Summary != "fresh-onefresh-two" {
		t.Fatal("fresh Result included historical replay or lost a text delta")
	}
}
