//go:build !windows

package node

import (
 "fmt"
 "os"
 "path/filepath"
 "syscall"
)

func lockCodexPairScope(scope string) (func(), error) {
 location, err := CodexPairInventoryPath(scope)
 if err != nil { return nil, err }
 if err := os.MkdirAll(filepath.Dir(location), 0700); err != nil { return nil, err }
 lockPath := filepath.Join(filepath.Dir(location), "codex-pair.lock")
 file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
 if err != nil { return nil, err }
 info, err := file.Stat()
 if err != nil || !info.Mode().IsRegular() { file.Close(); return nil, fmt.Errorf("unsafe native pair lock file") }
 if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
  file.Close()
  return nil, fmt.Errorf("native pair scope %s upgrade already in progress: %w",scope,err)
 }
 return func(){ _=syscall.Flock(int(file.Fd()),syscall.LOCK_UN); _=file.Close() },nil
}
