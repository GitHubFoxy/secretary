//go:build !windows

package node

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
)

func TestMAC1204CodexCancelKillsDetachedToolHost(t *testing.T) {
	testMAC1204Cancel(t, false)
}

func TestMAC1204CodexCancelCleansDescendantsAfterNativeWriteFailure(t *testing.T) {
	testMAC1204Cancel(t, true)
}

func testMAC1204Cancel(t *testing.T, failedWrite bool) {
	if os.Getenv("MAC1204_HELPER") == "1" {
		_ = os.WriteFile(os.Getenv("MAC1204_ROOT_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0600)
		child := exec.Command("/bin/sleep", "45")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(os.Getenv("MAC1204_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
		if os.Getenv("MAC1204_CLOSE_INPUT") == "1" {
			_ = os.Stdin.Close()
			_ = os.WriteFile(os.Getenv("MAC1204_INPUT_CLOSED_MARKER"), []byte("closed"), 0600)
			time.Sleep(600 * time.Millisecond)
			os.Exit(0)
		}
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "session/cancel") {
				late := exec.Command("/bin/sleep", "45")
				late.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				if err := late.Start(); err != nil { os.Exit(3) }
				_ = os.WriteFile(os.Getenv("MAC1204_LATE_PID_FILE"), []byte(strconv.Itoa(late.Process.Pid)), 0600)
				time.Sleep(180 * time.Millisecond)
				os.Exit(0)
			}
		}
		os.Exit(0)
	}
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	pidFile := filepath.Join(t.TempDir(), "sleep.pid")
	rootPIDFile := filepath.Join(filepath.Dir(pidFile), "root.pid")
	latePIDFile := filepath.Join(filepath.Dir(pidFile), "late.pid")
	closedMarker := filepath.Join(filepath.Dir(pidFile), "stdin-closed")
	t.Setenv("MAC1204_HELPER", "1")
	if failedWrite { t.Setenv("MAC1204_CLOSE_INPUT", "1") }
	t.Setenv("MAC1204_PID_FILE", pidFile)
	t.Setenv("MAC1204_ROOT_PID_FILE", rootPIDFile)
	t.Setenv("MAC1204_LATE_PID_FILE", latePIDFile)
	t.Setenv("MAC1204_INPUT_CLOSED_MARKER", closedMarker)
	client, err := acp.Start(ctx, os.Args[0], "-test.run=^TestMAC1204CodexCancelKillsDetachedToolHost$")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	other := exec.Command("/bin/sleep", "45")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	var pid int
	for {
		bytes, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err = strconv.Atoi(strings.TrimSpace(string(bytes)))
			if err == nil && pid > 1 {
				break
			}
		}
		if ctx.Err() != nil {
			t.Fatal("detached Codex tool host did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer func() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()
	processState := func(target int) (string, error) {
		output, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(target)).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(output)), nil
	}
	rootBytes, err := os.ReadFile(rootPIDFile)
	if err != nil { t.Fatal(err) }
	rootPID, err := strconv.Atoi(strings.TrimSpace(string(rootBytes)))
	if err != nil { t.Fatal(err) }
	if _, err := processState(pid); err != nil {
		t.Fatalf("detached tool exited before cancellation: %v", err)
	}
	if failedWrite {
		for {
			if _, err := os.Stat(closedMarker); err == nil { break }
			if ctx.Err() != nil { t.Fatal("native input did not close") }
			time.Sleep(5 * time.Millisecond)
		}
	}
	session := newACPSession("native-session", client, true)
	session.codexMessagePhases = true
	started := time.Now()
	err = session.Cancel(ctx)
	if failedWrite {
		if err == nil { t.Fatal("native write failure was not propagated") }
	} else if err != nil {
		t.Fatal(err)
	}
	for {
		state, err := processState(pid)
		if err != nil || strings.HasPrefix(state, "Z") {
			break
		}
		if time.Since(started) > 2*time.Second {
			t.Fatalf("Codex detached sleep survived Cancel for %v", time.Since(started))
		}
		time.Sleep(20 * time.Millisecond)
	}
	for {
		state, err := processState(rootPID)
		if err != nil || strings.HasPrefix(state, "Z") { break }
		if time.Since(started) > 2*time.Second { t.Fatal("native Codex process survived cancellation") }
		time.Sleep(20*time.Millisecond)
	}
	if !failedWrite {
	if lateBytes, err := os.ReadFile(latePIDFile); err != nil {
		t.Fatalf("late Codex tool host was not spawned during cancellation: %v", err)
	} else {
		latePID, err := strconv.Atoi(strings.TrimSpace(string(lateBytes)))
		if err != nil { t.Fatal(err) }
		defer func() { _ = syscall.Kill(latePID, syscall.SIGKILL) }()
		state, err := processState(latePID)
		if err == nil && !strings.HasPrefix(state, "Z") {
			t.Fatalf("late Codex tool host survived cancellation: pid=%d state=%s", latePID, state)
		}
	}
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Codex detached tool cleanup took %v", elapsed)
	}
	if err := other.Process.Signal(syscall.Signal(0)); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("unrelated process terminated: %v", err)
	}
}
