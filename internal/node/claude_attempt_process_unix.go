//go:build !windows

package node

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type claudeChild struct {
	pid int
	parent int
	group int
	start string
	state string
}

func claudeProcessSnapshot() (map[int]claudeChild, error) {
	output, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,stat=,lstart=").Output()
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

func cleanupClaudeAttemptProcesses(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	root := cmd.Process.Pid
	initial, err := claudeProcessSnapshot()
	if err != nil {
		return fmt.Errorf("inspect Claude descendants: %w", err)
	}
	owner, exists := initial[root]
	if !exists || strings.HasPrefix(owner.state, "Z") {
		return nil
	}
	children := claudeDescendants(root, initial)
	for _, child := range children {
		current, err := claudeProcessSnapshot()
		if err != nil {
			return fmt.Errorf("recheck Claude descendants: %w", err)
		}
		if process, ok := current[child.pid]; ok && process.start == child.start && process.parent == child.parent && process.group == child.group && !strings.HasPrefix(process.state, "Z") {
			if err := syscall.Kill(child.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("stop Claude descendant %d: %w", child.pid, err)
			}
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, err := claudeProcessSnapshot()
		if err != nil {
			return fmt.Errorf("verify Claude descendants: %w", err)
		}
		remaining := false
		for _, child := range children {
			if process, ok := current[child.pid]; ok && process.start == child.start && !strings.HasPrefix(process.state, "Z") {
				remaining = true
				if process.parent == child.parent && process.group == child.group {
					_ = syscall.Kill(child.pid, syscall.SIGKILL)
				}
			}
		}
		if !remaining {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("Claude descendants did not exit within 2 seconds")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
