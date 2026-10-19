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

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/traystatus"
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
	dataDir := t.TempDir()
	a := &trayApp{dataDir: dataDir, logPath: filepath.Join(dataDir, "logs", "node.log")}
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
	process, openErr := syscall.OpenProcess(0x00100000, false, uint32(unrelated.Process.Pid))
	if openErr != nil { t.Fatalf("unrelated process no longer available: %v", openErr) }
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

func TestW02LiveStatusTransitionsAndActions(t *testing.T) {
	cases:=[]struct{running bool; failed bool; live liveStatus; want string}{
		{false,false,liveStatus{},"Server: Offline"},
		{true,false,liveStatus{},"Server: Reconnecting"},
		{true,false,liveStatus{Connected:true},"Server: Connected"},
	}
	for _,tc:=range cases {
		rows:=buildMenu(tc.running,tc.failed,tc.live)
		if rows[1].label!=tc.want || !rows[2].separator {t.Fatalf("invalid server row/separator: %+v",rows)}
		if actionAt(rows,8+24+24+9+24)!="toggle" {t.Fatalf("missing node control: %+v",rows)}
		if actionAt(rows,menuHeight(rows)-8)!="exit" {t.Fatalf("missing exit action: %+v",rows)}
	}
}
func TestW02HarnessTransitionsAndFailureClosed(t *testing.T) {
	live:=liveStatus{Connected:true,Harnesses:[]harnessRow{{Name:"codex",Ready:true},{Name:"claude_code",Ready:false}}}
	rows:=buildMenu(true,false,live)
	if rows[3].label!="codex: Ready" || rows[4].label!="claude_code: Unavailable" {t.Fatalf("incorrect inventory: %+v",rows)}
	live.Harnesses[0].Ready=false
	if got:=buildMenu(true,false,live)[3].label;got!="codex: Unavailable"{t.Fatal(got)}
	if got:=buildMenu(false,false,live)[3].label;got!="codex: Unavailable"{t.Fatal(got)}
}
func TestW02CompactLayoutAndTheme(t *testing.T) {
	rows:=buildMenu(true,false,liveStatus{Harnesses:[]harnessRow{{Name:"codex"}}})
	if menuHeight(rows)!=8+24*7+9+4 {t.Fatalf("unexpected height %d",menuHeight(rows))}
	if !themeIsDark(0,nil) || themeIsDark(1,nil) {t.Fatal("theme mismatch")}
}

func TestW02StatusSnapshotPropagation(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"status.json")
	now:=time.Now().UTC()
	a:=&trayApp{statusFile:path,node:&managedNode{}}
	write:=func(connected bool, harnesses []harnessRow) {
		t.Helper()
		inventory:=core.HarnessInventorySnapshot{Node:"test",ObservedAt:now}
		for _,h:=range harnesses {
			status:=core.HarnessUnavailable
			if h.Ready {status=core.HarnessReady}
			inventory.Instances=append(inventory.Instances,core.HarnessInstance{
				ID:core.HarnessInstanceID("test/"+h.Name),Node:"test",Kind:core.HarnessKind(h.Name),Version:"1.0",
				Status:status,Authentication:core.HarnessAuthentication{Authenticated:h.Ready,Method:"local"},
			})
		}
		if err:=traystatus.Write(path,connected,inventory);err!=nil {t.Fatal(err)}
	}
	check:=func(wantServer,wantHarness string) {
		t.Helper()
		a.refreshStatus(time.Now())
		a.lock.Lock()
		rows:=buildMenu(a.node!=nil,false,a.live)
		a.lock.Unlock()
		if rows[1].label!=wantServer {t.Fatalf("server=%s, want %s",rows[1].label,wantServer)}
		if wantHarness!="" && rows[3].label!=wantHarness {t.Fatalf("harness=%s, want %s",rows[3].label,wantHarness)}
	}
	check("Server: Reconnecting","Harnesses: Unavailable")
	write(true,[]harnessRow{{Name:"codex",Ready:true}})
	check("Server: Connected","codex: Ready")
	write(true,[]harnessRow{{Name:"codex",Ready:false}})
	check("Server: Connected","codex: Unavailable")
	write(false,nil)
	check("Server: Reconnecting","Harnesses: Unavailable")
	write(true,[]harnessRow{{Name:"claude_code",Ready:true}})
	check("Server: Connected","claude_code: Ready")
	if err:=os.Remove(path);err!=nil{t.Fatal(err)}
	check("Server: Reconnecting","Harnesses: Unavailable")
	write(true,[]harnessRow{{Name:"codex",Ready:true}})
	a.refreshStatus(time.Now().Add(21*time.Second))
	a.lock.Lock()
	if a.live.Connected {t.Fatal("stale snapshot remained connected")}
	a.node=nil
	a.lock.Unlock()
	check("Server: Offline","Harnesses: Unavailable")
}

