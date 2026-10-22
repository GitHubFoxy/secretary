//go:build !windows

package node

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"os/exec"
	"testing"
	"time"
)

func TestMAC02ClaudeCancelStopsOriginalDescendantPIDsAndPreservesFollowup(t *testing.T) {
	dir := t.TempDir()
	shellPIDFile := filepath.Join(dir, "shell.pid")
	sleepPIDFile := filepath.Join(dir, "sleep.pid")
	launcher := filepath.Join(dir, "claude")
	script := `#!/bin/sh
read first
/bin/sh -c 'sleep 180 & echo $! > "$MAC02_SLEEP_PID"; wait' &
echo $! > "$MAC02_SHELL_PID"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"running"}]}}'
read interrupted
printf '%s\n' '{"type":"result","subtype":"success","result":""}'
read followup
printf '%s\n' '{"type":"result","subtype":"success","result":"resumed"}'
read end
`
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	session, err := (ClaudeCodeRuntime{Command: launcher, Environment: []string{"MAC02_SHELL_PID=" + shellPIDFile, "MAC02_SLEEP_PID=" + sleepPIDFile}}).Start(ctx, StartRequest{Workspace: dir, Task: "run sleep"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var shellPID, sleepPID int
	t.Cleanup(func() {
		for _, pid := range []int{sleepPID, shellPID} {
			if pid > 1 {
				if processes, err := claudeProcessSnapshot(); err == nil {
					if process, ok := processes[pid]; ok && !strings.HasPrefix(process.state, "Z") {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			}
		}
	})
	readPID := func(path string) int {
		t.Helper()
		for {
			data, err := os.ReadFile(path)
			if err == nil {
				pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
				if parseErr == nil && pid > 1 {
					return pid
				}
			}
			select {
			case <-ctx.Done():
				t.Fatalf("PID missing at %s: %v", path, ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	shellPID = readPID(shellPIDFile)
	sleepPID = readPID(sleepPIDFile)
	processes, err := claudeProcessSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if processes[sleepPID].parent != shellPID || processes[shellPID].parent != session.(*claudeSession).cmd.Process.Pid {
		t.Fatalf("unexpected initial process ancestry: shell=%+v sleep=%+v", processes[shellPID], processes[sleepPID])
	}
	nativeID := session.ID()
	if err := session.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "canceled" || result.Summary != "Claude Code attempt canceled" {
			t.Fatalf("terminal result=%+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		processes, err = claudeProcessSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		alive := false
		for _, pid := range []int{shellPID, sleepPID} {
			if p, ok := processes[pid]; ok && !strings.HasPrefix(p.state, "Z") {
				alive = true
			}
		}
		if !alive {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("original shell/sleep PIDs survived cancellation: %d %d", shellPID, sleepPID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := session.Prompt(ctx, "continue"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" || result.Summary != "resumed" {
			t.Fatalf("followup result=%+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if session.ID() != nativeID {
		t.Fatal("native session identity changed across Cancel")
	}
	select {
	case duplicate := <-session.Result():
		t.Fatalf("unexpected duplicate Result: %+v", duplicate)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMAC02ClaudeDescendantCleanupIsScopedToNativeProcess(t *testing.T) {
	cmd := &exec.Cmd{}
	if err := cleanupClaudeAttemptProcesses(cmd); err != nil {
		t.Fatal(err)
	}
}
