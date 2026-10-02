//go:build darwin || linux

package telegram

import (
	"errors"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

func configureTitleProcess(command *exec.Cmd) func() {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var cancelled atomic.Bool
	done := make(chan struct{})
	command.Cancel = func() error {
		cancelled.Store(true)
		group := -command.Process.Pid
		err := syscall.Kill(group, syscall.SIGTERM)
		go func() {
			defer close(done)
			time.Sleep(200 * time.Millisecond)
			_ = syscall.Kill(group, syscall.SIGKILL)
		}()
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return func() {
		if cancelled.Load() {
			<-done
		}
	}
}
