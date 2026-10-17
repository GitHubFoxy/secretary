//go:build !windows

package nativeexec

import "context"

func workerPlatformEnvironment(_ context.Context, entries []string) []string { return entries }
