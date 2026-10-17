//go:build !linux

package nativeexec

import "errors"

// Refuse insecure same-UID execution until this OS has an equivalent sandbox.
func sandboxInvocation(policy WorkerSandbox, command string, args []string) (string, []string, error) {
	return "", nil, errors.New("worker sandbox: OS isolation unsupported; refusing untrusted native Worker")
}
