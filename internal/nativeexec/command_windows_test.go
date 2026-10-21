//go:build windows

package nativeexec

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestDirectWindowsCommandShim(t *testing.T) {
 path:=filepath.Join(t.TempDir(),"codex shim.cmd")
 if err:=os.WriteFile(path,[]byte("@echo off\r\necho shim-%~1\r\n"),0600);err!=nil{t.Fatal(err)}
 cmd,err:=CommandContext(context.Background(),path,"ready")
 if err!=nil{t.Fatal(err)}
 output,err:=cmd.CombinedOutput()
 if err!=nil{t.Fatalf("Windows cmd shim: %v: %s",err,output)}
 if !strings.Contains(string(output),"shim-ready") {t.Fatalf("Windows shim arguments lost: %s",output)}
}
