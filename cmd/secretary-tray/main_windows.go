//go:build windows

package main

import (
	"crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "strings"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
	"github.com/beruseruko/secretary/internal/traystatus"
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
	statusChanged = wmApp + 3
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
	statusFile string
	lock sync.Mutex
	node *managedNode
	lastError string
	dataDir string
	logPath string
	login bool
	live liveStatus
	exiting bool
}
type harnessRow = traystatus.Harness
type liveStatus = traystatus.Snapshot
type menuRow struct { label string; action string; separator bool }
func buildMenu(running bool, failed bool, live liveStatus) []menuRow {
	state := "Stopped"
	if running { state = "Running" } else if failed { state = "Stopped (start failed)" }
	server := "Offline"
	if running { server = "Reconnecting" }
	if running && live.Connected { server = "Connected" }
	rows := []menuRow{{label:"Secretary Node — "+state}, {label:"Server: "+server}, {separator:true}}
	for _, h := range live.Harnesses {
		status := "Unavailable"
		if h.Ready && running { status = "Ready" }
		rows = append(rows,menuRow{label: h.Name+": "+status})
	}
	if len(live.Harnesses)==0 { rows=append(rows,menuRow{label:"Harnesses: Unavailable"}) }
	act:="Start Secretary Node"
	if running {act="Quit Secretary Node"}
	return append(rows,menuRow{label:act,action:"toggle"},menuRow{label:"Launch at Login",action:"login"},menuRow{label:"Open Logs",action:"logs"},menuRow{label:"Exit tray",action:"exit"})
}
func menuHeight(rows []menuRow) int32 {
	var n int32 = 8
	for _, row := range rows {if row.separator {n+=9} else {n+=24}}
	return n+4
}
func actionAt(rows []menuRow, y int32) string {
	top:=int32(8)
	for _,row:=range rows {h:=int32(24);if row.separator {h=9};if y>=top && y<top+h {return row.action};top+=h}
	return ""
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

	dataDir := flag.String("data-dir", defaultTrayDataDir(), "Node data directory")
	flag.Parse()
	app = &trayApp{dataDir:*dataDir}
	app.logPath = filepath.Join(app.dataDir,"logs","node.log")
	app.login, _ = loginEnabled()
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
	go app.watchStatus()
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
			app.lock.Lock()
			rows := app.menuRows()
			app.lock.Unlock()
			switch actionAt(rows,y) {
			case "toggle": go app.toggle()
			case "login": go app.toggleLogin()
			case "logs": go app.openLogs()
			case "exit": go func() { app.stop(); postMessage.Call(app.hwnd, wmDestroy, 0, 0) }()
			}
			return 0
		}
	case wmKillFocus:
		if hwnd == app.popup { showWindow.Call(hwnd, swHide); return 0 }
	case wmSettingChange, wmThemeChanged, nodeChanged, statusChanged:
		app.resizeOpenMenu()
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
	a.lock.Lock()
	h := menuHeight(a.menuRows())
	a.lock.Unlock()
	x, y := clampPopup(p, bounds, 220, h)
	setWindowPos.Call(a.popup, ^uintptr(0), uintptr(x), uintptr(y), 220, uintptr(h), swpNoActivate)
	invalidateRect.Call(a.popup, 0, 1)
	showWindow.Call(a.popup, swShow)
	setForeground.Call(a.popup)
}

func (a *trayApp) resizeOpenMenu() {
	if visible, _, _ := u.NewProc("IsWindowVisible").Call(a.popup); visible == 0 { return }
	var bounds rect
	if got, _, _ := u.NewProc("GetWindowRect").Call(a.popup, uintptr(unsafe.Pointer(&bounds))); got == 0 { return }
	p := pt{x:bounds.left, y:bounds.top}
	monitor, _, _ := monitorFromPoint.Call(uintptr(uint64(uint32(p.x))|uint64(uint32(p.y))<<32), monitorDefaultToNearest)
	info := monitorInfo{size:uint32(unsafe.Sizeof(monitorInfo{}))}
	if monitor != 0 {
		if got, _, _ := getMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); got != 0 { bounds = info.work }
	}
	a.lock.Lock()
	h := menuHeight(a.menuRows())
	a.lock.Unlock()
	if bounds.right <= bounds.left || bounds.bottom <= bounds.top { return }
	x, y := clampPopup(p, bounds, 220, h)
	setWindowPos.Call(a.popup, ^uintptr(0), uintptr(x), uintptr(y), 220, uintptr(h), swpNoActivate)
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

