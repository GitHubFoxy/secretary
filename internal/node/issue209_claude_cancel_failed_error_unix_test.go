//go:build !windows

package node

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIssue209ClaudeNativeErrorDuringFailedCancelEmitsFailedAndStopsCLI(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 12*time.Second)
	defer stop()
	dir := t.TempDir()
	trigger := filepath.Join(dir, "emit-error")
	launcher := filepath.Join(dir, "claude")
	script := `#!/bin/sh
read prompt
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"running"}]}}'
while [ ! -f "$ISSUE209_ERROR_TRIGGER" ]; do sleep 0.02; done
printf '%s\n' '{"type":"error","error":"native error during failed Cancel"}'
read keepalive
`
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runtime := ClaudeCodeRuntime{Command: launcher, Environment: []string{"ISSUE209_ERROR_TRIGGER=" + trigger}}
	raw, err := runtime.Start(ctx, StartRequest{Workspace: dir, Task: "run"})
	if err != nil {
		t.Fatal(err)
	}
	session := raw.(*claudeSession)
	defer session.Close()
	select {
	case <-session.Activity():
	case <-ctx.Done():
		t.Fatal("Claude did not begin active execution")
	}
	cleanupEntered := make(chan struct{})
	releaseCleanup := make(chan struct{})
	defer func() {
		select {
		case <-releaseCleanup:
		default:
			close(releaseCleanup)
		}
	}()
	session.cleanupAttempt = func(context.Context, *exec.Cmd, claudeChild) error {
		close(cleanupEntered)
		<-releaseCleanup
		return errors.New("native cleanup unavailable")
	}
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- session.Cancel(ctx) }()
	select {
	case <-cleanupEntered:
	case <-ctx.Done():
		t.Fatal("Cancel did not enter native cleanup")
	}
	if err := os.WriteFile(trigger, []byte("emit"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "native error deferred during Cancel", func() bool {
		session.stateMu.Lock()
		defer session.stateMu.Unlock()
		return session.canceling && strings.Contains(session.nativeErrorDuringCancel, "native error during failed Cancel")
	})
	close(releaseCleanup)
	select {
	case err := <-cancelDone:
		if err == nil || !strings.Contains(err.Error(), "native cleanup unavailable") {
			t.Fatalf("Cancel unexpectedly succeeded: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Cancel did not return after cleanup failure")
	}
	select {
	case result := <-session.Result():
		if result.Status != "failed" || result.FailureCode != "cancel_failed" || result.Summary != "native error during failed Cancel" {
			t.Fatalf("native error terminal lost after failed Cancel: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("lost native error left Attempt without terminal Result")
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal("native CLI remained running after failed Cancel")
	}
	select {
	case duplicate, ok := <-session.Result():
		if ok {
			t.Fatalf("duplicate terminal Result after failed Cancel: %+v", duplicate)
		}
	default:
	}
}
