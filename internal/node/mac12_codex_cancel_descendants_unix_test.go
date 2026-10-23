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
	if os.Getenv("MAC1204_HELPER") == "1" {
		_ = os.WriteFile(os.Getenv("MAC1204_ROOT_PID_FILE"), []byte(strconv.Itoa(os.Getpid())), 0600)
		child := exec.Command("/bin/sleep", "45")
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(os.Getenv("MAC1204_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0600)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "session/cancel") {
				os.Exit(0)
			}
		}
		os.Exit(0)
	}
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	pidFile := filepath.Join(t.TempDir(), "sleep.pid")
	rootPIDFile := filepath.Join(filepath.Dir(pidFile), "root.pid")
	t.Setenv("MAC1204_HELPER", "1")
	t.Setenv("MAC1204_PID_FILE", pidFile)
	t.Setenv("MAC1204_ROOT_PID_FILE", rootPIDFile)
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
	session := newACPSession("native-session", client, true)
	session.codexMessagePhases = true
	started := time.Now()
	if err := session.Cancel(ctx); err != nil {
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
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Codex detached tool cleanup took %v", elapsed)
	}
	if err := other.Process.Signal(syscall.Signal(0)); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("unrelated process terminated: %v", err)
	}
}
