//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

func setPrivateNodeUmask() { _ = syscall.Umask(0o077) }

func nodeShutdownSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }

func watchNodeShutdownEvent(stop func()) {}

func claimNodeInstance(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Node data directory for ownership: %w", err)
	}
	root, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve Node data directory for ownership: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(root, ".secretary-node.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Node ownership lock: %w", err)
	}
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("Node already active for data directory %s: %w", root, err)
		}
		return nil, fmt.Errorf("acquire Node ownership lock: %w", err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
			_ = lockFile.Close()
		})
	}, nil
}
