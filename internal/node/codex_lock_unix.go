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
 fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
 if err != nil { return nil, fmt.Errorf("cannot safely open native pair lock: %w", err) }
 file := os.NewFile(uintptr(fd), lockPath)
 info, err := file.Stat()
 if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
  file.Close()
  return nil, fmt.Errorf("unsafe native pair lock file")
 }
 pathInfo, err := os.Lstat(lockPath)
 if err != nil || !pathInfo.Mode().IsRegular() || !os.SameFile(info, pathInfo) {
  file.Close()
  return nil, fmt.Errorf("native pair lock path changed during open")
 }
 if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
  file.Close()
  return nil, fmt.Errorf("native pair scope %s upgrade already in progress: %w",scope,err)
 }
 return func(){ _=syscall.Flock(int(file.Fd()),syscall.LOCK_UN); _=file.Close() },nil
}
