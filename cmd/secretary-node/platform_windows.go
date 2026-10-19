//go:build windows

package main

import (
	"log"
	"os"
	"syscall"
	"unsafe"
)

var (
	nodeKernel32 = syscall.NewLazyDLL("kernel32.dll")
	openNodeEvent = nodeKernel32.NewProc("OpenEventW")
	waitNodeEvent = nodeKernel32.NewProc("WaitForSingleObject")
	closeNodeHandle = nodeKernel32.NewProc("CloseHandle")
)

func setPrivateNodeUmask() {}

func nodeShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }

func watchNodeShutdownEvent(stop func()) {
	name := os.Getenv("SECRETARY_NODE_SHUTDOWN_EVENT")
	if name == "" {
		return
	}
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		log.Printf("invalid Node shutdown event: %v", err)
		stop()
		return
	}
	handle, _, callErr := openNodeEvent.Call(0x00100000, 0, uintptr(unsafe.Pointer(p)))
	if handle == 0 {
		log.Printf("open Node shutdown event: %v", callErr)
		stop()
		return
	}
	go func() {
		defer closeNodeHandle.Call(handle)
		result, _, err := waitNodeEvent.Call(handle, 0xffffffff)
		if result != 0 {
			log.Printf("wait Node shutdown event: %v", err)
		}
		stop()
	}()
}
