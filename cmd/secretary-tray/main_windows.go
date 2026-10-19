//go:build windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	wmDestroy = 2
	wmPaint = 15
	wmSettingChange = 26
	wmKillFocus = 8
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	wmThemeChanged = 0x031A
	wmApp = 0x8000
	trayCallback = wmApp + 1
	nodeChanged = wmApp + 2
	nimAdd = 0
	nimDelete = 2
	nifMessage = 1
	nifIcon = 2
	nifTip = 4
	wsPopup = 0x80000000
	wsExToolWindow = 0x00000080
	wsExTopmost = 0x00000008
	swHide = 0
	swShow = 5
	swpNoActivate = 0x0010
	monitorDefaultToNearest = 2
	monitorWorkArea = 1
	processSetQuota = 0x0100
	processTerminate = 0x0001
	createNoWindow = 0x08000000
	createNewProcessGroup = 0x00000200
	jobExtendedLimits = 9
	jobKillOnClose = 0x2000
	transparent = 1
	dtSingleLine = 0x20
	dtVCenter = 4
)

var (
	u = syscall.NewLazyDLL("user32.dll")
	s = syscall.NewLazyDLL("shell32.dll")
	k = syscall.NewLazyDLL("kernel32.dll")
	g = syscall.NewLazyDLL("gdi32.dll")
	registerClass = u.NewProc("RegisterClassExW")
	createWindow = u.NewProc("CreateWindowExW")
	defWindowProc = u.NewProc("DefWindowProcW")
	destroyWindow = u.NewProc("DestroyWindow")
	getMessage = u.NewProc("GetMessageW")
	translateMessage = u.NewProc("TranslateMessage")
	dispatchMessage = u.NewProc("DispatchMessageW")
	postQuitMessage = u.NewProc("PostQuitMessage")
	postMessage = u.NewProc("PostMessageW")
	getCursorPos = u.NewProc("GetCursorPos")
	monitorFromPoint = u.NewProc("MonitorFromPoint")
	getMonitorInfo = u.NewProc("GetMonitorInfoW")
	setWindowPos = u.NewProc("SetWindowPos")
	showWindow = u.NewProc("ShowWindow")
	setForeground = u.NewProc("SetForegroundWindow")
	invalidateRect = u.NewProc("InvalidateRect")
	beginPaint = u.NewProc("BeginPaint")
	endPaint = u.NewProc("EndPaint")
	fillRect = u.NewProc("FillRect")
	drawText = u.NewProc("DrawTextW")
	loadIcon = u.NewProc("LoadIconW")
	shellNotify = s.NewProc("Shell_NotifyIconW")
	getModuleHandle = k.NewProc("GetModuleHandleW")
	createMutex = k.NewProc("CreateMutexW")
	closeHandle = k.NewProc("CloseHandle")
	createEvent = k.NewProc("CreateEventW")
	setEvent = k.NewProc("SetEvent")
	createJob = k.NewProc("CreateJobObjectW")
	setJobInfo = k.NewProc("SetInformationJobObject")
	assignJob = k.NewProc("AssignProcessToJobObject")
	createBrush = g.NewProc("CreateSolidBrush")
	deleteObject = g.NewProc("DeleteObject")
	setTextColor = g.NewProc("SetTextColor")
	setBkMode = g.NewProc("SetBkMode")
)

type pt struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }
type monitorInfo struct {
	size uint32
	monitor rect
	work rect
	flags uint32
}

type msg struct {
	hwnd uintptr
	message uint32
	wparam uintptr
	lparam uintptr
	time uint32
	position pt
	private uint32
}
type wndClass struct {
	size uint32
	style uint32
	proc uintptr
	classExtra int32
	windowExtra int32
	instance uintptr
	icon uintptr
	cursor uintptr
	background uintptr
	menuName *uint16
	name *uint16
	smallIcon uintptr
}
type iconData struct {
	size uint32
	hwnd uintptr
	id uint32
	flags uint32
	callback uint32
	icon uintptr
	tip [128]uint16
	state uint32
	stateMask uint32
	info [256]uint16
	timeout uint32
	infoTitle [64]uint16
	infoFlags uint32
	guid [16]byte
	balloonIcon uintptr
}
type paintStruct struct {
	dc uintptr
	erase int32
	paint rect
	restore int32
	update int32
	reserved [32]byte
}
type basicLimits struct {
	processTime, jobTime int64
	flags uint32
	minimum, maximum uintptr
	active uint32
	affinity uintptr
	priority, scheduling uint32
}
type ioCounters struct{ read, write, other, readBytes, writeBytes, otherBytes uint64 }
type extendedLimits struct {
	basic basicLimits
	io ioCounters
	processMemory, jobMemory, peakProcess, peakJob uintptr
}
type managedNode struct {
	cmd *exec.Cmd
	job uintptr
	event uintptr
	done chan struct{}
}
type trayApp struct {
	hwnd uintptr
	popup uintptr
	lock sync.Mutex
	node *managedNode
	lastError string
	exiting bool
}
var app *trayApp

