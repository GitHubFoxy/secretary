//go:build !windows

package acp

import (
	"errors"
	"strings"
	"testing"
)

func TestMAC1204RejectsUnavailableAndInvalidACPLaunchIdentity(t *testing.T) {
	pid := 54321
	valid := acpProcessIdentity{pid: pid, parent: 1, group: pid, start: "Fri Oct 23 10:10:10 2026", state: "S"}
	for _, tc := range []struct {
		name string
		processes map[int]acpProcessIdentity
		err error
	}{
		{name: "missing", processes: map[int]acpProcessIdentity{}},
		{name: "wrong group", processes: map[int]acpProcessIdentity{pid: {pid:pid, group:pid+1, start:valid.start, state:"S"}}},
		{name: "zombie", processes: map[int]acpProcessIdentity{pid: {pid:pid, group:pid, start:valid.start, state:"Z"}}},
		{name: "missing birth identity", processes: map[int]acpProcessIdentity{pid: {pid:pid, group:pid, state:"S"}}},
		{name: "inspection error", err: errors.New("ps failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := captureACPLaunchIdentity(pid, func() (map[int]acpProcessIdentity, error) { return tc.processes, tc.err })
			if err == nil || root.start != "" { t.Fatalf("accepted unverified ACP root: %+v %v", root, err) }
		})
	}
	root, err := captureACPLaunchIdentity(pid, func() (map[int]acpProcessIdentity, error) {
		return map[int]acpProcessIdentity{pid: valid}, nil
	})
	if err != nil || !sameACPProcess(root, valid) { t.Fatalf("valid root rejected: %+v %v", root, err) }
}

func TestMAC1204UnverifiedRootCannotReportSuccessfulCleanup(t *testing.T) {
	tracker := &acpDescendantTracker{
		root: acpProcessIdentity{pid: 54321},
		seen: map[int]acpProcessIdentity{54322: {pid:54322, group:54322, start:"Fri Oct 23 10:10:10 2026", state:"S"}},
		snapshot: func() (map[int]acpProcessIdentity, error) {
			return map[int]acpProcessIdentity{54322: {pid:54322, group:54322, start:"Fri Oct 23 10:10:10 2026", state:"S"}}, nil
		},
	}
	cleanup, err := prepareACPDescendantCleanup(tracker)
	if err == nil || !strings.Contains(err.Error(), "unverified") { t.Fatalf("unverified root accepted: %v", err) }
	if cleanup == nil { t.Fatal("unverified root discarded cleanup procedure") }
}
