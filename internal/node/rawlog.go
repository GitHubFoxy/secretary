package node

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// rotatingLog keeps a bounded number of raw ACP JSONL files per Worker.
type rotatingLog struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

func openRotatingLog(path string, maxBytes int64, maxFiles int) (*rotatingLog, error) {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	if maxFiles <= 0 {
		maxFiles = 5
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	return &rotatingLog{path: path, maxBytes: maxBytes, maxFiles: maxFiles, file: file, size: info.Size()}, nil
}
func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size > 0 && l.size+int64(len(p)) > l.maxBytes {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}
func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
func (l *rotatingLog) rotate() error {
	if err := l.file.Close(); err != nil {
		return err
	}
	for index := l.maxFiles - 1; index >= 1; index-- {
		from, to := fmt.Sprintf("%s.%d", l.path, index), fmt.Sprintf("%s.%d", l.path, index+1)
		_ = os.Remove(to)
		_ = os.Rename(from, to)
	}
	_ = os.Remove(l.path + ".1")
	if err := os.Rename(l.path, l.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.file, l.size = file, 0
	return nil
}

var _ io.WriteCloser = (*rotatingLog)(nil)