func wide(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }
func rgb(r, green, b uint32) uintptr { return uintptr(r | green<<8 | b<<16) }

func checked(p *syscall.LazyProc, args ...uintptr) (uintptr, error) {
	result, _, err := p.Call(args...)
	if result != 0 { return result, nil }
	if err != syscall.Errno(0) { return 0, err }
	return 0, fmt.Errorf("%s failed", p.Name)
}

func main() {
	mutexName := wide("Local\\SecretaryNodeTraySingleton")
	single, _, callErr := createMutex.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))
	if single == 0 { fmt.Fprintln(os.Stderr, "tray mutex:", callErr); os.Exit(1) }
	defer closeHandle.Call(single)
	if callErr == syscall.ERROR_ALREADY_EXISTS { return }

	app = &trayApp{}
	className := wide("SecretaryNodeTrayWindow")
	module, err := checked(getModuleHandle, 0)
	if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	wc := wndClass{size: uint32(unsafe.Sizeof(wndClass{})), proc: syscall.NewCallback(windowProc), instance: module, name: className}
	if _, err := checked(registerClass, uintptr(unsafe.Pointer(&wc))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	hwnd, err := checked(createWindow, 0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("Secretary Node"))), 0, 0, 0, 0, 0, 0, 0, module, 0)
	if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	app.hwnd = hwnd
	popup, err := checked(createWindow, wsExToolWindow|wsExTopmost, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("Secretary Node"))), wsPopup, 0, 0, 220, 104, hwnd, 0, module, 0)
	if err != nil { destroyWindow.Call(hwnd); fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	app.popup = popup

	icon, _, _ := loadIcon.Call(0, uintptr(32512))
	data := iconData{size: uint32(unsafe.Sizeof(iconData{})), hwnd: hwnd, id: 1, flags: nifMessage|nifIcon|nifTip, callback: trayCallback, icon: icon}
	copy(data.tip[:], syscall.StringToUTF16("Secretary Node"))
	if _, err := checked(shellNotify, nimAdd, uintptr(unsafe.Pointer(&data))); err != nil {
		destroyWindow.Call(popup); destroyWindow.Call(hwnd)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	go app.start()
	var m msg
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(result) <= 0 { break }
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	app.stop()
	shellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	destroyWindow.Call(popup)
	destroyWindow.Call(hwnd)
}

func windowProc(hwnd uintptr, message uint32, wp, lp uintptr) uintptr {
	if app == nil { result, _, _ := defWindowProc.Call(hwnd, uintptr(message), wp, lp); return result }
	switch message {
	case trayCallback:
		if uint32(lp) == wmLButtonUp || uint32(lp) == wmRButtonUp { app.showMenu() }
		return 0
	case wmPaint:
		if hwnd == app.popup { app.paintMenu(); return 0 }
	case wmLButtonUp:
		if hwnd == app.popup {
			y := int32(lp >> 16)
			showWindow.Call(app.popup, swHide)
			switch {
			case y >= 30 && y < 60:
				go app.toggle()
			case y >= 65 && y < 98:
				go func() { app.stop(); postMessage.Call(app.hwnd, wmDestroy, 0, 0) }()
			}
			return 0
		}
	case wmKillFocus:
		if hwnd == app.popup { showWindow.Call(hwnd, swHide); return 0 }
	case wmSettingChange, wmThemeChanged, nodeChanged:
		invalidateRect.Call(app.popup, 0, 1)
		return 0
	case wmDestroy:
		if hwnd == app.hwnd {
			app.lock.Lock()
			app.exiting = true
			app.lock.Unlock()
			postQuitMessage.Call(0)
			return 0
		}
	}
	result, _, _ := defWindowProc.Call(hwnd, uintptr(message), wp, lp)
	return result
}

func (a *trayApp) showMenu() {
	var p pt
	getCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	bounds := rect{}
	monitor, _, _ := monitorFromPoint.Call(uintptr(uint64(uint32(p.x))|uint64(uint32(p.y))<<32), monitorDefaultToNearest)
	info := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if monitor != 0 {
		if result, _, _ := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); result != 0 { bounds = info.work }
	}
	if bounds.right <= bounds.left || bounds.bottom <= bounds.top {
		bounds = rect{left: p.x, top: p.y, right: p.x+220, bottom: p.y+104}
	}
	x, y := clampPopup(p, bounds, 220, 104)
	setWindowPos.Call(a.popup, ^uintptr(0), uintptr(x), uintptr(y), 220, 104, swpNoActivate)
	invalidateRect.Call(a.popup, 0, 1)
	showWindow.Call(a.popup, swShow)
	setForeground.Call(a.popup)
}

