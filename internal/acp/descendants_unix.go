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
	"sync"
	"time"
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

type acpDescendantTracker struct {
	root acpProcessIdentity
	mu sync.Mutex
	seen map[int]acpProcessIdentity
	done chan struct{}
	finished chan struct{}
	once sync.Once
}

func startACPDescendantTracker(pid int) *acpDescendantTracker {
	t := &acpDescendantTracker{root: acpProcessIdentity{pid: pid}, seen: make(map[int]acpProcessIdentity), done: make(chan struct{}), finished: make(chan struct{})}
	t.sample()
	go func() {
		defer close(t.finished)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				t.sample()
			case <-t.done:
				return
			}
		}
	}()
	return t
}

func (t *acpDescendantTracker) stop() {
	t.once.Do(func() { close(t.done) })
	<-t.finished
}

func (t *acpDescendantTracker) sample() error {
	processes, err := acpProcessSnapshot()
	if err != nil { return err }
	t.mu.Lock()
	defer t.mu.Unlock()
	root, ok := processes[t.root.pid]
	if !ok || root.group != t.root.pid || strings.HasPrefix(root.state, "Z") { return nil }
	if t.root.start == "" { t.root = root }
	if !sameACPProcess(root, t.root) { return errors.New("ACP root identity changed") }
	owned := map[int]bool{t.root.pid: true}
	for pid, recorded := range t.seen {
		if now, ok := processes[pid]; ok && sameACPProcess(now, recorded) {
			owned[pid] = true
		}
	}
	for {
		added := false
		for _, p := range processes {
			if !owned[p.pid] && owned[p.parent] && !strings.HasPrefix(p.state, "Z") {
				owned[p.pid] = true
				t.seen[p.pid] = p
				added = true
			}
		}
		if !added { break }
	}
	return nil
}

func (t *acpDescendantTracker) cleanup() error {
	var failures []error
	deadline := time.Now().Add(2 * time.Second)
	quietUntil := time.Now().Add(350 * time.Millisecond)
	for {
		if err := t.sample(); err != nil { failures = append(failures, err) }
		t.mu.Lock()
		children := make([]acpProcessIdentity, 0, len(t.seen))
		for _, child := range t.seen { children = append(children, child) }
		t.mu.Unlock()
		processes, err := acpProcessSnapshot()
		if err != nil { return errors.Join(append(failures, err)...)}
		active := false
		for _, child := range children {
			current, ok := processes[child.pid]
			if !ok || !sameACPProcess(current, child) || strings.HasPrefix(current.state, "Z") { continue }
			active = true
			if err := syscall.Kill(child.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
				failures = append(failures, fmt.Errorf("stop ACP descendant %d: %w", child.pid, err))
			}
		}
		if !active && time.Now().After(quietUntil) { return errors.Join(failures...) }
		if time.Now().After(deadline) {
			return errors.Join(append(failures, errors.New("ACP descendants remain alive after two seconds"))...)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func prepareACPDescendantCleanup(tracker *acpDescendantTracker) (func() error, error) {
	if tracker == nil { return nil, nil }
	if err := tracker.sample(); err != nil { return nil, err }
	return tracker.cleanup, nil
}
