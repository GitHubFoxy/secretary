//go:build windows

package main

import (
	"log"
 "crypto/sha256"
 "encoding/hex"
 "path/filepath"
 "strings"
 "fmt"
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

func claimNodeInstance(dataDir string) (func(), error) {
 absolute,err:=filepath.Abs(dataDir)
 if err!=nil{return nil,err}
 digest:=sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absolute))))
 name:="Local\\SecretaryNodeData-"+hex.EncodeToString(digest[:16])
 ptr,err:=syscall.UTF16PtrFromString(name)
 if err!=nil{return nil,err}
 handle,_,callErr:=nodeKernel32.NewProc("CreateMutexW").Call(0,1,uintptr(unsafe.Pointer(ptr)))
 if handle==0{return nil,fmt.Errorf("create Node ownership mutex: %w",callErr)}
 if callErr==syscall.ERROR_ALREADY_EXISTS {
  closeNodeHandle.Call(handle)
  return nil,fmt.Errorf("Node already active for data directory %s",absolute)
 }
 return func(){closeNodeHandle.Call(handle)},nil
}
