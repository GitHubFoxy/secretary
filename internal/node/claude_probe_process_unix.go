//go:build !windows

package node

import "os/exec"

func attachClaudeProbeProcess(*exec.Cmd) (func(), error) {
	return func() {}, nil
}
