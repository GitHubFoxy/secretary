//go:build windows

package main

import (
 "os"
 "crypto/sha256"
 "encoding/hex"
 "unsafe"
 "path/filepath"
 "strings"
 "testing"
 "golang.org/x/sys/windows/registry"
)
func TestW03LoginPersistsAndDisables(t *testing.T) {
 dir:=t.TempDir()
 app=&trayApp{dataDir:dir}
 original,oldErr:=registry.OpenKey(registry.CURRENT_USER,loginKey,registry.QUERY_VALUE)
 var previous string
 if oldErr==nil {previous,_,_=original.GetStringValue(loginName);original.Close()}
 t.Cleanup(func(){
  k,_,err:=registry.CreateKey(registry.CURRENT_USER,loginKey,registry.SET_VALUE)
  if err==nil {defer k.Close();if previous!=""{_ = k.SetStringValue(loginName,previous)}else{_ = k.DeleteValue(loginName)}}
 })
 if err:=setLogin(true);err!=nil{t.Fatal(err)}
 active,err:=loginEnabled()
 if err!=nil||!active{t.Fatalf("login not registered: %v",err)}
 app=&trayApp{dataDir:dir,login:active}
 rows:=app.menuRows()
 if rows[len(rows)-3].label!="Launch at Login: On"{t.Fatalf("menu: %+v",rows)}
 if err:=setLogin(false);err!=nil{t.Fatal(err)}
 active,err=loginEnabled()
 if err!=nil||active{t.Fatalf("login not removed: %v",err)}
}
func TestW03LoginCommandEscaping(t *testing.T){
 value:=loginCommand(`C:\Program Files\Secretary\secretary-tray.exe`,`C:\Users\Demo User\.secretary\node`)
 if !strings.Contains(value,`"C:\Program Files\Secretary\secretary-tray.exe"`) || !strings.Contains(value,`"C:\Users\Demo User\.secretary\node"`) {t.Fatal(value)}
}
func TestW03LogsPathMatchesNodeData(t *testing.T){
 root:=t.TempDir(); app=&trayApp{dataDir:root,logPath:filepath.Join(root,"logs","node.log")}
 if err:=os.MkdirAll(filepath.Dir(app.logPath),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(app.logPath,[]byte("node boot"),0600);err!=nil{t.Fatal(err)}
 value,err:=os.ReadFile(app.logPath)
 if err!=nil||string(value)!="node boot"{t.Fatalf("log path: %s %v",value,err)}
 rows:=app.menuRows()
 if rows[len(rows)-2].action!="logs"{t.Fatalf("missing logs action: %+v",rows)}
}

func TestW03OpenLogsInvokesExplorerBoundary(t *testing.T) {
 root:=t.TempDir()
 path:=filepath.Join(root,"logs","node.log")
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("node started"),0600);err!=nil{t.Fatal(err)}
 calls:=0
 openNodeLogs(path,os.Stat,func(got string)error{calls++;if got!=path{t.Fatalf("path %q",got)};return nil},func(title,msg string){t.Fatalf("unexpected error: %s %s",title,msg)})
 if calls!=1{t.Fatalf("explorer calls %d",calls)}
}
func TestW03OpenLogsActionableErrors(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"logs","node.log")
 launched:=false
 var errorText string
 report:=func(_,message string){errorText=message}
 openNodeLogs(path,os.Stat,func(string)error{launched=true;return nil},report)
 if launched||!strings.Contains(errorText,"Start Secretary Node"){t.Fatalf("missing file: launched=%v message=%q",launched,errorText)}
 if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,[]byte("node started"),0600);err!=nil{t.Fatal(err)}
 errorText=""
 openNodeLogs(path,os.Stat,func(string)error{return os.ErrPermission},report)
 if !strings.Contains(errorText,"File Explorer")||!strings.Contains(errorText,path){t.Fatal(errorText)}
}

func TestW03HeadlessNodeOwnershipPreventsTrayDuplicate(t *testing.T) {
 root:=t.TempDir()
 absolute,err:=filepath.Abs(root)
 if err!=nil{t.Fatal(err)}
 digest:=sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absolute))))
 name:=wide("Local\\SecretaryNodeData-"+hex.EncodeToString(digest[:16]))
 handle,_,err:=createMutex.Call(0,1,uintptr(unsafe.Pointer(name)))
 if handle==0{t.Fatalf("claim simulated headless Node: %v",err)}
 defer closeHandle.Call(handle)
 active,err:=existingNodeForData(root)
 if err!=nil||!active{t.Fatalf("headless Node not detected: %v %v",active,err)}
 a:=&trayApp{dataDir:root}
 a.start()
 if a.node!=nil||!strings.Contains(a.lastError,"headless Node"){t.Fatalf("duplicate Node attempt: %v %s",a.node,a.lastError)}
}
func TestW03OwnershipReleasedAfterHeadlessExit(t *testing.T) {
 root:=t.TempDir()
 absolute,err:=filepath.Abs(root)
 if err!=nil{t.Fatal(err)}
 digest:=sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absolute))))
 name:=wide("Local\\SecretaryNodeData-"+hex.EncodeToString(digest[:16]))
 handle,_,err:=createMutex.Call(0,1,uintptr(unsafe.Pointer(name)))
 if handle==0{t.Fatalf("create mutex: %v",err)}
 closeHandle.Call(handle)
 active,err:=existingNodeForData(root)
 if err!=nil||active{t.Fatalf("stale Node ownership: %v %v",active,err)}
}
