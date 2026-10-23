//go:build !windows

package node

import "os/exec"

func prepareClaudeProbeProcess(*exec.Cmd) error { return nil }

func attachClaudeProbeProcess(*exec.Cmd) (func(), error) {
	return func() {}, nil
}
