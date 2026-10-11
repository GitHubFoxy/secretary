//go:build darwin || linux

package telegram

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCodexFormatterTimeoutTerminatesWrapperAndChild(t *testing.T) {
	fixture := t.TempDir()
	heartbeat := filepath.Join(fixture, "heartbeat")
	childPIDFile := filepath.Join(fixture, "child.pid")
	wrapper := filepath.Join(fixture, "codex-wrapper")
	// Both wrapper and child ignore TERM, reproducing a JS/native wrapper that
	// outlives ordinary CommandContext cancellation. The child writes a heartbeat.
	script := `#!/bin/sh
trap '' TERM
/bin/sh -c 'trap "" TERM; while :; do printf x >> "$FORMATTER_HEARTBEAT"; sleep 0.02; done' &
echo "$!" > "$FORMATTER_CHILD_PID_FILE"
wait
`
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORMATTER_HEARTBEAT", heartbeat)
	t.Setenv("FORMATTER_CHILD_PID_FILE", childPIDFile)
	f := &CodexFormatter{Enabled: true, Model: "gpt-6-luna", Prompt: "format only", Command: wrapper, Timeout: 750 * time.Millisecond, TempRoot: t.TempDir()}
	var process *os.Process
	var diagnostics bytes.Buffer
	f.CommandRun = func(_ context.Context, cmd *exec.Cmd) error {
		cmd.Stderr = &diagnostics
		err := cmd.Start()
		if err != nil {
			return err
		}
		process = cmd.Process
		return cmd.Wait()
	}
	defer func() {
		if process != nil {
			_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
		}
	}()
	start := time.Now()
	_, err := f.Format(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 2*time.Second {
		t.Fatalf("elapsed=%v error=%v", time.Since(start), err)
	}
	pidData, readerr := os.ReadFile(childPIDFile)
	if readerr != nil {
		t.Fatalf("fixture child never started: %v; %s", readerr, diagnostics.String())
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	before, err := os.ReadFile(heartbeat)
	if err != nil || len(before) == 0 {
		t.Fatal("fixture child did not run")
	}
	time.Sleep(100 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil || len(after) != len(before) {
		t.Fatal("child remained running after formatter timeout")
	}
	// Linux may briefly retain a killed orphan as a zombie; it cannot execute.
	if err := syscall.Kill(pid, 0); err == nil {
		status, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if err != nil || !strings.Contains(string(status), ") Z ") {
			t.Fatal("child process survived timeout")
		}
	}
	entries, _ := os.ReadDir(f.TempRoot)
	if len(entries) != 0 {
		t.Fatal("timeout leaked formatter temporary files")
	}
}
