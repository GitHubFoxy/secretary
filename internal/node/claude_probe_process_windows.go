//go:build windows

package node

import (
	"errors"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var claudeProbeAssignJob = windows.AssignProcessToJobObject

func prepareClaudeProbeProcess(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	return nil
}

func attachClaudeProbeProcess(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = windows.CloseHandle(job)
		}
	}()
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return nil, err
	}
	err = claudeProbeAssignJob(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		return nil, err
	}
	if err = resumeClaudeProbeProcess(uint32(cmd.Process.Pid)); err != nil {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(job) }, nil
}

func resumeClaudeProbeProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	var thread windows.ThreadEntry32
	thread.Size = uint32(unsafe.Sizeof(thread))
	if err := windows.Thread32First(snapshot, &thread); err != nil {
		return err
	}
	for {
		if thread.OwnerProcessID == pid {
			handle, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, thread.ThreadID)
			if err != nil {
				return err
			}
			count, resumeErr := windows.ResumeThread(handle)
			_ = windows.CloseHandle(handle)
			if resumeErr != nil {
				return resumeErr
			}
			if count == 0xffffffff {
				return errors.New("Claude native probe could not resume suspended process")
			}
			return nil
		}
		err = windows.Thread32Next(snapshot, &thread)
		if err != nil {
			return err
		}
	}
}
