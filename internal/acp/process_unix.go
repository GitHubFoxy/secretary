//go:build !windows

package acp

import "os/exec"

// Preserve existing Unix process cancellation behavior.
func configureACPProcess(_ *exec.Cmd) {}
