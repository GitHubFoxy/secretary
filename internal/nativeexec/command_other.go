//go:build !windows

package nativeexec

import "os/exec"

func invocation(command string, args []string) (string, []string, error) {
    return command, args, nil
}

func configureCommandLine(_ *exec.Cmd, _ string, _ []string) {}
