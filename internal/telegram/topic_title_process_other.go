//go:build !darwin && !linux

package telegram

import "os/exec"

func configureTitleProcess(command *exec.Cmd) func() {
	return func() {}
}
