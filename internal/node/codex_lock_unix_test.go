//go:build !windows

package node

import (
 "os"
 "path/filepath"
 "testing"
)

func TestCodexPairScopeLockRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
 t.Setenv("HOME",t.TempDir())
 inventory,err:=CodexPairInventoryPath("node")
 if err!=nil {t.Fatal(err)}
 if err:=os.MkdirAll(filepath.Dir(inventory),0700);err!=nil {t.Fatal(err)}
 target:=filepath.Join(t.TempDir(),"target")
 canary:=[]byte("do not overwrite")
 if err:=os.WriteFile(target,canary,0600);err!=nil {t.Fatal(err)}
 lockPath:=filepath.Join(filepath.Dir(inventory),"codex-pair.lock")
 if err:=os.Symlink(target,lockPath);err!=nil {t.Fatal(err)}
 release,err:=lockCodexPairScope("node")
 if err==nil {
  if release!=nil {release()}
  t.Fatal("symlink lock accepted")
 }
 got,err:=os.ReadFile(target)
 if err!=nil {t.Fatal(err)}
 if string(got)!=string(canary) {t.Fatal("symlink target was modified")}
}

func TestCodexPairScopeLockRejectsConcurrentOwner(t *testing.T) {
 t.Setenv("HOME",t.TempDir())
 release,err:=lockCodexPairScope("node")
 if err!=nil {t.Fatal(err)}
 defer release()
 second,err:=lockCodexPairScope("node")
 if err==nil {
  if second!=nil {second()}
  t.Fatal("concurrent lock acquired")
 }
}
