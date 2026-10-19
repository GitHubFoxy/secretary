//go:build windows

package main

import (
 "errors"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "syscall"
 "unsafe"

 "golang.org/x/sys/windows/registry"
)

const loginKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const loginName = "Secretary Node"

func defaultTrayDataDir() string {
 home,err:=os.UserHomeDir()
 if err!=nil{return ".secretary-node"}
 return filepath.Join(home,".secretary","node")
}
func loginCommand(exe,dataDir string) string {
 return syscall.EscapeArg(exe)+" --data-dir "+syscall.EscapeArg(dataDir)
}
func loginEnabled() (bool,error) {
 k,err:=registry.OpenKey(registry.CURRENT_USER,loginKey,registry.QUERY_VALUE)
 if errors.Is(err,syscall.ERROR_FILE_NOT_FOUND) {return false,nil}
 if err!=nil{return false,err}
 defer k.Close()
 value,_,err:=k.GetStringValue(loginName)
 if errors.Is(err,syscall.ERROR_FILE_NOT_FOUND) {return false,nil}
 if err!=nil{return false,err}
 exe,err:=os.Executable()
 if err!=nil{return false,err}
 return strings.EqualFold(value,loginCommand(exe,app.dataDir)),nil
}
func setLogin(enabled bool) error {
 if !enabled {
  k,err:=registry.OpenKey(registry.CURRENT_USER,loginKey,registry.SET_VALUE)
  if errors.Is(err,syscall.ERROR_FILE_NOT_FOUND){return nil}
  if err!=nil{return err}
  defer k.Close()
  err=k.DeleteValue(loginName)
  if errors.Is(err,syscall.ERROR_FILE_NOT_FOUND){return nil}
  return err
 }
 exe,err:=os.Executable()
 if err!=nil{return err}
 k,_,err:=registry.CreateKey(registry.CURRENT_USER,loginKey,registry.SET_VALUE)
 if err!=nil{return err}
 defer k.Close()
 return k.SetStringValue(loginName,loginCommand(exe,app.dataDir))
}
func (a *trayApp) menuRows() []menuRow {
 rows:=buildMenu(a.node!=nil,a.lastError!="",a.live)
 for i:=range rows {if rows[i].action=="login" && a.login {rows[i].label="Launch at Login: On"}}
 return rows
}
func (a *trayApp) toggleLogin() {
 a.lock.Lock()
 next:=!a.login
 a.lock.Unlock()
 err:=setLogin(next)
 a.lock.Lock()
 if err!=nil {a.lastError="Could not change Launch at Login: "+err.Error()} else {a.login=next;a.lastError=""}
 a.lock.Unlock()
 postMessage.Call(a.hwnd,statusChanged,0,0)
 if err!=nil {showTrayError("Launch at Login",err.Error()+"\nCheck Windows startup app permissions.") }
}
func (a *trayApp) openLogs() {
 path:=a.logPath
 if _,err:=os.Stat(path);err!=nil {
  showTrayError("Open Logs",fmt.Sprintf("Node log is not available at %s\nStart Secretary Node and retry. %v",path,err))
  return
 }
 command:=exec.Command("explorer.exe","/select,",path)
 if err:=command.Start();err!=nil {showTrayError("Open Logs",fmt.Sprintf("Cannot open %s: %v\nOpen this path in File Explorer.",path,err))}
}
func showTrayError(title,message string) {
 proc:=u.NewProc("MessageBoxW")
 proc.Call(0,uintptr(unsafe.Pointer(wide(message))),uintptr(unsafe.Pointer(wide(title))),0x10)
}
