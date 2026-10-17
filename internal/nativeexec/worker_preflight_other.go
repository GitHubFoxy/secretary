//go:build !windows

package nativeexec

func PreflightWorkerSandbox(protectedDir string) error { return nil }

func ManagedWorkerWorkspaceRoot(protectedDir string)(string,error){return "",nil}
