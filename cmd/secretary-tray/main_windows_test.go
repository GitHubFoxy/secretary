//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestMain(m *testing.M) {
	switch os.Getenv("SECRETARY_TRAY_TEST_HELPER") {
	case "managed":
		name := wide(os.Getenv("SECRETARY_NODE_SHUTDOWN_EVENT"))
		event, _, _ := k.NewProc("OpenEventW").Call(0x00100000, 0, uintptr(unsafe.Pointer(name)))
		if event == 0 { os.Exit(31) }
		result, _, _ := k.NewProc("WaitForSingleObject").Call(event, 15000)
		closeHandle.Call(event)
		if result != 0 { os.Exit(32) }
		os.Exit(0)
	case "unrelated":
		time.Sleep(15 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNativeSingleInstanceMutex(t *testing.T) {
	name := wide("Local\\SecretaryNodeTrayTestMutex")
	first, err := checked(createMutex, 0, 1, uintptr(unsafe.Pointer(name)))
	if err != nil { t.Fatal(err) }
	defer closeHandle.Call(first)
	second, _, result := createMutex.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if second == 0 { t.Fatalf("second mutex open failed: %v", result) }
	defer closeHandle.Call(second)
	if result != syscall.ERROR_ALREADY_EXISTS { t.Fatalf("duplicate tray mutex: got %v", result) }
}

func TestManagedNodeLifecycleAndUnrelatedProcess(t *testing.T) {
	t.Setenv("SECRETARY_TRAY_NODE_EXE", os.Args[0])
	t.Setenv("SECRETARY_TRAY_TEST_HELPER", "managed")
	a := &trayApp{}
	a.start()
	a.lock.Lock()
	n := a.node
	err := a.lastError
	a.lock.Unlock()
	if n == nil { t.Fatalf("Node did not start: %s", err) }
	defer a.stop()

	a.start()
	a.lock.Lock()
	if a.node != n { t.Fatal("second launch replaced the managed Node") }
	a.lock.Unlock()

	unrelated := exec.Command(os.Args[0])
	unrelated.Env = append(os.Environ(), "SECRETARY_TRAY_TEST_HELPER=unrelated")
	if err := unrelated.Start(); err != nil { t.Fatal(err) }
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()

	a.stop()
	select {
	case <-n.done:
	default:
		t.Fatal("graceful shutdown did not wait for managed Node")
	}
	a.lock.Lock()
	stillRunning := a.node != nil
	a.lock.Unlock()
	if stillRunning { t.Fatal("managed Node still marked running") }
	process, err := syscall.OpenProcess(0x00100000, false, uint32(unrelated.Process.Pid))
	if err != nil { t.Fatalf("unrelated process no longer available: %v", err) }
	defer syscall.CloseHandle(process)
	value, _, _ := k.NewProc("WaitForSingleObject").Call(uintptr(process), 0)
	if value != 0x102 { t.Fatalf("unrelated process was terminated: wait=%d", value) }

	a.start()
	a.lock.Lock()
	restarted := a.node
	a.lock.Unlock()
	if restarted == nil || restarted == n { t.Fatal("Node did not restart after shutdown") }
	a.stop()
}

func TestStopWithoutOwnedNode(t *testing.T) {
	a := &trayApp{}
	a.stop()
	if a.node != nil { t.Fatal("stopping without an owned Node changed state") }
}

func TestThemeRegistryReturnsBoolean(t *testing.T) {
	_ = darkMode()
}
