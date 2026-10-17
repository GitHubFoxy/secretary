//go:build windows

package node

import (
    "context"
    "os"
    "path/filepath"
    "strings"
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


// Exercise a real npm-style .cmd shim, not a bare Go .exe. The shim must
// preserve native stdin/stdout pipes for ACP, and must be used for Resume.
func TestWindowsNPMCommandShimStartsAndResumesACP(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    shimDir := filepath.Join(t.TempDir(), "npm global shims")
    if err := os.MkdirAll(shimDir, 0o700); err != nil {
        t.Fatal(err)
    }
    shim := filepath.Join(shimDir, "codex-acp.cmd")
    script := "@echo off\r\n\"%SECRETARY_WINDOWS_TEST_BINARY%\" -test.run=TestFakeACPProcess\r\n"
    if err := os.WriteFile(shim, []byte(script), 0o600); err != nil {
        t.Fatal(err)
    }
    t.Setenv("SECRETARY_WINDOWS_TEST_BINARY", os.Args[0])
    runtime := ACPRuntime{Command: shim}
    workspace := t.TempDir()
    first, err := runtime.Start(ctx, StartRequest{WorkerRef: "npm-shim", Workspace: workspace, Task: "first"})
    if err != nil {
        t.Fatalf("npm .cmd ACP launch failed: %v", err)
    }
    nativeID := first.ID()
    expectWindowsResult(t, ctx, first, "npm first")
    if err := first.Close(); err != nil {
        t.Fatal(err)
    }
    second, err := runtime.Resume(ctx, StartRequest{WorkerRef: "npm-shim", Workspace: workspace}, nativeID)
    if err != nil {
        t.Fatalf("npm .cmd ACP resume failed: %v", err)
    }
    defer second.Close()
    if second.ID() != nativeID {
        t.Fatalf("npm shim changed native ID: %s -> %s", nativeID, second.ID())
    }
    if err := second.(Queueer).Queue(ctx, "follow-up"); err != nil {
        t.Fatal(err)
    }
    expectWindowsResult(t, ctx, second, "npm follow-up")

    probe := filepath.Join(shimDir, "codex.cmd")
    if err := os.WriteFile(probe, []byte("@echo off\r\necho native-probe-%~1\r\n"), 0o600); err != nil {
        t.Fatal(err)
    }
    result, err := (ExecCommandRunner{}).Run(ctx, probe, "--version")
    if err != nil || strings.TrimSpace(result.Stdout) != "native-probe---version" {
        t.Fatalf("Windows npm shim readiness failed: result=%#v err=%v", result, err)
    }
    ps1 := filepath.Join(shimDir, "codex.ps1")
    if err := os.WriteFile(ps1, []byte("Write-Output bypass"), 0o600); err != nil {
        t.Fatal(err)
    }
    if _, err := (ExecCommandRunner{}).Run(ctx, ps1, "--version"); err == nil || !strings.Contains(err.Error(), ".ps1") {
        t.Fatalf("PowerShell shim was accepted as a native executable: %v", err)
    }
}
