//go:build !windows

package nativeexec

func PreflightWorkerSandbox(protectedDir string) error { return nil }
