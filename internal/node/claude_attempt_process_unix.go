//go:build !windows

package node

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const claudeCleanupLimit = 2 * time.Second


type claudeChild struct {
	pid int
	parent int
	group int
	start string
	state string
}

func claudeProcessSnapshotContext(ctx context.Context) (map[int]claudeChild, error) {
	output, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=,stat=,lstart=").Output()
	if err != nil {
		return nil, err
	}
	processes := make(map[int]claudeChild)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		group, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		processes[pid] = claudeChild{pid: pid, parent: parent, group: group, state: fields[3], start: strings.Join(fields[4:], " ")}
	}
	return processes, nil
}

func claudeProcessSnapshot() (map[int]claudeChild, error) {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCleanupLimit)
	defer cancel()
	return claudeProcessSnapshotContext(ctx)
}

func captureClaudeRoot(cmd *exec.Cmd) (claudeChild, error) {
	return captureClaudeRootWithSnapshot(cmd, claudeProcessSnapshotContext)
}

func captureClaudeRootWithSnapshot(cmd *exec.Cmd, snapshot func(context.Context) (map[int]claudeChild, error)) (claudeChild, error) {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCleanupLimit)
	defer cancel()
	processes, err := snapshot(ctx)
	if err != nil {
		return claudeChild{}, err
	}
	root, ok := processes[cmd.Process.Pid]
	if !ok {
		if err := syscall.Kill(cmd.Process.Pid, 0); errors.Is(err, syscall.ESRCH) {
			return claudeChild{}, errClaudeRootAlreadyExited
		} else if err != nil {
			return claudeChild{}, fmt.Errorf("verify missing Claude process identity: %w", err)
		}
		return claudeChild{}, errors.New("Claude process is alive but absent from process inspection")
	}
	if strings.HasPrefix(root.state, "Z") {
		return claudeChild{}, errClaudeRootAlreadyExited
	}
	if root.group != cmd.Process.Pid {
		return claudeChild{}, errors.New("Claude process identity unavailable")
	}
	return root, nil
}

func sameClaudeProcess(a, b claudeChild) bool {
	return a.pid == b.pid && a.start == b.start && a.group == b.group && a.parent == b.parent
}

func claudeDescendants(root int, processes map[int]claudeChild) []claudeChild {
	visited := map[int]bool{root: true}
	var ordered []claudeChild
	for {
		added := false
		for _, process := range processes {
			if !visited[process.pid] && visited[process.parent] {
				visited[process.pid] = true
				ordered = append(ordered, process)
				added = true
			}
		}
		if !added {
			break
		}
	}
	for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
		ordered[i], ordered[j] = ordered[j], ordered[i]
	}
	return ordered
}

func cleanupClaudeAttemptProcesses(ctx context.Context, cmd *exec.Cmd, expected claudeChild) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, claudeCleanupLimit)
	defer cancel()
	initial, err := claudeProcessSnapshotContext(ctx)
	if err != nil {
		return fmt.Errorf("inspect Claude descendants: %w", err)
	}
	root, ok := initial[expected.pid]
	if !ok || !sameClaudeProcess(root, expected) || strings.HasPrefix(root.state, "Z") {
		return errors.New("Claude process identity changed before cancellation")
	}
	children := claudeDescendants(expected.pid, initial)
	for _, child := range children {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Claude cleanup timed out: %w", err)
		}
		current, err := claudeProcessSnapshotContext(ctx)
		if err != nil {
			return fmt.Errorf("recheck Claude descendants: %w", err)
		}
		if currentRoot, ok := current[expected.pid]; !ok || !sameClaudeProcess(currentRoot, expected) {
			return errors.New("Claude process identity changed during cancellation")
		}
		if process, ok := current[child.pid]; ok && sameClaudeProcess(process, child) && !strings.HasPrefix(process.state, "Z") {
			if err := syscall.Kill(child.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("stop Claude descendant %d: %w", child.pid, err)
			}
		}
	}
	for {
		current, err := claudeProcessSnapshotContext(ctx)
		if err != nil {
			return fmt.Errorf("verify Claude descendants: %w", err)
		}
		remaining := false
		for _, child := range children {
			if process, ok := current[child.pid]; ok && sameClaudeProcess(process, child) && !strings.HasPrefix(process.state, "Z") {
				remaining = true
			}
		}
		if !remaining {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Claude descendants did not exit within two seconds: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Claude descendants did not exit within two seconds: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}
