//go:build windows

package main

import (
 "os"
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
