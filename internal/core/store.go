package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) RecoverInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE attempts SET state = ?, updated_at = ? WHERE state IN (?, ?)`, AttemptInterrupted, timestamp(s.now()), AttemptStarting, AttemptActive)
	return err
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
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS worker_bindings (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
  worker_ref TEXT NOT NULL UNIQUE,
  node_id TEXT NOT NULL,
  runtime_session_id TEXT NOT NULL,
  workspace TEXT NOT NULL DEFAULT '',
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
CREATE TABLE IF NOT EXISTS web_sessions (
  id TEXT PRIMARY KEY,
  person_id TEXT NOT NULL REFERENCES persons(id),
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);
`)
	if err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	if _, err = s.db.ExecContext(ctx, `ALTER TABLE worker_bindings ADD COLUMN workspace TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("migrate worker workspace: %w", err)
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
	var entries []ConversationEntry
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
	accepted, err := withTx(s, ctx, func(tx *sql.Tx) (acceptedDispatch, error) {
		task, err := getTask(ctx, tx, taskID)
		if err != nil {
			return acceptedDispatch{}, err
		}
		if task.State != TaskDispatching {
			return acceptedDispatch{}, ErrInvalidTransition
		}
		now := s.now()
		binding := WorkerBinding{ID: newID("wkb"), TaskID: task.ID, WorkerRef: workerRef, NodeID: nodeID, RuntimeSessionID: runtimeSessionID, Workspace: workspace, CreatedAt: now}
		attempt := Attempt{ID: newID("att"), WorkerBindingID: binding.ID, Number: 1, State: AttemptStarting, CreatedAt: now, UpdatedAt: now}
		if _, err := tx.ExecContext(ctx, `INSERT INTO worker_bindings(id, task_id, worker_ref, node_id, runtime_session_id, workspace, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, binding.ID, binding.TaskID, binding.WorkerRef, binding.NodeID, binding.RuntimeSessionID, binding.Workspace, timestamp(now)); err != nil {
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
	return s.transitionAttempt(ctx, attemptID, []AttemptState{AttemptStarting}, AttemptActive)
}

func (s *Store) MarkAttemptInterrupted(ctx context.Context, attemptID string) (Attempt, error) {
	return s.transitionAttempt(ctx, attemptID, []AttemptState{AttemptStarting, AttemptActive}, AttemptInterrupted)
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
		var conversationID string
		if err = tx.QueryRowContext(ctx, `SELECT t.conversation_id FROM tasks t JOIN worker_bindings w ON w.task_id = t.id WHERE w.id = ?`, attempt.WorkerBindingID).Scan(&conversationID); err != nil {
			return completedResult{}, err
		}
		entry, err := appendEntry(ctx, tx, now, conversationID, EntryWorkerResult, summary)
		if err != nil {
			return completedResult{}, err
		}
		return completedResult{result: result, entry: entry}, nil
	})
	if err == nil && !completed.duplicate {
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
	_, err := tx.ExecContext(ctx, `INSERT INTO conversation_entries(id, conversation_id, seq, kind, body, created_at) VALUES(?, ?, ?, ?, ?, ?)`, entry.ID, entry.ConversationID, entry.Seq, entry.Kind, entry.Body, timestamp(now))
	return entry, err
}

func (s *Store) Task(ctx context.Context, id string) (Task, error) { return getTask(ctx, s.db, id) }

func (s *Store) TasksForConversation(ctx context.Context, conversationID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, text, state, created_at, updated_at FROM tasks WHERE conversation_id = ? ORDER BY created_at`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]Task, 0)
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.ConversationID, &task.Text, &task.State, newTimestampScanner(&task.CreatedAt), newTimestampScanner(&task.UpdatedAt)); err != nil {
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
	err := q.QueryRowContext(ctx, `SELECT id, conversation_id, text, state, created_at, updated_at FROM tasks WHERE id = ?`, id).Scan(&task.ID, &task.ConversationID, &task.Text, &task.State, newTimestampScanner(&task.CreatedAt), newTimestampScanner(&task.UpdatedAt))
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
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	value, err := fn(tx)
	if err != nil {
		tx.Rollback()
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return value, nil
}
func withTxErr(store *Store, ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