func TestW02PopupHeightFollowsInventory(t *testing.T) {
	before:=buildMenu(true,false,liveStatus{Harnesses:[]harnessRow{{Name:"codex"}}})
	after:=buildMenu(true,false,liveStatus{Harnesses:[]harnessRow{{Name:"codex"},{Name:"claude_code"},{Name:"fx"}}})
	if menuHeight(after)-menuHeight(before)!=48 {t.Fatalf("popup height not adjusted: %d -> %d",menuHeight(before),menuHeight(after))}
	if actionAt(after,menuHeight(after)-8)!="exit" {t.Fatal("exit action not repositioned")}
}

func TestW03UpgradeCloseShutsDownTray(t *testing.T) {
 binary:=filepath.Join(t.TempDir(),"secretary-tray.exe")
 build:=exec.Command("go","build","-o",binary,".")
 if output,err:=build.CombinedOutput();err!=nil{t.Fatalf("build tray fixture: %v: %s",err,output)}
 marker:=filepath.Join(t.TempDir(),"node-starts.txt")
 cmd:=exec.Command(binary,"--data-dir",t.TempDir())
 cmd.Env=append(os.Environ(),"SECRETARY_TRAY_NODE_EXE="+os.Args[0],"SECRETARY_TRAY_TEST_HELPER=managed","SECRETARY_TRAY_TEST_MARKER="+marker)
 if err:=cmd.Start();err!=nil{t.Fatal(err)}
 defer func(){_ = cmd.Process.Kill();_ = cmd.Wait()}()
 windowProc:=u.NewProc("FindWindowW")
 deadline:=time.Now().Add(15*time.Second)
 var window uintptr
 for time.Now().Before(deadline) {
  window,_,_=windowProc.Call(uintptr(unsafe.Pointer(wide("SecretaryNodeTrayWindow"))),uintptr(unsafe.Pointer(wide("Secretary Node"))))
  data,_:=os.ReadFile(marker)
  if window!=0 && strings.Contains(string(data),"node started"){break}
  time.Sleep(50*time.Millisecond)
 }
 if window==0{t.Fatal("tray window did not become available")}
 findWindowEx:=u.NewProc("FindWindowExW")
 var popup uintptr
 current:=uintptr(0)
 for {
  candidate,_,_:=findWindowEx.Call(0,current,uintptr(unsafe.Pointer(wide("SecretaryNodeTrayWindow"))),uintptr(unsafe.Pointer(wide("Secretary Node"))))
  if candidate==0{break}
  owner,_,_:=u.NewProc("GetWindow").Call(candidate,4)
  if owner!=0{popup=candidate;break}
  current=candidate
 }
 if popup==0{t.Fatal("owned tray popup was not created")}
 if result,_,err:=postMessage.Call(popup,wmClose,0,0);result==0{t.Fatalf("post close: %v",err)}
 process,err:=syscall.OpenProcess(0x00100000,false,uint32(cmd.Process.Pid))
 if err!=nil{t.Fatal(err)}
 defer syscall.CloseHandle(process)
 wait:=k.NewProc("WaitForSingleObject")
 result,_,_:=wait.Call(uintptr(process),25000)
 if result!=0{t.Fatalf("tray remained alive after cooperative upgrade close: %d",result)}
}
