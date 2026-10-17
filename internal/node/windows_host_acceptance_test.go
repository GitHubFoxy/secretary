//go:build windows

package node

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"
)

// Runs on a Windows host (not a GOOS=windows cross-compile): the existing ACP
// protocol fixture is spawned as a real Windows process, then resumed by its
// exact native session ID after the first process has been closed.
// A separately authenticated live harness run is still required for release.
func TestWindowsNativeACPProcessStartAndResume(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    runtime := ACPRuntime{
        Command: os.Args[0],
        Arguments: []string{"-test.run=TestFakeACPProcess"},
    }
    workspace := t.TempDir()
    first, err := runtime.Start(ctx, StartRequest{WorkerRef: "windows-worker", Workspace: workspace, Task: "first turn"})
    if err != nil {
        t.Fatalf("start Windows ACP process: %v", err)
    }
    nativeID := first.ID()
    if nativeID == "" {
        t.Fatal("native session identity not returned")
    }
    expectWindowsResult(t, ctx, first, "first turn")
    if err := first.Close(); err != nil {
        t.Fatalf("close first process: %v", err)
    }

    resumed, err := runtime.Resume(ctx, StartRequest{WorkerRef: "windows-worker", Workspace: workspace}, nativeID)
    if err != nil {
        t.Fatalf("resume Windows ACP process: %v", err)
    }
    defer resumed.Close()
    if resumed.ID() != nativeID {
        t.Fatalf("native session changed from %q to %q", nativeID, resumed.ID())
    }
    if err := resumed.(Queueer).Queue(ctx, "follow-up turn"); err != nil {
        t.Fatalf("queue follow-up to resumed native process: %v", err)
    }
    expectWindowsResult(t, ctx, resumed, "follow-up turn")

    missing := ACPRuntime{Command: filepath.Join(t.TempDir(), "missing-native-acp.exe")}
    if session, err := missing.Resume(ctx, StartRequest{WorkerRef: "windows-worker", Workspace: workspace}, nativeID); err == nil {
        _ = session.Close()
        t.Fatal("missing native binary resumed silently")
    }
}

func expectWindowsResult(t *testing.T, ctx context.Context, session Session, label string) {
    t.Helper()
    select {
    case result := <-session.Result():
        if result.Status != "succeeded" || result.Summary != "fake task completed" {
            t.Fatalf("%s result: %#v", label, result)
        }
    case <-ctx.Done():
        t.Fatalf("%s result timed out: %v", label, ctx.Err())
    }
    // The fixture sends one terminal result per prompt, never a second one.
    select {
    case duplicate := <-session.Result():
        t.Fatalf("%s delivered duplicate terminal Result: %#v", label, duplicate)
    case <-time.After(100 * time.Millisecond):
    }
}
