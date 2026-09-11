package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const maxUserMarkdownBytes = 1 << 20

// UserDocument is the validated external user.md snapshot.
type UserDocument struct {
	Path     string `json:"path"`
	Revision int64  `json:"revision"`
	Content  string `json:"content"`
}

type UserDocumentManager struct {
	path     string
	revision string
	mu       sync.RWMutex
	snapshot UserDocument
}

func UserDocumentPath(dataDir string) string {
	if filepath.Base(dataDir) == "user.md" {
		return dataDir
	}
	return filepath.Join(dataDir, "user.md")
}

func OpenUserDocument(dataDir string) (*UserDocumentManager, error) {
	path := UserDocumentPath(dataDir)
	manager := &UserDocumentManager{path: path, revision: path + ".revision"}
	snapshot, err := manager.read()
	if err != nil {
		return nil, err
	}
	manager.snapshot = snapshot
	return manager, nil
}

func (m *UserDocumentManager) Path() string { return m.path }
func (m *UserDocumentManager) Snapshot() UserDocument {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}
func (m *UserDocumentManager) Load() (UserDocument, error)                { return m.Reload() }
func (m *UserDocumentManager) Write(content string) (UserDocument, error) { return m.Save(content) }

func ValidateUserMarkdown(content string) error {
	if !utf8.ValidString(content) {
		return fmt.Errorf("%w: user.md must be valid UTF-8", ErrInvalid)
	}
	if strings.IndexByte(content, 0) >= 0 {
		return fmt.Errorf("%w: user.md contains NUL", ErrInvalid)
	}
	if len(content) > maxUserMarkdownBytes {
		return fmt.Errorf("%w: user.md is too large", ErrInvalid)
	}
	return nil
}

func (m *UserDocumentManager) read() (UserDocument, error) {
	content, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		content = []byte{}
	} else if err != nil {
		return UserDocument{}, fmt.Errorf("read user.md: %w", err)
	}
	if err := ValidateUserMarkdown(string(content)); err != nil {
		return UserDocument{}, err
	}
	revision := int64(1)
	if data, err := os.ReadFile(m.revision); err == nil {
		parsed, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		if parseErr != nil || parsed < 1 {
			return UserDocument{}, fmt.Errorf("%w: invalid user.md revision", ErrInvalid)
		}
		revision = parsed
	} else if !errors.Is(err, os.ErrNotExist) {
		return UserDocument{}, fmt.Errorf("read user.md revision: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return UserDocument{}, err
	}
	if _, err := os.Stat(m.path); errors.Is(err, os.ErrNotExist) {
		if err := atomicWriteFile(m.path, content); err != nil {
			return UserDocument{}, err
		}
	}
	if _, err := os.Stat(m.revision); errors.Is(err, os.ErrNotExist) {
		if err := atomicWriteFile(m.revision, []byte(strconv.FormatInt(revision, 10))); err != nil {
			return UserDocument{}, err
		}
	}
	return UserDocument{Path: m.path, Revision: revision, Content: string(content)}, nil
}

func (m *UserDocumentManager) Reload() (UserDocument, error) {
	next, err := m.read()
	if err != nil {
		return UserDocument{}, err
	}
	m.mu.Lock()
	m.snapshot = next
	m.mu.Unlock()
	return next, nil
}

func (m *UserDocumentManager) Save(content string) (UserDocument, error) {
	if err := ValidateUserMarkdown(content); err != nil {
		return UserDocument{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.snapshot
	if err := atomicWriteFile(m.path, []byte(content)); err != nil {
		return UserDocument{}, err
	}
	next := UserDocument{Path: m.path, Revision: previous.Revision + 1, Content: content}
	if err := atomicWriteFile(m.revision, []byte(strconv.FormatInt(next.Revision, 10))); err != nil {
		_ = atomicWriteFile(m.path, []byte(previous.Content))
		return UserDocument{}, err
	}
	m.snapshot = next
	return next, nil
}

func atomicWriteFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".atomic.*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
