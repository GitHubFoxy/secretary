package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Memory is deliberately limited to two owner-editable Markdown files. The
// historical user.md remains an independent, readable legacy document.
const (
	MemoryFacts       = "memory"
	MemoryPreferences = "preferences"
)

var (
	ErrMemoryRevisionConflict = errors.New("core: memory revision conflict")
	ErrInvalidMemory          = errors.New("core: invalid memory document or request")
	ErrMemoryWriteNotRequested = errors.New("core: owner did not explicitly request a memory change")
)

func MemoryDocumentPath(userPath, kind string) (string, error) {
	name := ""
	switch kind {
	case MemoryFacts:
		name = "memory.md"
	case MemoryPreferences:
		name = "user_preferences.md"
	default:
		return "", fmt.Errorf("%w: unknown kind %q", ErrInvalidMemory, kind)
	}
	if userPath == "" {
		userPath = "user.md"
	}
	path, err := filepath.Abs(filepath.Join(filepath.Dir(userPath), name))
	if err != nil {
		return "", err
	}
	return path, nil
}

func validateMemoryContent(kind, content string) error {
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) || len(content) > maxUserDocumentBytes {
		return fmt.Errorf("%w: %s.md must be valid UTF-8 without NUL and at most 1 MiB", ErrInvalidMemory, kind)
	}
	return nil
}

