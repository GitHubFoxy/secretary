//go:build !windows

package nativeexec

func invocation(command string, args []string) (string, []string, error) {
    return command, args, nil
}
