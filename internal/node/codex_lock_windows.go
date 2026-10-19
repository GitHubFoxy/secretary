//go:build windows

package node

import (
 "os"
 "path/filepath"

 "golang.org/x/sys/windows"
)

func lockCodexPairScope(scope string) (func(), error) {
 inventory, err := CodexPairInventoryPath(scope)
 if err != nil { return nil, err }
 if err := os.MkdirAll(filepath.Dir(inventory), 0700); err != nil { return nil, err }
 file, err := os.OpenFile(filepath.Join(filepath.Dir(inventory), "codex-pair.lock"), os.O_CREATE|os.O_RDWR, 0600)
 if err != nil { return nil, err }
 overlapped := new(windows.Overlapped)
 err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
 if err != nil { file.Close(); return nil, err }
 return func() {
  _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
  _ = file.Close()
 }, nil
}
