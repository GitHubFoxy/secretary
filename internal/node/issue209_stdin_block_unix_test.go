//go:build !windows

package node

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type issue209BlockedClaudeStdin struct {
	started chan struct{}
	closed chan struct{}
	closeOnce sync.Once
	successfulWrites atomic.Int32
}

func (w *issue209BlockedClaudeStdin) Write([]byte) (int, error) {
	select {
	case w.started <- struct{}{}:
	default:
	}
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *issue209BlockedClaudeStdin) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

func TestIssue209CancelBlockedFirstClaudeStdinWriteStopsCLIAndPublishesTerminal(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	cmd := exec.Command("sh", "-c", "exec sleep 180")
	configureClaudeProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	writer := &issue209BlockedClaudeStdin{started: make(chan struct{}, 1), closed: make(chan struct{})}
	session := &claudeSession{
		id: "4b22ac0e-109c-4aac-8a17-3e78cba13809",
		cmd: cmd,
		stdin: writer,
		stdout: stdout,
		stderr: &bytes.Buffer{},
		activity: make(chan Activity, 4),
		result: make(chan Result, 4),
		ready: make(chan error, 1),
		writeGate: make(chan struct{}, 1),
		done: make(chan struct{}),
		initialPromptPending: true,
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	session.claudeRoot = claudeChild{pid: cmd.Process.Pid}
	go session.run()
	defer session.Close()
	promptDone := make(chan error, 1)
	go func() { promptDone <- session.Prompt(ctx, "run a long task") }()
	select {
	case <-writer.started:
	case <-ctx.Done():
		t.Fatal("initial Claude stdin write never became blocked")
	}
	if len(session.writeGate) != 1 {
		t.Fatal("blocked Claude stdin did not hold writeGate")
	}
	cancelEntered := make(chan struct{})
	session.beforeInitialCancelClaim = func() { close(cancelEntered) }
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- session.Cancel(ctx) }()
	select {
	case <-cancelEntered:
	case <-ctx.Done():
		t.Fatal("Cancel never reached held stdin writeGate")
	}
	select {
	case err := <-cancelDone:
		if err != nil {
			t.Fatalf("Cancel failed to stop blocked Claude CLI: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Cancel waited for blocked stdin writeGate instead of stopping CLI")
	}
	select {
	case err := <-promptDone:
		if err == nil {
			t.Fatal("native initial Prompt completed after Cancel")
		}
	case <-ctx.Done():
		t.Fatal("blocked native Prompt never returned after CLI shutdown")
	}
	select {
	case result := <-session.Result():
		if result.Status != "canceled" {
			t.Fatalf("blocked CLI canceled with incorrect terminal status: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal("blocked CLI did not publish terminal Result")
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal("Claude CLI was not stopped and reaped")
	}
	if cmd.ProcessState == nil {
		t.Fatal("Claude CLI was not reaped")
	}
	if writer.successfulWrites.Load() != 0 {
		t.Fatal("native Prompt was injected after accepted Cancel")
	}
	select {
	case extra, ok := <-session.Result():
		if ok {
			t.Fatalf("duplicate terminal Result: %+v", extra)
		}
	default:
		t.Fatal("terminal stream did not close after native process cleanup")
	}
}