func clampPopup(p pt, work rect, width, height int32) (int32, int32) {
	clamp := func(value, lo, hi int32) int32 {
		if hi < lo { hi = lo }
		if value < lo { return lo }
		if value > hi { return hi }
		return value
	}
	return clamp(p.x, work.left, work.right-width), clamp(p.y, work.top, work.bottom-height)
}

func themeIsDark(value uint64, err error) bool { return themeIsDark(value, err) }

func darkMode() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil { return false }
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	return err == nil && value == 0
}

func (a *trayApp) paintMenu() {
	var ps paintStruct
	dc, _, _ := beginPaint.Call(a.popup, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 { return }
	defer endPaint.Call(a.popup, uintptr(unsafe.Pointer(&ps)))
	dark := darkMode()
	bg, fg, muted := rgb(255,255,255), rgb(25,25,25), rgb(100,100,100)
	if dark { bg, fg, muted = rgb(32,32,32), rgb(242,242,242), rgb(175,175,175) }
	brush, _, _ := createBrush.Call(bg)
	r := rect{0,0,220,104}
	fillRect.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	deleteObject.Call(brush)
	setBkMode.Call(dc, transparent)
	a.lock.Lock()
	running, last := a.node != nil, a.lastError
	a.lock.Unlock()
	status := "Stopped"
	action := "Start Secretary Node"
	if running { status, action = "Running", "Quit Secretary Node" } else if last != "" { status = "Stopped (start failed)" }
	a.draw(dc, "Secretary Node — "+status, rect{10,3,214,30}, muted)
	a.draw(dc, action, rect{10,30,214,60}, fg)
	a.draw(dc, "Exit tray", rect{10,65,214,98}, fg)
}

func (a *trayApp) draw(dc uintptr, value string, r rect, color uintptr) {
	setTextColor.Call(dc, color)
	drawText.Call(dc, uintptr(unsafe.Pointer(wide(value))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), dtSingleLine|dtVCenter)
}

func (a *trayApp) toggle() {
	a.lock.Lock()
	running := a.node != nil
	a.lock.Unlock()
	if running { a.stop() } else { a.start() }
}

func (a *trayApp) start() {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.node != nil || a.exiting { return }
	file := os.Getenv("SECRETARY_TRAY_NODE_EXE")
	if file == "" {
		exe, err := os.Executable()
		if err != nil { a.lastError = err.Error(); return }
		file = filepath.Join(filepath.Dir(exe), "secretary-node.exe")
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil { a.lastError = err.Error(); return }
	eventName := "Local\\SecretaryNodeStop-" + hex.EncodeToString(token)
	event, err := checked(createEvent, 0, 1, 0, uintptr(unsafe.Pointer(wide(eventName))))
	if err != nil { a.lastError = err.Error(); return }
	job, err := checked(createJob, 0, 0)
	if err != nil { closeHandle.Call(event); a.lastError = err.Error(); return }
	limits := extendedLimits{}
	limits.basic.flags = jobKillOnClose
	if _, err = checked(setJobInfo, job, jobExtendedLimits, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); err != nil {
		closeHandle.Call(job); closeHandle.Call(event); a.lastError = err.Error(); return
	}
	args := []string{}
	if config := os.Getenv("SECRETARY_TRAY_NODE_CONFIG"); config != "" { args = append(args, "--config", config) }
	cmd := exec.Command(file, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | createNewProcessGroup, HideWindow: true}
	cmd.Env = append(os.Environ(), "SECRETARY_NODE_SHUTDOWN_EVENT="+eventName)
	if err = cmd.Start(); err != nil {
		closeHandle.Call(job); closeHandle.Call(event); a.lastError = err.Error(); return
	}
	process, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err == nil {
		_, err = checked(assignJob, job, uintptr(process))
		syscall.CloseHandle(process)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		closeHandle.Call(job); closeHandle.Call(event)
		a.lastError = err.Error()
		return
	}
	n := &managedNode{cmd: cmd, job: job, event: event, done: make(chan struct{})}
	a.node, a.lastError = n, ""
	go func() {
		_ = cmd.Wait()
		a.lock.Lock()
		if a.node == n { a.node = nil }
		closeHandle.Call(n.job)
		closeHandle.Call(n.event)
		close(n.done)
		a.lock.Unlock()
		postMessage.Call(a.hwnd, nodeChanged, 0, 0)
	}()
	postMessage.Call(a.hwnd, nodeChanged, 0, 0)
}

func (a *trayApp) stop() {
	a.lock.Lock()
	n := a.node
	if n != nil { setEvent.Call(n.event) }
	a.lock.Unlock()
	if n == nil { return }
	select {
	case <-n.done:
	case <-time.After(10*time.Second):
		_ = n.cmd.Process.Kill()
		<-n.done
	}
}
