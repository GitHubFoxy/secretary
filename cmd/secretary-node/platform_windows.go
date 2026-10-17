//go:build windows

package main

import "os"

// Windows filesystem permissions are governed by the containing directory ACL.
func setPrivateNodeUmask() {}

func nodeShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }
