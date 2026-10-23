//go:build !windows

package acp

import (
	"errors"
	"strings"
	"testing"
)

func TestMAC1204RecoveredInspectionStillKillsRecordedDetachedChild(t *testing.T) {
	inspectionFailure := errors.New("transient ps inspection failure")
	rootPID, childPID, unrelatedPID := 41001, 41002, 41003
	root := acpProcessIdentity{pid: rootPID, group: rootPID, parent: 1, start: "Fri Oct 23 12:00:00 2026", state: "S"}
	child := acpProcessIdentity{pid: childPID, group: childPID, parent: 1, start: "Fri Oct 23 12:00:01 2026", state: "S"}
	unrelated := acpProcessIdentity{pid: unrelatedPID, group: unrelatedPID, parent: 1, start: "Fri Oct 23 12:00:02 2026", state: "S"}
	processes := map[int]acpProcessIdentity{rootPID: root, childPID: child, unrelatedPID: unrelated}
	killed := make(map[int]bool)
	tracker := &acpDescendantTracker{
		root: root,
		seen: map[int]acpProcessIdentity{childPID: child},
		inspectionErr: inspectionFailure,
		snapshot: func() (map[int]acpProcessIdentity, error) {
			current := make(map[int]acpProcessIdentity)
			for pid, process := range processes {
				if !killed[pid] { current[pid] = process }
			}
			return current, nil
		},
		signal: func(pid int) error {
			killed[pid] = true
			return nil
		},
	}
	cleanup, prepareErr := prepareACPDescendantCleanup(tracker)
	if !errors.Is(prepareErr, inspectionFailure) { t.Fatalf("prior inspection error lost: %v", prepareErr) }
	if cleanup == nil { t.Fatal("cleanup discarded after recovered inspection") }
	err := cleanup()
	if !errors.Is(err, inspectionFailure) { t.Fatalf("cleanup lost original inspection error: %v", err) }
	if !killed[childPID] { t.Fatal("recorded detached child survived recovered cleanup") }
	if killed[rootPID] || killed[unrelatedPID] { t.Fatalf("unrelated or native PID signaled: %+v", killed) }
	if !strings.Contains(err.Error(), "transient ps") { t.Fatalf("inspection error absent from result: %v", err) }
}

func TestMAC1204RecoveredInspectionRejectsChangedDescendantIdentity(t *testing.T) {
	rootPID, childPID := 42001, 42002
	root := acpProcessIdentity{pid: rootPID, group: rootPID, start: "Fri Oct 23 12:00:00 2026", state: "S"}
	old := acpProcessIdentity{pid: childPID, group: childPID, start: "Fri Oct 23 12:00:01 2026", state: "S"}
	reused := old
	reused.start = "Fri Oct 23 12:01:01 2026"
	killed := false
	tracker := &acpDescendantTracker{
		root: root,
		seen: map[int]acpProcessIdentity{childPID: old},
		inspectionErr: errors.New("transient ps"),
		snapshot: func() (map[int]acpProcessIdentity, error) {
			return map[int]acpProcessIdentity{rootPID: root, childPID: reused}, nil
		},
		signal: func(int) error { killed = true; return nil },
	}
	cleanup, err := prepareACPDescendantCleanup(tracker)
	if err == nil || cleanup == nil { t.Fatal("expected original inspection error and cleanup") }
	if err := cleanup(); err == nil { t.Fatal("lost original failure") }
	if killed { t.Fatal("changed PID identity was signaled") }
}
