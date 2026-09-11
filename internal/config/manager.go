package config

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

//go:embed defaults/*
var defaults embed.FS

type Manager struct {
	path     string
	mu       sync.RWMutex
	snapshot Snapshot
	record   func(previous, next Snapshot) error
}

func Open(path string) (*Manager, error) {
	if err := ensureDefault(path); err != nil {
		return nil, err
	}
	snapshot, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Manager{path: path, snapshot: snapshot}, nil
}
func (m *Manager) Path() string { return m.path }

func (m *Manager) OpenUserDocument() (*UserDocumentManager, error) {
	return OpenUserDocument(filepath.Dir(m.path))
}

// SetChangeRecorder installs a durable recorder. A failed recorder prevents
// the new configuration from becoming active.
func (m *Manager) SetChangeRecorder(record func(previous, next Snapshot) error) {
	m.mu.Lock()
	m.record = record
	m.mu.Unlock()
}

func (m *Manager) Snapshot() Snapshot { m.mu.RLock(); defer m.mu.RUnlock(); return m.snapshot }

// Reload leaves the active snapshot unchanged when parsing, validation, or
// durable recording fails.
func (m *Manager) Reload() (Snapshot, error) {
	next, err := Load(m.path)
	if err != nil {
		return Snapshot{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if next.Version == m.snapshot.Version {
		return m.snapshot, nil
	}
	if m.record != nil {
		if err := m.record(m.snapshot, next); err != nil {
			return Snapshot{}, fmt.Errorf("record config reload: %w", err)
		}
	}
	m.snapshot = next
	return next, nil
}

func ensureDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat config %s: %w", path, err)
	}
	base := filepath.Dir(path)
	if err := os.MkdirAll(filepath.Join(base, "profiles"), 0o700); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	for _, name := range []string{"config.toml", "secretary.md", "worker.md", "child-worker.md"} {
		from := "defaults/" + name
		to := path
		if name != "config.toml" {
			to = filepath.Join(base, "profiles", name)
		}
		body, err := fs.ReadFile(defaults, from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, body, 0o600); err != nil {
			return fmt.Errorf("write default %s: %w", to, err)
		}
	}
	return nil
}
