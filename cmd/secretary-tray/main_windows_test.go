//go:build windows

package main

import (
	"os"
	"os/exec"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestMain(m *testing.M) {
	switch os.Getenv("SECRETARY_TRAY_TEST_HELPER") {
	case "managed":
		if marker := os.Getenv("SECRETARY_TRAY_TEST_MARKER"); marker != "" {
			file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil { os.Exit(35) }
			_, err = file.WriteString("node started\\n")
			_ = file.Close()
			if err != nil { os.Exit(36) }
		}
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

func TestActualTraySecondLaunchDoesNotSpawnNode(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "secretary-tray.exe")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil { t.Fatalf("build tray fixture: %v: %s", err, output) }
	marker := filepath.Join(t.TempDir(), "managed-starts.txt")
	startTray := func() *exec.Cmd {
		cmd := exec.Command(binary)
		cmd.Env = append(os.Environ(),
			"SECRETARY_TRAY_NODE_EXE="+os.Args[0],
			"SECRETARY_TRAY_TEST_HELPER=managed",
			"SECRETARY_TRAY_TEST_MARKER="+marker,
		)
		if err := cmd.Start(); err != nil { t.Fatal(err) }
		return cmd
	}
	first := startTray()
	defer func() { _ = first.Process.Kill(); _ = first.Wait() }()
	deadline := time.Now().Add(15*time.Second)
	for {
		data, _ := os.ReadFile(marker)
		if strings.Count(string(data), "node started") == 1 { break }
		if time.Now().After(deadline) { t.Fatalf("first tray did not start managed Node; marker=%q", data) }
		time.Sleep(50*time.Millisecond)
	}
	second := startTray()
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case err := <-done:
		if err != nil { t.Fatalf("second tray exited unsuccessfully: %v", err) }
	case <-time.After(5*time.Second):
		_ = second.Process.Kill()
		t.Fatal("second tray instance remained alive")
	}
	time.Sleep(300*time.Millisecond)
	data, err := os.ReadFile(marker)
	if err != nil { t.Fatal(err) }
	if strings.Count(string(data), "node started") != 1 {
		t.Fatalf("second tray started an additional Node: %q", data)
	}
	process, err := syscall.OpenProcess(0x00100000, false, uint32(first.Process.Pid))
	if err != nil { t.Fatalf("first tray is no longer alive: %v", err) }
	defer syscall.CloseHandle(process)
	value, _, _ := k.NewProc("WaitForSingleObject").Call(uintptr(process), 0)
	if value != 0x102 { t.Fatalf("first tray unexpectedly exited: %d", value) }
}

func TestPopupClampedToWorkArea(t *testing.T) {
	work := rect{left:-1920, top:40, right:0, bottom:1040}
	for _, tc := range []struct{ point pt; x, y int32 }{
		{pt{-1930,0},-1920,40},
		{pt{-1,1039},-220,936},
		{pt{-1000,500},-1000,500},
	} {
		x,y := clampPopup(tc.point,work,220,104)
		if x != tc.x || y != tc.y { t.Fatalf("clampPopup(%+v) = %d,%d; want %d,%d",tc.point,x,y,tc.x,tc.y) }
	}
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

func TestThemeSelection(t *testing.T) {
	for _, tc := range []struct { value uint64; err error; want bool }{
		{0,nil,true},
		{1,nil,false},
		{2,nil,false},
		{0,errors.New("missing preference"),false},
	} {
		if got := themeIsDark(tc.value,tc.err); got != tc.want {
			t.Fatalf("themeIsDark(%d,%v) = %v; want %v",tc.value,tc.err,got,tc.want)
		}
	}
}
