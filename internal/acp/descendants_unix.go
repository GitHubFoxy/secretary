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
	inspectionErr error
}

func startACPDescendantTracker(pid int) (*acpDescendantTracker, error) {
	t := &acpDescendantTracker{root: acpProcessIdentity{pid: pid}, seen: make(map[int]acpProcessIdentity), done: make(chan struct{}), finished: make(chan struct{})}
	if err := t.captureRoot(); err != nil { return nil, err }
	go func() {
		defer close(t.finished)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				if err := t.sample(); err != nil { t.mu.Lock(); t.inspectionErr = errors.Join(t.inspectionErr, err); t.mu.Unlock() }
			case <-t.done:
				return
			}
		}
	}()
	return t, nil
}

func captureACPLaunchIdentity(pid int, snapshot func() (map[int]acpProcessIdentity, error)) (acpProcessIdentity, error) {
	processes, err := snapshot()
	if err != nil { return acpProcessIdentity{}, err }
	root, ok := processes[pid]
	if !ok || root.group != root.pid || strings.HasPrefix(root.state, "Z") || root.start == "" {
		return acpProcessIdentity{}, errors.New("ACP launch root identity unavailable")
	}
	return root, nil
}

func (t *acpDescendantTracker) captureRoot() error {
	deadline := time.Now().Add(200 * time.Millisecond)
	for {
		root, err := captureACPLaunchIdentity(t.root.pid, acpProcessSnapshot)
		if err == nil {
			t.root = root
			return t.sample()
		}
		if time.Now().After(deadline) { return fmt.Errorf("verify ACP launch process: %w", err) }
		time.Sleep(10*time.Millisecond)
	}
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
	if t.root.start == "" { return errors.New("ACP root identity unverified") }
	if !ok { return nil }
	if root.group != t.root.pid { return errors.New("ACP root identity invalid") }
	if strings.HasPrefix(root.state, "Z") { return nil }
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
	t.mu.Lock()
	priorErr := t.inspectionErr
	t.mu.Unlock()
	if priorErr != nil { return priorErr }
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
		t.mu.Lock()
		observedErr := t.inspectionErr
		t.mu.Unlock()
		if observedErr != nil { failures = append(failures, observedErr); return errors.Join(failures...) }
		if !active && time.Now().After(quietUntil) { return errors.Join(failures...) }
		if time.Now().After(deadline) {
			return errors.Join(append(failures, errors.New("ACP descendants remain alive after two seconds"))...)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func prepareACPDescendantCleanup(tracker *acpDescendantTracker) (func() error, error) {
	if tracker == nil { return nil, nil }
	tracker.mu.Lock()
	priorErr := tracker.inspectionErr
	tracker.mu.Unlock()
	if priorErr != nil { return tracker.cleanup, priorErr }
	if err := tracker.sample(); err != nil { return tracker.cleanup, err }
	return tracker.cleanup, nil
}
