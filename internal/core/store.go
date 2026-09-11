package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("core: not found")
	ErrInvalidTransition = errors.New("core: invalid state transition")
)

type Store struct {
	db  *sql.DB
	now func() time.Time

	observerMu sync.RWMutex
	observer   func(ConversationEntry)

	idempotencyMu sync.Mutex
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure sqlite: %w", err)
	}
	if err := backupBeforeMigration(ctx, db, dsn); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

// backupBeforeMigration keeps a recoverable copy before any schema work. The
// SQLite VACUUM INTO snapshot includes committed WAL pages and produces a
// standalone backup instead of copying only the main database file.
func backupBeforeMigration(ctx context.Context, db *sql.DB, dsn string) error {
	databasePath, ok := sqliteDatabasePath(dsn)
	if !ok {
		return nil
	}
	info, err := os.Stat(databasePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat sqlite database for backup: %w", err)
	}
	if info.IsDir() || info.Size() == 0 {
		return nil
	}

	backupPath := databasePath + ".backup"
	temporaryPath := backupPath + ".tmp"
	if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale sqlite backup: %w", err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := db.ExecContext(ctx, `VACUUM INTO `+sqliteStringLiteral(temporaryPath)); err != nil {
		return fmt.Errorf("create sqlite backup: %w", err)
	}
	if err := os.Chmod(temporaryPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("set sqlite backup permissions: %w", err)
	}
	file, err := os.OpenFile(temporaryPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open sqlite backup for sync: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync sqlite backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close sqlite backup: %w", err)
	}
	if err := os.Rename(temporaryPath, backupPath); err != nil {
		return fmt.Errorf("install sqlite backup: %w", err)
	}
	removeTemporary = false
	return nil
}