// SQLite BEGIN IMMEDIATE is a cross-process writer lock. Keep the lock
// across revision checking, the file rename and the SQLite commit. In-process
// mutexes alone do not protect a server and a separate MCP process.
func (s *Store) withMemoryTransaction(ctx context.Context, operation func(*sql.Conn) (UserDocument, func() error, error)) (UserDocument, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return UserDocument{}, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return UserDocument{}, err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return UserDocument{}, fmt.Errorf("lock memory revision: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	document, revert, err := operation(conn)
	if err != nil {
		return UserDocument{}, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		// A failed COMMIT must not leave the file ahead of SQLite. Revert only
		// while this transaction still owns the SQLite writer lock.
		if revert != nil {
			if restoreErr := revert(); restoreErr != nil {
				return UserDocument{}, fmt.Errorf("commit memory: %v; restore previous file: %w", err, restoreErr)
			}
		}
		return UserDocument{}, fmt.Errorf("commit memory revision: %w", err)
	}
	committed = true
	return document, nil
}

type memorySQL interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// ReadMemoryDocument imports valid manual Markdown edits under the same
// SQLite lock used for writes. Corruption and read failures remain visible.
func (s *Store) ReadMemoryDocument(ctx context.Context, userPath, kind string) (UserDocument, error) {
	s.userDocumentMu.Lock()
	defer s.userDocumentMu.Unlock()
	return s.withMemoryTransaction(ctx, func(conn *sql.Conn) (UserDocument, func() error, error) {
		document, err := s.readMemoryDocumentUnlocked(ctx, conn, userPath, kind)
		return document, nil, err
	})
}

func (s *Store) readMemoryDocumentUnlocked(ctx context.Context, q memorySQL, userPath, kind string) (UserDocument, error) {
	path, err := MemoryDocumentPath(userPath, kind)
	if err != nil {
		return UserDocument{}, err
	}
	var current UserDocument
	err = q.QueryRowContext(ctx, `SELECT path, revision, content, updated_at FROM secretary_memory_documents WHERE kind = ?`, kind).
		Scan(&current.Path, &current.Revision, &current.Content, newTimestampScanner(&current.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		raw, readErr := os.ReadFile(path)
		if errors.Is(readErr, os.ErrNotExist) {
			raw = []byte{}
			if err := atomicWrite(path, raw); err != nil {
				return UserDocument{}, fmt.Errorf("initialize %s: %w", path, err)
			}
		} else if readErr != nil {
			return UserDocument{}, fmt.Errorf("read %s: %w", path, readErr)
		}
		if err := validateMemoryContent(kind, string(raw)); err != nil {
			return UserDocument{}, err
		}
		current = UserDocument{Path: path, Revision: 1, Content: string(raw), UpdatedAt: s.now()}
		_, err = q.ExecContext(ctx, `INSERT INTO secretary_memory_documents(kind, path, revision, content, updated_at) VALUES(?, ?, ?, ?, ?)`,
			kind, path, current.Revision, current.Content, timestamp(current.UpdatedAt))
		if err != nil {
			return UserDocument{}, fmt.Errorf("initialize memory revision: %w", err)
		}
		return current, nil
	}
	if err != nil {
		return UserDocument{}, fmt.Errorf("read memory revision: %w", err)
	}
	if current.Path != path {
		return UserDocument{}, fmt.Errorf("%w: %s is already bound to %s", ErrInvalidMemory, kind, current.Path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return UserDocument{}, fmt.Errorf("read %s: %w", path, err)
	}
	if err := validateMemoryContent(kind, string(raw)); err != nil {
		return UserDocument{}, err
	}
	if string(raw) != current.Content {
		now := s.now()
		result, err := q.ExecContext(ctx, `UPDATE secretary_memory_documents SET content = ?, revision = revision + 1, updated_at = ? WHERE kind = ? AND revision = ?`,
			string(raw), timestamp(now), kind, current.Revision)
		if err != nil {
			return UserDocument{}, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return UserDocument{}, err
		}
		if affected != 1 {
			return UserDocument{}, ErrMemoryRevisionConflict
		}
		current.Content, current.Revision, current.UpdatedAt = string(raw), current.Revision+1, now
	}
	return current, nil
}

// SaveMemoryDocumentIfRevision requires the latest revision. A second Store or
// process cannot check a stale revision, write, and undo the first one's file.
func (s *Store) SaveMemoryDocumentIfRevision(ctx context.Context, userPath, kind, content string, expected int64) (UserDocument, error) {
	s.userDocumentMu.Lock()
	defer s.userDocumentMu.Unlock()
	return s.withMemoryTransaction(ctx, func(conn *sql.Conn) (UserDocument, func() error, error) {
		return s.saveMemoryDocumentUnlocked(ctx, conn, userPath, kind, content, expected)
	})
}

func (s *Store) saveMemoryDocumentUnlocked(ctx context.Context, q memorySQL, userPath, kind, content string, expected int64) (UserDocument, func() error, error) {
	if err := validateMemoryContent(kind, content); err != nil {
		return UserDocument{}, nil, err
	}
	if expected < 1 {
		return UserDocument{}, nil, fmt.Errorf("%w: expected_revision is required", ErrInvalidMemory)
	}
	previous, err := s.readMemoryDocumentUnlocked(ctx, q, userPath, kind)
	if err != nil {
		return UserDocument{}, nil, err
	}
	if previous.Revision != expected {
		return UserDocument{}, nil, ErrMemoryRevisionConflict
	}
	if previous.Content == content {
		return previous, nil, nil
	}
	now := s.now()
	result, err := q.ExecContext(ctx, `UPDATE secretary_memory_documents SET revision = ?, content = ?, updated_at = ? WHERE kind = ? AND revision = ?`,
		previous.Revision+1, content, timestamp(now), kind, previous.Revision)
	if err != nil {
		return UserDocument{}, nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return UserDocument{}, nil, err
	}
	if affected != 1 {
		return UserDocument{}, nil, ErrMemoryRevisionConflict
	}
	revert := func() error { return atomicWrite(previous.Path, []byte(previous.Content)) }
	if err := atomicWrite(previous.Path, []byte(content)); err != nil {
		// atomicWrite can fail on directory sync after the rename. Restore
		// before releasing the writer reservation in that case.
		raw, readErr := os.ReadFile(previous.Path)
		if readErr == nil && string(raw) == content {
			if restoreErr := revert(); restoreErr != nil {
				return UserDocument{}, nil, fmt.Errorf("write memory: %v; restore file: %w", err, restoreErr)
			}
		}
		return UserDocument{}, nil, fmt.Errorf("write %s: %w", previous.Path, err)
	}
	return UserDocument{Path: previous.Path, Revision: previous.Revision+1, Content: content, UpdatedAt: now}, revert, nil
}

// ExplicitMemoryRequest rejects normal conversation as a write authorization.
// An explicit "remember" request is required in the originating owner turn.
func ExplicitMemoryRequest(input string) bool {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" || strings.ContainsAny(text, "\\n\\r") {
		return false
	}
	// Quoted/translated/negated examples do not authorize a durable write.
	if strings.ContainsAny(text, "\"“”«»") {
		return false
	}
	for _, ambiguous := range []string{
		"translate", "переведи", "перевести", "перевод", "quote", "цитат",
		"do not remember", "don't remember", "do not save", "don't save",
		"не запоминай", "не сохраняй", "not a request", "не просьба",
		"instead", "вместо этого", "или лучше", "or should", "just an example",
		"просто пример", "hypothetical", "гипотетическ",
	} {
		if strings.Contains(text, ambiguous) {
			return false
		}
	}
	if strings.ContainsAny(text, "?;—") || strings.Contains(text, " - ") {
		return false
	}
	for _, prefix := range []string{"пожалуйста, ", "пожалуйста ", "please, ", "please "} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}
	for _, prefix := range []string{
		"запомни ", "запомни,", "запиши в память ", "сохрани в память ", "сохрани как предпочтение ",
		"на будущее запомни ", "хочу, чтобы ты запомнил ", "я хочу, чтобы ты запомнил ",
		"remember ", "please remember ", "save to memory ", "save in memory ",
		"can you remember ", "could you remember ", "remember that ",
		"/remember ", "/memory ",
	} {
		if strings.HasPrefix(text, prefix) {
			body := strings.TrimSpace(strings.TrimPrefix(text, prefix))
			return body != "" && !strings.HasPrefix(body, "not ") && !strings.HasPrefix(body, "не ")
		}
	}
	return false
}

// RememberOwnerMemory is the sole conversational mutation path. It checks the
// active owner-origin token and the actual server-stored owner instruction.
func (s *Store) RememberOwnerMemory(ctx context.Context, userPath, kind, entry string, expected int64, personID, capability, turnID, inputID string) (UserDocument, error) {
	if err := s.ValidateSecretaryOrigin(ctx, personID, capability, turnID, inputID); err != nil {
		return UserDocument{}, err
	}
	turn, err := s.SecretaryTurn(ctx, turnID)
	if err != nil {
		return UserDocument{}, err
	}
	if turn.InputID != inputID || !ExplicitMemoryRequest(turn.Input) {
		return UserDocument{}, ErrMemoryWriteNotRequested
	}
	entry = strings.TrimSpace(entry)
	if entry == "" || strings.ContainsRune(entry, 0) {
		return UserDocument{}, fmt.Errorf("%w: nonempty Markdown entry is required", ErrInvalidMemory)
	}
	s.userDocumentMu.Lock()
	defer s.userDocumentMu.Unlock()
	return s.withMemoryTransaction(ctx, func(conn *sql.Conn) (UserDocument, func() error, error) {
		previous, err := s.readMemoryDocumentUnlocked(ctx, conn, userPath, kind)
		if err != nil {
			return UserDocument{}, nil, err
		}
		if expected < 1 || previous.Revision != expected {
			return UserDocument{}, nil, ErrMemoryRevisionConflict
		}
		next := previous.Content
		if next != "" && !strings.HasSuffix(next, "\n") {
			next += "\n"
		}
		next += "- " + strings.ReplaceAll(entry, "\n", "\n  ") + "\n"
		return s.saveMemoryDocumentUnlocked(ctx, conn, userPath, kind, next, expected)
	})
}
