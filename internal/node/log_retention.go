package node

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

type LogRetention struct {
	MaxAge   time.Duration
	MaxBytes int64
}

// PruneLogs deletes old files first, then oldest files until the size cap holds.
func PruneLogs(dir string, retention LogRetention) error {
	if retention.MaxAge <= 0 {
		retention.MaxAge = 30 * 24 * time.Hour
	}
	if retention.MaxBytes <= 0 {
		retention.MaxBytes = 1 << 30
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type file struct {
		path string
		info os.FileInfo
	}
	files := make([]file, 0, len(entries))
	cutoff := time.Now().Add(-retention.MaxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		path := filepath.Join(dir, entry.Name())
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		files = append(files, file{path, info})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().Before(files[j].info.ModTime()) })
	var total int64
	for _, file := range files {
		total += file.info.Size()
	}
	for _, file := range files {
		if total <= retention.MaxBytes {
			break
		}
		if err := os.Remove(file.path); err != nil {
			return err
		}
		total -= file.info.Size()
	}
	return nil
}
