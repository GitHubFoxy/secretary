package core

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestTicket10MigrationBackfillsLegacyWorkerResultIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ticket10-b1-legacy.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, _, attempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{WorkerRef: "legacy-worker", Intent: "inspect", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "safe"}, TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	finished, err := store.FinishAttempt(ctx, attempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "legacy summary"}})
	if err != nil || finished.Result == nil {
		t.Fatalf("finish legacy result=%#v err=%v", finished.Result, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Rebuild only conversation_entries to the c431e4c shape. The durable
	// Phase 4 Worker/Turn/Result relation remains available for migration.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var entry ConversationEntry
	if err := raw.QueryRowContext(ctx, `SELECT id, conversation_id, seq, kind, body, created_at FROM conversation_entries WHERE kind = ?`, EntryWorkerResult).Scan(&entry.ID, &entry.ConversationID, &entry.Seq, &entry.Kind, &entry.Body, newTimestampScanner(&entry.CreatedAt)); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	_, err = raw.ExecContext(ctx, `PRAGMA foreign_keys = OFF;
ALTER TABLE conversation_entries RENAME TO conversation_entries_with_identity;
CREATE TABLE conversation_entries (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL REFERENCES conversations(id),
  seq INTEGER NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(conversation_id, seq)
);
INSERT INTO conversation_entries(id, conversation_id, seq, kind, body, created_at) VALUES(?, ?, ?, ?, ?, ?);
DROP TABLE conversation_entries_with_identity;`, entry.ID, entry.ConversationID, entry.Seq, entry.Kind, entry.Body, timestamp(entry.CreatedAt))
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var results []ConversationEntry
	for _, candidate := range entries {
		if candidate.Kind == EntryWorkerResult {
			results = append(results, candidate)
		}
	}
	if len(results) != 1 {
		t.Fatalf("migrated worker result entries=%d, want exactly one: %#v", len(results), results)
	}
	migrated := results[0]
	if migrated.WorkerRef != worker.WorkerRef || migrated.TurnID != finished.Result.TurnID || migrated.ResultID != finished.Result.ID {
		t.Fatalf("migrated identity=%#v, want worker=%q turn=%q result=%q", migrated, worker.WorkerRef, finished.Result.TurnID, finished.Result.ID)
	}
}

func TestTicket10MigrationUsesExplicitFallbackForLegacyPhase3Result(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ticket10-b1-phase3.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO tasks(id, conversation_id, text, state, created_at, updated_at) VALUES('task-legacy', ?, 'legacy', 'completed', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`, conversation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO worker_bindings(id, task_id, worker_ref, node_id, runtime_session_id, created_at) VALUES('binding-legacy', 'task-legacy', 'legacy-phase3-worker', 'node', 'native-session', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO attempts(id, worker_binding_id, number, state, created_at, updated_at) VALUES('attempt-legacy', 'binding-legacy', 1, 'succeeded', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO results(id, attempt_id, status, summary, created_at) VALUES('result-legacy', 'attempt-legacy', 'succeeded', 'phase3 summary', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := store.AppendEntry(ctx, conversation.ID, EntryWorkerResult, "phase3 summary"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, `PRAGMA foreign_keys = OFF; ALTER TABLE conversation_entries RENAME TO conversation_entries_with_identity; CREATE TABLE conversation_entries (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id), seq INTEGER NOT NULL, kind TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(conversation_id, seq)); INSERT INTO conversation_entries(id, conversation_id, seq, kind, body, created_at) SELECT id, conversation_id, seq, kind, body, created_at FROM conversation_entries_with_identity; DROP TABLE conversation_entries_with_identity;`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].WorkerRef != "legacy-phase3-worker" || entries[0].TurnID != "legacy-attempt:attempt-legacy" || entries[0].ResultID != "result-legacy" {
		t.Fatalf("legacy migration entries=%#v", entries)
	}
}

func TestTicket10TerminalConversationEntriesCarryResultIdentityPerTurn(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ticket10-b1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, firstTurn, firstAttempt, err := store.CreateWorker(ctx, conversation.ID, WorkerSpec{
		WorkerRef: "worker-one", Intent: "inspect", ProjectID: "project", NodeID: "node", HarnessInstanceID: "node/fx", PolicySnapshot: "private",
	}, TurnSpec{Input: "first"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.FinishAttempt(ctx, firstAttempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "same summary"}})
	if err != nil || first.Result == nil {
		t.Fatalf("finish first: result=%#v err=%v", first.Result, err)
	}
	secondTurn, secondAttempt, err := store.CreateTurn(ctx, worker.ID, TurnSpec{Input: "second"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.FinishAttempt(ctx, secondAttempt.ID, FinishAttemptInput{AttemptOutcomeInput: AttemptOutcomeInput{Status: OutcomeSucceeded, Classification: OutcomeFinal, Summary: "same summary"}})
	if err != nil || second.Result == nil {
		t.Fatalf("finish second: result=%#v err=%v", second.Result, err)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var results []ConversationEntry
	for _, entry := range entries {
		if entry.Kind == EntryWorkerResult {
			results = append(results, entry)
		}
	}
	if len(results) != 2 {
		t.Fatalf("worker result entries=%d want=2: %#v", len(results), results)
	}
	want := []struct{ turnID, resultID string }{{firstTurn.ID, first.Result.ID}, {secondTurn.ID, second.Result.ID}}
	for i, entry := range results {
		if entry.WorkerRef != worker.WorkerRef || entry.TurnID != want[i].turnID || entry.ResultID != want[i].resultID || entry.Body != "same summary" {
			t.Fatalf("entry[%d]=%#v want worker=%q turn=%q result=%q", i, entry, worker.WorkerRef, want[i].turnID, want[i].resultID)
		}
	}
}
