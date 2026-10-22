//go:build !windows

package node

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestMAC02ConcurrentCancelTerminalAndCleanupFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		cleanupErr error
		wantStatus string
	}{
		{name: "cleanup succeeds", wantStatus: "canceled"},
		{name: "cleanup fails", cleanupErr: errors.New("child survived"), wantStatus: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			s := &claudeSession{
				id: "same-native-session",
				active: true,
				inputID: "attempt-one",
				claudeRoot: claudeChild{pid: 123},
				result: make(chan Result, 4),
				cleanupAttempt: func(context.Context, *exec.Cmd, claudeChild) error {
					close(entered)
					<-release
					return tc.cleanupErr
				},
			}
			ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			first := make(chan error, 1)
			go func() { first <- s.Cancel(ctx) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("cleanup never entered")
			}
			second := make(chan error, 1)
			go func() { second <- s.Cancel(ctx) }()
			select {
			case err := <-second:
				if err != nil {
					t.Fatalf("concurrent Cancel: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("concurrent Cancel blocked")
			}
			s.emitResult(Result{Status: "canceled", Summary: "Claude Code attempt canceled"})
			if err := s.Prompt(ctx, "premature followup"); err == nil {
				t.Fatal("Follow-up accepted during pending Cancel")
			}
			select {
			case result := <-s.Result():
				t.Fatalf("terminal published before cleanup completed: %+v", result)
			default:
			}
			close(release)
			select {
			case err := <-first:
				if (err == nil) != (tc.cleanupErr == nil) {
					t.Fatalf("cleanup error=%v, want %v", err, tc.cleanupErr)
				}
			case <-ctx.Done():
				t.Fatal("Cancel did not finish")
			}
			select {
			case result := <-s.Result():
				if result.Status != tc.wantStatus {
					t.Fatalf("terminal=%+v, want status %q", result, tc.wantStatus)
				}
				if tc.cleanupErr != nil && !strings.Contains(result.Summary, tc.cleanupErr.Error()) {
					t.Fatalf("cleanup failure not visible: %+v", result)
				}
			default:
				t.Fatal("terminal Result missing after Cancel completion")
			}
			select {
			case extra := <-s.Result():
				t.Fatalf("duplicate terminal: %+v", extra)
			default:
			}
			if s.active || s.canceling || s.pendingTerminal != nil {
				t.Fatal("Attempt retained pending terminal after Cancel completion")
			}
		})
	}
}

func TestMAC02RootCaptureFailureStopsAndReapsCLI(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "claude")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec sleep 180\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var captured *exec.Cmd
	runtime := ClaudeCodeRuntime{
		Command: launcher,
		captureRoot: func(cmd *exec.Cmd) (claudeChild, error) {
			mu.Lock()
			captured = cmd
			mu.Unlock()
			return claudeChild{}, errors.New("root inspection unavailable")
		},
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	session, err := runtime.Start(ctx, StartRequest{Workspace: dir, DeferInitialPrompt: true})
	if session != nil || !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("session=%v error=%v; expected launch rejection", session, err)
	}
	mu.Lock()
	cmd := captured
	mu.Unlock()
	if cmd == nil || cmd.Process == nil || cmd.ProcessState == nil {
		t.Fatalf("started CLI not reaped after root identity failure: %+v", cmd)
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("capture failure did not kill and reap CLI: state=%v", cmd.ProcessState)
	}
}
