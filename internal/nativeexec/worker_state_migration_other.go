//go:build !windows

package nativeexec

func MigrateWindowsNativeStore(source,destination string) error { return nil }
