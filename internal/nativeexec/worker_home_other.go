//go:build !windows

package nativeexec

import "context"
func WindowsWorkerHome(ctx context.Context) (string,error) {return "",nil}
