//go:build !windows

package acp

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type acpProcessIdentity struct {
	pid int
	parent int
	group int
	start string
	state string
}

func acpProcessSnapshot() (map[int]acpProcessIdentity, error) {
	output, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,stat=,lstart=").Output()
	if err != nil {
		return nil, err
	}
	processes := make(map[int]acpProcessIdentity)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		pid, errPID := strconv.Atoi(fields[0])
		parent, errParent := strconv.Atoi(fields[1])
		group, errGroup := strconv.Atoi(fields[2])
		if errPID != nil || errParent != nil || errGroup != nil {
			continue
		}
		processes[pid] = acpProcessIdentity{pid: pid, parent: parent, group: group, state: fields[3], start: strings.Join(fields[4:], " ")}
	}
	return processes, nil
}

func sameACPProcess(a, b acpProcessIdentity) bool {
	return a.pid == b.pid && a.group == b.group && a.start == b.start
}

func prepareACPDescendantCleanup(pid int) (func() error, error) {
	if pid <= 1 {
		return nil, errors.New("ACP native process identity unavailable")
	}
	processes, err := acpProcessSnapshot()
	if err != nil {
		return nil, err
	}
	root, ok := processes[pid]
	if !ok || root.group != pid || strings.HasPrefix(root.state, "Z") {
		return nil, errors.New("ACP native process identity unavailable")
	}
	owned := map[int]bool{pid: true}
	children := make([]acpProcessIdentity, 0)
	for {
		added := false
		for _, process := range processes {
			if !owned[process.pid] && owned[process.parent] {
				owned[process.pid] = true
				children = append(children, process)
				added = true
			}
		}
		if !added {
			break
		}
	}
	return func() error {
		current, err := acpProcessSnapshot()
		if err != nil {
			return err
		}
		if now, ok := current[pid]; ok && !sameACPProcess(now, root) && !strings.HasPrefix(now.state, "Z") {
			return errors.New("ACP native process identity changed during cancellation")
		}
		var failures []error
		for i := len(children) - 1; i >= 0; i-- {
			child := children[i]
			now, ok := current[child.pid]
			if !ok || !sameACPProcess(now, child) || strings.HasPrefix(now.state, "Z") {
				continue
			}
			if err := syscall.Kill(child.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
				failures = append(failures, fmt.Errorf("stop ACP descendant %d: %w", child.pid, err))
			}
		}
		return errors.Join(failures...)
	}, nil
}
