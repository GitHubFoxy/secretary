//go:build !windows

package main

import (
    "os"
    "syscall"
)

// Private umask is an additional Unix filesystem safeguard; Windows uses ACLs.
func setPrivateNodeUmask() { _ = syscall.Umask(0o077) }

func nodeShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