func sqliteDatabasePath(dsn string) (string, bool) {
	if dsn == "" || dsn == ":memory:" {
		return "", false
	}
	if strings.HasPrefix(dsn, "file:") {
		parsed, err := url.Parse(dsn)
		if err != nil || parsed.Query().Get("mode") == "memory" {
			return "", false
		}
		path := parsed.Path
		if path == "" {
			path = parsed.Opaque
		}
		if path == "" || path == ":memory:" {
			return "", false
		}
		return path, true
	}
	path := dsn
	if query := strings.IndexByte(path, '?'); query >= 0 {
		if strings.Contains(path[query+1:], "mode=memory") {
			return "", false
		}
		path = path[:query]
	}
	return path, path != ""
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// RecordConfigVersion keeps the exact compiled snapshot that a Worker binding references.
func (s *Store) RecordConfigVersion(ctx context.Context, version, sourcePath, compiledJSON string) error {
	if version == "" || compiledJSON == "" {
		return errors.New("core: config version and compiled snapshot are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO config_versions(version, source_path, compiled_json, created_at) VALUES(?, ?, ?, ?)`, version, sourcePath, compiledJSON, timestamp(s.now()))
	return err
}

func (s *Store) RecordConfigChange(ctx context.Context, previousVersion, nextVersion, sourcePath, compiledJSON, diffJSON string) error {
	if previousVersion == "" || nextVersion == "" || compiledJSON == "" || diffJSON == "" {
		return errors.New("core: complete config change is required")
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO config_versions(version, source_path, compiled_json, created_at) VALUES(?, ?, ?, ?)`, nextVersion, sourcePath, compiledJSON, timestamp(s.now())); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO config_events(id, previous_version, next_version, diff_json, created_at) VALUES(?, ?, ?, ?, ?)`, newID("cev"), previousVersion, nextVersion, diffJSON, timestamp(s.now()))
		return err
	})
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	if key == "" {
		return "", false, errors.New("core: setting key is required")
	}
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	if key == "" {
		return errors.New("core: setting key is required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, timestamp(s.now()))
	return err
}

func (s *Store) ConfigVersion(ctx context.Context, version string) (string, error) {
	if version == "" {
		return "", errors.New("core: config version is required")
	}
	var compiled string
	err := s.db.QueryRowContext(ctx, `SELECT compiled_json FROM config_versions WHERE version = ?`, version).Scan(&compiled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return compiled, err
}

// RecoverInterrupted preserves the legacy Phase 3 recovery contract. It does
// not inspect Phase 4 Attempts because a server cannot prove the state of a
// remote Node without an explicit resolver.
func (s *Store) RecoverInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attempts SET state = ?, updated_at = ? WHERE state IN (?, ?)`, AttemptInterrupted, timestamp(s.now()), AttemptStarting, AttemptActive)
	return err
}

// RecoverPhase4Attempts asks an explicit Node/harness resolver about every
// starting or active Phase 4 Attempt. A nil resolver is a deliberate no-op,
// not a claim that remote execution is dead. Only an unknown probe result is
// converted to an interrupted Attempt and its final Result.
func (s *Store) RecoverPhase4Attempts(ctx context.Context, resolver Phase4AttemptRecoveryResolver) error {
	if resolver == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM phase4_attempts WHERE state IN (?, ?)`, AttemptStarting, AttemptActive)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		attempt, err := s.Phase4Attempt(ctx, id)
		if err != nil {
			return err
		}
		decision, err := resolver.ResolvePhase4Attempt(ctx, attempt)
		if err != nil {
			return err
		}
		switch decision {
		case Phase4RecoveryAlive:
			continue
		case Phase4RecoveryUnknown:
			if _, _, _, err := s.InterruptPhase4Attempt(ctx, id, "runtime_execution_unknown", "Attempt execution could not be proven by the recovery resolver"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("core: invalid Phase 4 recovery decision %q", decision)
		}
	}
	return nil
}

func (s *Store) SetEntryObserver(observer func(ConversationEntry)) {
	s.observerMu.Lock()
	s.observer = observer
	s.observerMu.Unlock()
}

func (s *Store) notifyEntry(entry ConversationEntry) {
	s.observerMu.RLock()
	observer := s.observer
	s.observerMu.RUnlock()
	if observer != nil {
		observer(entry)
	}
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS persons (
  id TEXT PRIMARY KEY,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversations (
  id TEXT PRIMARY KEY,
  person_id TEXT NOT NULL UNIQUE REFERENCES persons(id),
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conversation_entries (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  seq INTEGER NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(conversation_id, seq)
);
CREATE TABLE IF NOT EXISTS inbound_messages (
  adapter_id TEXT NOT NULL,
  external_message_id TEXT NOT NULL,
  entry_id TEXT NOT NULL REFERENCES conversation_entries(id),
  PRIMARY KEY(adapter_id, external_message_id)
);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  text TEXT NOT NULL,
  state TEXT NOT NULL,
  parent_task_id TEXT NOT NULL DEFAULT '',
  parent_attempt_id TEXT NOT NULL DEFAULT '',
  child_index INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
-- Phase 3 tasks remain legacy migration data. New product state uses these
-- Worker-first tables and never creates a Task row.
CREATE TABLE IF NOT EXISTS workers (
  id TEXT PRIMARY KEY,
  worker_ref TEXT NOT NULL UNIQUE,
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  title TEXT NOT NULL,
  intent TEXT NOT NULL,
  project_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  harness_instance_id TEXT NOT NULL,
  policy_snapshot TEXT NOT NULL,
  status TEXT NOT NULL,
  current_turn_id TEXT,
  last_result_summary TEXT NOT NULL DEFAULT '',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  closed_at TEXT
);
CREATE TABLE IF NOT EXISTS turns (
  id TEXT PRIMARY KEY,
  worker_id TEXT NOT NULL REFERENCES workers(id),
  input TEXT NOT NULL,
  normalized_intent TEXT NOT NULL DEFAULT '',
  context_snapshot TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  current_attempt_id TEXT,
  result_id TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS turns_one_active_per_worker
  ON turns(worker_id) WHERE state IN ('queued', 'starting', 'active', 'waiting_approval', 'needs_input');
CREATE TABLE IF NOT EXISTS worker_bindings (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
  worker_ref TEXT NOT NULL UNIQUE,
  node_id TEXT NOT NULL,
  runtime_session_id TEXT NOT NULL,
  workspace TEXT NOT NULL DEFAULT '',
  parent_binding_id TEXT NOT NULL DEFAULT '',
  parent_attempt_id TEXT NOT NULL DEFAULT '',
  profile_version TEXT NOT NULL DEFAULT '',
  profile_name TEXT NOT NULL DEFAULT '',
  profile_hash TEXT NOT NULL DEFAULT '',
  runtime TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  reasoning TEXT NOT NULL DEFAULT '',
  allow_tools TEXT NOT NULL DEFAULT '',
  profile_delivery TEXT NOT NULL DEFAULT '',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS attempts (
  id TEXT PRIMARY KEY,
  worker_binding_id TEXT NOT NULL REFERENCES worker_bindings(id),
  number INTEGER NOT NULL,
  state TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(worker_binding_id, number)
);
CREATE TABLE IF NOT EXISTS results (
  id TEXT PRIMARY KEY,
  attempt_id TEXT NOT NULL UNIQUE REFERENCES attempts(id),
  status TEXT NOT NULL,
  summary TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS secretary_capabilities (
  id TEXT PRIMARY KEY,
  person_id TEXT NOT NULL REFERENCES persons(id),
  token_hash TEXT NOT NULL UNIQUE,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS worker_capabilities (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id),
  worker_ref TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL UNIQUE,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS web_sessions (
  id TEXT PRIMARY KEY,
  person_id TEXT NOT NULL REFERENCES persons(id),
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS config_versions (
  version TEXT PRIMARY KEY,
  source_path TEXT NOT NULL,
  compiled_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS config_events (
  id TEXT PRIMARY KEY,
  previous_version TEXT NOT NULL,
  next_version TEXT NOT NULL REFERENCES config_versions(version),
  diff_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id TEXT PRIMARY KEY,
  seq INTEGER,
  kind TEXT NOT NULL,
  aggregate_type TEXT NOT NULL DEFAULT '',
  aggregate_id TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  correlation_id TEXT NOT NULL DEFAULT '',
  causation_id TEXT NOT NULL DEFAULT '',
  worker_ref TEXT NOT NULL DEFAULT '',
  attempt_id TEXT NOT NULL DEFAULT '',
  runtime_session_id TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS event_sequence (
  id INTEGER PRIMARY KEY CHECK(id = 1),
  next_seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS idempotency_records (
  operation TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  outcome_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(operation, idempotency_key)
);
CREATE TABLE IF NOT EXISTS deliveries (
  id TEXT PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES events(id),
  entry_id TEXT REFERENCES conversation_entries(id),
  target TEXT NOT NULL,
  idempotency_key TEXT NOT NULL UNIQUE,
  state TEXT NOT NULL,
  retry_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  delivered_at TEXT
);
CREATE INDEX IF NOT EXISTS events_created_at ON events(created_at);
CREATE INDEX IF NOT EXISTS events_seq ON events(seq);
CREATE INDEX IF NOT EXISTS deliveries_state ON deliveries(state, updated_at);
`)
	if err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	for _, migration := range []struct{ table, column, name string }{
		{"tasks", "parent_task_id TEXT NOT NULL DEFAULT ''", "task parent"},
		{"tasks", "parent_attempt_id TEXT NOT NULL DEFAULT ''", "task parent attempt"},
		{"tasks", "child_index INTEGER NOT NULL DEFAULT 0", "task child index"},
		{"worker_bindings", "workspace TEXT NOT NULL DEFAULT ''", "workspace"},
		{"worker_bindings", "parent_binding_id TEXT NOT NULL DEFAULT ''", "parent binding"},
		{"worker_bindings", "parent_attempt_id TEXT NOT NULL DEFAULT ''", "parent attempt"},
		{"worker_bindings", "profile_version TEXT NOT NULL DEFAULT ''", "profile version"},
		{"worker_bindings", "profile_name TEXT NOT NULL DEFAULT ''", "profile name"},
		{"worker_bindings", "profile_hash TEXT NOT NULL DEFAULT ''", "profile hash"},
		{"worker_bindings", "runtime TEXT NOT NULL DEFAULT ''", "runtime"},
		{"worker_bindings", "model TEXT NOT NULL DEFAULT ''", "model"},
		{"worker_bindings", "reasoning TEXT NOT NULL DEFAULT ''", "reasoning"},
		{"worker_bindings", "allow_tools TEXT NOT NULL DEFAULT ''", "allow tools"},
		{"worker_bindings", "profile_delivery TEXT NOT NULL DEFAULT ''", "profile delivery"},
	} {
		if _, err = s.db.ExecContext(ctx, `ALTER TABLE `+migration.table+` ADD COLUMN `+migration.column); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate %s %s: %w", migration.table, migration.name, err)
		}
	}
	if _, err = s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS tasks_parent_attempt ON tasks(parent_attempt_id); CREATE UNIQUE INDEX IF NOT EXISTS tasks_child_index ON tasks(parent_attempt_id, child_index) WHERE parent_attempt_id <> ''`); err != nil {
		return fmt.Errorf("migrate child task indexes: %w", err)
	}
	if err := s.migrateDurableEventSchema(ctx); err != nil {
		return err
	}
	if err := s.migratePhase4Lifecycle(ctx); err != nil {
		return err
	}
	return nil
}

type personConversation struct {
	person       Person
	conversation Conversation
}

type inboundEntry struct {
	entry     ConversationEntry
	duplicate bool
}

type acceptedDispatch struct {
	task    Task
	binding WorkerBinding
	attempt Attempt
}

type completedResult struct {
	result    Result
	entry     ConversationEntry
	duplicate bool
}

func (s *Store) CreatePersonWithConversation(ctx context.Context) (Person, Conversation, error) {
	created, err := withTx(s, ctx, func(tx *sql.Tx) (personConversation, error) {
		now := s.now()
		person := Person{ID: newID("per"), CreatedAt: now}
		conversation := Conversation{ID: newID("con"), PersonID: person.ID}
		if _, err := tx.ExecContext(ctx, `INSERT INTO persons(id, created_at) VALUES(?, ?)`, person.ID, timestamp(now)); err != nil {
			return personConversation{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(id, person_id, created_at) VALUES(?, ?, ?)`, conversation.ID, person.ID, timestamp(now)); err != nil {
			return personConversation{}, err
		}
		return personConversation{person: person, conversation: conversation}, nil
	})
	return created.person, created.conversation, err
}

func (s *Store) EnsureOwner(ctx context.Context) (Person, Conversation, error) {
	var person Person
	err := s.db.QueryRowContext(ctx, `SELECT id, created_at FROM persons ORDER BY created_at LIMIT 1`).Scan(&person.ID, newTimestampScanner(&person.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return s.CreatePersonWithConversation(ctx)
	}
	if err != nil {
		return Person{}, Conversation{}, err
	}
	conversation, err := s.ConversationForPerson(ctx, person.ID)
	return person, conversation, err
}

func (s *Store) ConversationForPerson(ctx context.Context, personID string) (Conversation, error) {
	var conversation Conversation
	err := s.db.QueryRowContext(ctx, `SELECT id, person_id FROM conversations WHERE person_id = ?`, personID).Scan(&conversation.ID, &conversation.PersonID)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, ErrNotFound
	}
	return conversation, err
}

func (s *Store) CreateWebSession(ctx context.Context, personID string) (string, error) {
	token := newID("web")
	_, err := s.db.ExecContext(ctx, `INSERT INTO web_sessions(id, person_id, token_hash, created_at) VALUES(?, ?, ?, ?)`, newID("wse"), personID, hashToken(token), timestamp(s.now()))
	return token, err
}

func (s *Store) WebSessionPerson(ctx context.Context, token string) (Person, error) {
	var person Person
	err := s.db.QueryRowContext(ctx, `SELECT p.id, p.created_at FROM persons p JOIN web_sessions w ON w.person_id = p.id WHERE w.token_hash = ?`, hashToken(token)).Scan(&person.ID, newTimestampScanner(&person.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	return person, err
}

func (s *Store) AppendInbound(ctx context.Context, conversationID, adapterID, externalID, body string) (ConversationEntry, bool, error) {
	stored, err := withTx(s, ctx, func(tx *sql.Tx) (inboundEntry, error) {
		var entryID string
		err := tx.QueryRowContext(ctx, `SELECT entry_id FROM inbound_messages WHERE adapter_id = ? AND external_message_id = ?`, adapterID, externalID).Scan(&entryID)
		if err == nil {
			entry, err := getEntry(ctx, tx, entryID)
			return inboundEntry{entry: entry, duplicate: true}, err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return inboundEntry{}, err
		}
		entry, err := appendEntry(ctx, tx, s.now(), conversationID, EntryUser, body)
		if err != nil {
			return inboundEntry{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_messages(adapter_id, external_message_id, entry_id) VALUES(?, ?, ?)`, adapterID, externalID, entry.ID); err != nil {
			return inboundEntry{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE events SET kind = ?, source = ? WHERE correlation_id = ?`, "message.saved", adapterID, entry.ID); err != nil {
			return inboundEntry{}, err
		}
		return inboundEntry{entry: entry}, nil
	})
	if err == nil && !stored.duplicate {
		s.notifyEntry(stored.entry)
	}
	return stored.entry, stored.duplicate, err
}

func (s *Store) AppendEntry(ctx context.Context, conversationID string, kind EntryKind, body string) (ConversationEntry, error) {
	entry, err := withTx(s, ctx, func(tx *sql.Tx) (ConversationEntry, error) {
		return appendEntry(ctx, tx, s.now(), conversationID, kind, body)
	})
	if err == nil {
		s.notifyEntry(entry)
	}
	return entry, err
}

func (s *Store) EntriesAfter(ctx context.Context, conversationID string, afterSeq int64) ([]ConversationEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, seq, kind, body, created_at FROM conversation_entries WHERE conversation_id = ? AND seq > ? ORDER BY seq`, conversationID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]ConversationEntry, 0)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) CreateTask(ctx context.Context, conversationID, text string) (Task, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Task, error) {
		now := s.now()
		task := Task{ID: newID("tsk"), ConversationID: conversationID, Text: text, State: TaskDispatching, CreatedAt: now, UpdatedAt: now}
		_, err := tx.ExecContext(ctx, `INSERT INTO tasks(id, conversation_id, text, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`, task.ID, task.ConversationID, task.Text, task.State, timestamp(now), timestamp(now))
		return task, err
	})
}

func (s *Store) MarkDispatchFailed(ctx context.Context, taskID string) (Task, error) {
	return s.transitionTask(ctx, taskID, []TaskState{TaskDispatching}, TaskDispatchFailed)
}

func (s *Store) RetryDispatch(ctx context.Context, taskID string) (Task, error) {
	return s.transitionTask(ctx, taskID, []TaskState{TaskDispatchFailed}, TaskDispatching)
}

func (s *Store) AcceptDispatch(ctx context.Context, taskID, workerRef, nodeID, runtimeSessionID, workspace string) (Task, WorkerBinding, Attempt, error) {
	return s.AcceptDispatchWithProfile(ctx, taskID, workerRef, nodeID, runtimeSessionID, workspace, BindingProfile{})
}

func (s *Store) AcceptDispatchWithProfile(ctx context.Context, taskID, workerRef, nodeID, runtimeSessionID, workspace string, profile BindingProfile) (Task, WorkerBinding, Attempt, error) {
	accepted, err := withTx(s, ctx, func(tx *sql.Tx) (acceptedDispatch, error) {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return acceptedDispatch{}, err
		}
		if task.State != TaskDispatching {
			return acceptedDispatch{}, ErrInvalidTransition
		}
		now := s.now()
		binding := WorkerBinding{ID: newID("wkb"), TaskID: task.ID, WorkerRef: workerRef, NodeID: nodeID, RuntimeSessionID: runtimeSessionID, Workspace: workspace, Profile: profile, CreatedAt: now}
		attempt := Attempt{ID: newID("att"), WorkerBindingID: binding.ID, Number: 1, State: AttemptStarting, CreatedAt: now, UpdatedAt: now}
		if task.ParentTaskID != "" {
			if err := tx.QueryRowContext(ctx, `SELECT id FROM worker_bindings WHERE task_id = ? AND archived = 0`, task.ParentTaskID).Scan(&binding.ParentBindingID); err != nil {
				return acceptedDispatch{}, err
			}
			binding.ParentAttemptID = task.ParentAttemptID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO worker_bindings(id, task_id, worker_ref, node_id, runtime_session_id, workspace, parent_binding_id, parent_attempt_id, profile_version, profile_name, profile_hash, runtime, model, reasoning, allow_tools, profile_delivery, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, binding.ID, binding.TaskID, binding.WorkerRef, binding.NodeID, binding.RuntimeSessionID, binding.Workspace, binding.ParentBindingID, binding.ParentAttemptID, binding.Profile.Version, binding.Profile.Name, binding.Profile.Hash, binding.Profile.Runtime, binding.Profile.Model, binding.Profile.Reasoning, binding.Profile.Tools, binding.Profile.Delivery, timestamp(now)); err != nil {
			return acceptedDispatch{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO attempts(id, worker_binding_id, number, state, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`, attempt.ID, attempt.WorkerBindingID, attempt.Number, attempt.State, timestamp(now), timestamp(now)); err != nil {
			return acceptedDispatch{}, err
		}
		task.State, task.UpdatedAt = TaskOpen, now
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, task.State, timestamp(now), task.ID); err != nil {
			return acceptedDispatch{}, err
		}
		return acceptedDispatch{task: task, binding: binding, attempt: attempt}, nil
	})
	return accepted.task, accepted.binding, accepted.attempt, err
}

func (s *Store) SetAttemptActive(ctx context.Context, attemptID string) (Attempt, error) {
	attempt, err := s.transitionAttempt(ctx, attemptID, []AttemptState{AttemptStarting}, AttemptActive)
	if !errors.Is(err, ErrNotFound) {
		return attempt, err
	}
	return s.SetPhase4AttemptActive(ctx, attemptID)
}

func (s *Store) MarkAttemptInterrupted(ctx context.Context, attemptID string) (Attempt, error) {
	attempt, err := s.transitionAttempt(ctx, attemptID, []AttemptState{AttemptStarting, AttemptActive}, AttemptInterrupted)
	if !errors.Is(err, ErrNotFound) {
		return attempt, err
	}
	if _, _, _, err := s.InterruptPhase4Attempt(ctx, attemptID, "interrupted", "Attempt explicitly interrupted"); err != nil {
		return Attempt{}, err
	}
	return s.Phase4Attempt(ctx, attemptID)
}

func (s *Store) CompleteAttempt(ctx context.Context, attemptID string, status ResultStatus, summary string) (Result, bool, error) {
	completed, err := withTx(s, ctx, func(tx *sql.Tx) (completedResult, error) {
		var existing Result
		err := tx.QueryRowContext(ctx, `SELECT id, attempt_id, status, summary, created_at FROM results WHERE attempt_id = ?`, attemptID).Scan(&existing.ID, &existing.AttemptID, &existing.Status, &existing.Summary, newTimestampScanner(&existing.CreatedAt))
		if err == nil {
			return completedResult{result: existing, duplicate: true}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return completedResult{}, err
		}
		attempt, err := getAttempt(ctx, tx, attemptID)
		if err != nil {
			return completedResult{}, err
		}
		if attempt.State != AttemptStarting && attempt.State != AttemptActive {
			return completedResult{}, ErrInvalidTransition
		}
		terminal, err := resultAttemptState(status)
		if err != nil {
			return completedResult{}, err
		}
		now := s.now()
		if _, err = tx.ExecContext(ctx, `UPDATE attempts SET state = ?, updated_at = ? WHERE id = ?`, terminal, timestamp(now), attempt.ID); err != nil {
			return completedResult{}, err
		}
		result := Result{ID: newID("res"), AttemptID: attempt.ID, Status: status, Summary: summary, CreatedAt: now}
		if _, err = tx.ExecContext(ctx, `INSERT INTO results(id, attempt_id, status, summary, created_at) VALUES(?, ?, ?, ?, ?)`, result.ID, result.AttemptID, result.Status, result.Summary, timestamp(now)); err != nil {
			return completedResult{}, err
		}
		var conversationID, parentBindingID string
		if err = tx.QueryRowContext(ctx, `SELECT t.conversation_id, w.parent_binding_id FROM tasks t JOIN worker_bindings w ON w.task_id = t.id WHERE w.id = ?`, attempt.WorkerBindingID).Scan(&conversationID, &parentBindingID); err != nil {
			return completedResult{}, err
		}
		var entry ConversationEntry
		if parentBindingID == "" {
			entry, err = appendEntry(ctx, tx, now, conversationID, EntryWorkerResult, summary)
			if err != nil {
				return completedResult{}, err
			}
		} else {
			if _, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = (SELECT task_id FROM worker_bindings WHERE id = ?)`, TaskClosed, timestamp(now), attempt.WorkerBindingID); err != nil {
				return completedResult{}, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE worker_bindings SET archived = 1 WHERE id = ?`, attempt.WorkerBindingID); err != nil {
				return completedResult{}, err
			}
		}
		return completedResult{result: result, entry: entry}, nil
	})
	if err == nil && !completed.duplicate && completed.entry.ID != "" {
		s.notifyEntry(completed.entry)
	}
	return completed.result, completed.duplicate, err
}

func (s *Store) CloseTask(ctx context.Context, taskID string) (CloseOutcome, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (CloseOutcome, error) {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return CloseOutcome{}, err
		}
		if task.State != TaskOpen {
			return CloseOutcome{}, ErrInvalidTransition
		}
		var attempt Attempt
		err = tx.QueryRowContext(ctx, `SELECT a.id, a.worker_binding_id, a.number, a.state, a.created_at, a.updated_at FROM attempts a JOIN worker_bindings w ON w.id = a.worker_binding_id WHERE w.task_id = ? ORDER BY a.number DESC LIMIT 1`, taskID).Scan(&attempt.ID, &attempt.WorkerBindingID, &attempt.Number, &attempt.State, newTimestampScanner(&attempt.CreatedAt), newTimestampScanner(&attempt.UpdatedAt))
		if err != nil {
			return CloseOutcome{}, err
		}
		now := s.now()
		if attempt.State == AttemptStarting || attempt.State == AttemptActive {
			task.State, task.UpdatedAt = TaskClosing, now
			_, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, task.State, timestamp(now), taskID)
			return CloseOutcome{Task: task, CancelAttempt: &attempt}, err
		}
		task.State, task.UpdatedAt = TaskClosed, now
		if _, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, task.State, timestamp(now), taskID); err != nil {
			return CloseOutcome{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE worker_bindings SET archived = 1 WHERE task_id = ?`, taskID)
		return CloseOutcome{Task: task}, err
	})
}

func (s *Store) FinishClosingTask(ctx context.Context, taskID string) (Task, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Task, error) {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return Task{}, err
		}
		if task.State != TaskClosing {
			return Task{}, ErrInvalidTransition
		}
		now := s.now()
		task.State, task.UpdatedAt = TaskClosed, now
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, task.State, timestamp(now), task.ID); err != nil {
			return Task{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE worker_bindings SET archived = 1 WHERE task_id = ?`, task.ID); err != nil {
			return Task{}, err
		}
		return task, nil
	})
}

func (s *Store) RotateSecretaryCapability(ctx context.Context, personID string) (string, error) {
	token := newID("sec")
	hash := hashToken(token)
	err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		now := s.now()
		if _, err := tx.ExecContext(ctx, `UPDATE secretary_capabilities SET revoked_at = ? WHERE person_id = ? AND revoked_at IS NULL`, timestamp(now), personID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO secretary_capabilities(id, person_id, token_hash, created_at) VALUES(?, ?, ?, ?)`, newID("cap"), personID, hash, timestamp(now))
		return err
	})
	return token, err
}

func (s *Store) IssueWorkerCapability(ctx context.Context, taskID, workerRef string) (string, error) {
	if taskID == "" || workerRef == "" {
		return "", errors.New("core: task and worker reference are required")
	}
	token := newID("wcap")
	err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if task.State != TaskDispatching && task.State != TaskOpen {
			return ErrInvalidTransition
		}
		now := s.now()
		result, err := tx.ExecContext(ctx, `UPDATE worker_capabilities SET task_id = ?, token_hash = ?, revoked_at = NULL, created_at = ? WHERE worker_ref = ?`, taskID, hashToken(token), timestamp(now), workerRef)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO worker_capabilities(id, task_id, worker_ref, token_hash, created_at) VALUES(?, ?, ?, ?, ?)`, newID("wcap"), taskID, workerRef, hashToken(token), timestamp(now))
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) RevokeWorkerCapability(ctx context.Context, workerRef string) error {
	if workerRef == "" {
		return errors.New("core: worker reference is required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE worker_capabilities SET revoked_at = ? WHERE worker_ref = ? AND revoked_at IS NULL`, timestamp(s.now()), workerRef)
	return err
}

func (s *Store) AuthorizeWorkerCapability(ctx context.Context, workerRef, token string) (bool, error) {
	if workerRef == "" || token == "" {
		return false, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_capabilities WHERE worker_ref = ? AND token_hash = ? AND revoked_at IS NULL`, workerRef, hashToken(token)).Scan(&count)
	return count == 1, err
}

func (s *Store) HasSecretaryCapability(ctx context.Context, personID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_capabilities WHERE person_id = ? AND revoked_at IS NULL`, personID).Scan(&count)
	return count > 0, err
}

func (s *Store) AuthorizeSecretaryCapability(ctx context.Context, personID, token string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM secretary_capabilities WHERE person_id = ? AND token_hash = ? AND revoked_at IS NULL`, personID, hashToken(token)).Scan(&count)
	return count == 1, err
}

func (s *Store) transitionTask(ctx context.Context, taskID string, from []TaskState, to TaskState) (Task, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Task, error) {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return Task{}, err
		}
		for _, state := range from {
			if task.State == state {
				task.State, task.UpdatedAt = to, s.now()
				_, err = tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, task.State, timestamp(task.UpdatedAt), task.ID)
				return task, err
			}
		}
		return Task{}, ErrInvalidTransition
	})
}

func (s *Store) transitionAttempt(ctx context.Context, attemptID string, from []AttemptState, to AttemptState) (Attempt, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (Attempt, error) {
		attempt, err := getAttempt(ctx, tx, attemptID)
		if err != nil {
			return Attempt{}, err
		}
		for _, state := range from {
			if attempt.State == state {
				attempt.State, attempt.UpdatedAt = to, s.now()
				_, err = tx.ExecContext(ctx, `UPDATE attempts SET state = ?, updated_at = ? WHERE id = ?`, attempt.State, timestamp(attempt.UpdatedAt), attempt.ID)
				return attempt, err
			}
		}
		return Attempt{}, ErrInvalidTransition
	})
}

func appendEntry(ctx context.Context, tx *sql.Tx, now time.Time, conversationID string, kind EntryKind, body string) (ConversationEntry, error) {
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM conversation_entries WHERE conversation_id = ?`, conversationID).Scan(&seq); err != nil {
		return ConversationEntry{}, err
	}
	entry := ConversationEntry{ID: newID("ent"), ConversationID: conversationID, Seq: seq, Kind: kind, Body: body, CreatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_entries(id, conversation_id, seq, kind, body, created_at) VALUES(?, ?, ?, ?, ?, ?)`, entry.ID, entry.ConversationID, entry.Seq, entry.Kind, entry.Body, timestamp(now)); err != nil {
		return ConversationEntry{}, err
	}
	event, err := appendEventTx(ctx, tx, now, EventInput{Kind: "conversation.entry", AggregateType: "conversation", AggregateID: conversationID, Source: "server", CorrelationID: entry.ID, Payload: entry}, entry)
	if err != nil {
		return ConversationEntry{}, err
	}
	if _, _, err := enqueueDeliveryTx(ctx, tx, now, event.ID, entry.ID, "conversation", "conversation:"+entry.ID); err != nil {
		return ConversationEntry{}, err
	}
	return entry, nil
}

func (s *Store) Task(ctx context.Context, id string) (Task, error) { return getTask(ctx, s.db, id) }

func (s *Store) TasksForConversation(ctx context.Context, conversationID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, text, state, parent_task_id, parent_attempt_id, child_index, created_at, updated_at FROM tasks WHERE conversation_id = ? ORDER BY created_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.ConversationID, &task.Text, &task.State, &task.ParentTaskID, &task.ParentAttemptID, &task.ChildIndex, newTimestampScanner(&task.CreatedAt), newTimestampScanner(&task.UpdatedAt)); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func getTask(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Task, error) {
	var task Task
	err := q.QueryRowContext(ctx, `SELECT id, conversation_id, text, state, parent_task_id, parent_attempt_id, child_index, created_at, updated_at FROM tasks WHERE id = ?`, id).Scan(&task.ID, &task.ConversationID, &task.Text, &task.State, &task.ParentTaskID, &task.ParentAttemptID, &task.ChildIndex, newTimestampScanner(&task.CreatedAt), newTimestampScanner(&task.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

func getAttempt(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Attempt, error) {
	var attempt Attempt
	err := q.QueryRowContext(ctx, `SELECT id, worker_binding_id, number, state, created_at, updated_at FROM attempts WHERE id = ?`, id).Scan(&attempt.ID, &attempt.WorkerBindingID, &attempt.Number, &attempt.State, newTimestampScanner(&attempt.CreatedAt), newTimestampScanner(&attempt.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, ErrNotFound
	}
	return attempt, err
}

func getEntry(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (ConversationEntry, error) {
	var entry ConversationEntry
	err := q.QueryRowContext(ctx, `SELECT id, conversation_id, seq, kind, body, created_at FROM conversation_entries WHERE id = ?`, id).Scan(&entry.ID, &entry.ConversationID, &entry.Seq, &entry.Kind, &entry.Body, newTimestampScanner(&entry.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationEntry{}, ErrNotFound
	}
	return entry, err
}

func scanEntry(scanner interface{ Scan(...any) error }) (ConversationEntry, error) {
	var entry ConversationEntry
	err := scanner.Scan(&entry.ID, &entry.ConversationID, &entry.Seq, &entry.Kind, &entry.Body, newTimestampScanner(&entry.CreatedAt))
	return entry, err
}

func resultAttemptState(status ResultStatus) (AttemptState, error) {
	switch status {
	case ResultSucceeded:
		return AttemptSucceeded, nil
	case ResultFailed:
		return AttemptFailed, nil
	case ResultCanceled:
		return AttemptCanceled, nil
	case ResultInterrupted:
		return AttemptInterrupted, nil
	default:
		return "", fmt.Errorf("unknown result status %q", status)
	}
}

func newID(prefix string) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(bytes)
}
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

type timestampScanner struct{ destination *time.Time }

func newTimestampScanner(destination *time.Time) *timestampScanner {
	return &timestampScanner{destination}
}
func (s *timestampScanner) Scan(value any) error {
	text, ok := value.(string)
	if !ok {
		b, bok := value.([]byte)
		if !bok {
			return fmt.Errorf("timestamp is %T", value)
		}
		text = string(b)
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err == nil {
		*s.destination = parsed
	}
	return err
}

func withTx[T any](store *Store, ctx context.Context, fn func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	for try := 0; try < 8; try++ {
		tx, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			if sqliteRetryable(err) {
				time.Sleep(time.Duration(try+1) * 5 * time.Millisecond)
				continue
			}
			return zero, err
		}
		value, err := fn(tx)
		if err != nil {
			_ = tx.Rollback()
			if sqliteRetryable(err) {
				time.Sleep(time.Duration(try+1) * 5 * time.Millisecond)
				continue
			}
			return zero, err
		}
		if err := tx.Commit(); err != nil {
			if sqliteRetryable(err) {
				time.Sleep(time.Duration(try+1) * 5 * time.Millisecond)
				continue
			}
			return zero, err
		}
		return value, nil
	}
	return zero, errors.New("core: sqlite remained locked after transaction retries")
}
func withTxErr(store *Store, ctx context.Context, fn func(*sql.Tx) error) error {
	_, err := withTx(store, ctx, func(tx *sql.Tx) (struct{}, error) { return struct{}{}, fn(tx) })
	return err
}

func sqliteRetryable(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "database is locked") || strings.Contains(text, "database table is locked") || strings.Contains(text, "busy") {
		return true
	}
	return strings.Contains(text, "unique constraint failed: conversation_entries") || strings.Contains(text, "unique constraint failed: phase4_attempt_outcomes") || strings.Contains(text, "unique constraint failed: phase4_results")
}