func themeIsDark(value uint64, err error) bool { return err == nil && value == 0 }

func darkMode() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil { return false }
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	return themeIsDark(value, err)
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
	a.lock.Lock()
	rows := a.menuRows()
	a.lock.Unlock()
	r := rect{0,0,220,menuHeight(rows)}
	fillRect.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	deleteObject.Call(brush)
	setBkMode.Call(dc, transparent)
	top:=int32(8)
	for _, row := range rows {
		if row.separator {
			line:=rect{10,top+4,210,top+5}
			b,_,_:=createBrush.Call(muted)
			fillRect.Call(dc,uintptr(unsafe.Pointer(&line)),b)
			deleteObject.Call(b)
			top+=9
			continue
		}
		color:=fg
		if row.action=="" {color=muted}
		a.draw(dc,row.label,rect{10,top,214,top+24},color)
		top+=24
	}
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
	if active,err:=existingNodeForData(a.dataDir);err!=nil {a.lastError=err.Error();return} else if active {a.lastError="A headless Node already uses this data directory";return}
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
	args := []string{"--data-dir",a.dataDir}
	if config := os.Getenv("SECRETARY_TRAY_NODE_CONFIG"); config != "" { args = append(args, "--config", config) }
	if err := os.MkdirAll(filepath.Dir(a.logPath),0700);err != nil {a.lastError=err.Error();closeHandle.Call(job);closeHandle.Call(event);return}
	logFile,err := os.OpenFile(a.logPath,os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
	if err != nil {a.lastError=err.Error();closeHandle.Call(job);closeHandle.Call(event);return}
	defer logFile.Close()
	cmd := exec.Command(file, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | createNewProcessGroup, HideWindow: true}
	statusToken := hex.EncodeToString(token)
	statusFile := filepath.Join(os.TempDir(), "secretary-tray-status-"+statusToken+".json")
	cmd.Env = append(os.Environ(), "SECRETARY_NODE_SHUTDOWN_EVENT="+eventName, "SECRETARY_NODE_TRAY_STATUS_FILE="+statusFile)
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
	a.node, a.lastError, a.statusFile, a.live = n, "", statusFile, liveStatus{}
	go func() {
		_ = cmd.Wait()
		a.lock.Lock()
		if a.node == n { a.node = nil; a.statusFile = ""; a.live = liveStatus{} }
		closeHandle.Call(n.job)
		closeHandle.Call(n.event)
		close(n.done)
		_ = os.Remove(statusFile)
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

func readLiveStatus(path string, running bool, now time.Time) liveStatus {
 return traystatus.Read(path,running,now)
}

func (a *trayApp) refreshStatus(now time.Time) {
	a.lock.Lock()
	path, running := a.statusFile, a.node != nil
	a.lock.Unlock()
	next := readLiveStatus(path, running, now)
	a.lock.Lock()
	if a.statusFile == path && (a.live.Connected != next.Connected || !equalHarnesses(a.live.Harnesses, next.Harnesses)) {
		a.live = next
		if a.hwnd != 0 { postMessage.Call(a.hwnd, statusChanged, 0, 0) }
	}
	a.lock.Unlock()
}

func (a *trayApp) watchStatus() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.lock.Lock()
		exiting := a.exiting
		a.lock.Unlock()
		if exiting { return }
		a.refreshStatus(time.Now())
	}
}
func equalHarnesses(a,b []harnessRow) bool {
	if len(a)!=len(b){return false}
	for i:=range a {if a[i]!=b[i]{return false}}
	return true
}

func existingNodeForData(dataDir string)(bool,error) {
 absolute,err:=filepath.Abs(dataDir)
 if err!=nil{return false,err}
 digest:=sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absolute))))
 name:="Local\\SecretaryNodeData-"+hex.EncodeToString(digest[:16])
 ptr:=wide(name)
 handle,_,callErr:=k.NewProc("OpenMutexW").Call(0x00100000,0,uintptr(unsafe.Pointer(ptr)))
 if handle!=0 {closeHandle.Call(handle);return true,nil}
 if callErr==syscall.ERROR_FILE_NOT_FOUND {return false,nil}
 return false,fmt.Errorf("check Node process ownership: %w",callErr)
}
