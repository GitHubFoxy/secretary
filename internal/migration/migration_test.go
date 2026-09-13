package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	_ "modernc.org/sqlite"
)

func TestManualCopyMigrationPreservesPhase3HistoryAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "phase3.db")
	if err := seedPhase3Database(ctx, source); err != nil {
		t.Fatal(err)
	}
	userSource := filepath.Join(dir, "source-user.md")
	if err := os.WriteFile(userSource, []byte("# User\nkeep this\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userSource+".revision", []byte("7\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	copyPath := filepath.Join(dir, "copy", "secretary.db")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fixtureCopyFile(source, copyPath); err != nil {
		t.Fatal(err)
	}
	userCopy := filepath.Join(dir, "copy", "user.md")
	report, err := Run(ctx, Options{
		SourcePath:                  copyPath,
		DestinationPath:             copyPath,
		SourceUserDocumentPath:      userSource,
		DestinationUserDocumentPath: userCopy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Workers != 1 || report.Turns != 1 || report.Attempts != 2 || report.Outcomes != 2 || report.Results != 1 {
		t.Fatalf("unexpected migration report: %#v", report)
	}
	second, err := Run(ctx, Options{SourcePath: copyPath, DestinationPath: copyPath, SourceUserDocumentPath: userSource, DestinationUserDocumentPath: userCopy})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Idempotent || second.Workers != 0 || second.Turns != 0 || second.Attempts != 0 || second.Outcomes != 0 || second.Results != 0 {
		t.Fatalf("rerun was not idempotent: %#v", second)
	}
	content, err := os.ReadFile(userCopy)
	if err != nil || string(content) != "# User\nkeep this\n" {
		t.Fatalf("user.md content=%q err=%v", content, err)
	}
	revision, err := os.ReadFile(userCopy + ".revision")
	if err != nil || string(revision) != "7\n" {
		t.Fatalf("user.md revision=%q err=%v", revision, err)
	}

	db, err := sql.Open("sqlite", copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var personID, conversationID, body, intent, projectSnapshot string
	if err := db.QueryRowContext(ctx, `SELECT p.id, c.id, e.body, w.intent, w.project_snapshot
		FROM persons p JOIN conversations c ON c.person_id = p.id
		JOIN conversation_entries e ON e.conversation_id = c.id
		JOIN workers w ON w.conversation_id = c.id
		WHERE e.kind = 'worker_result'`).Scan(&personID, &conversationID, &body, &intent, &projectSnapshot); err != nil {
		t.Fatal(err)
	}
	if personID != "person-1" || conversationID != "conversation-1" || body != "done" || intent != "ship it" || !strings.Contains(projectSnapshot, "workspace") {
		t.Fatalf("history lost: person=%q conversation=%q body=%q intent=%q project=%q", personID, conversationID, body, intent, projectSnapshot)
	}
	var taskCount, workerCount, turnCount, attemptCount, outcomeCount, resultCount int
	for query, target := range map[string]*int{
		`SELECT COUNT(*) FROM tasks`:                   &taskCount,
		`SELECT COUNT(*) FROM workers`:                 &workerCount,
		`SELECT COUNT(*) FROM turns`:                   &turnCount,
		`SELECT COUNT(*) FROM phase4_attempts`:         &attemptCount,
		`SELECT COUNT(*) FROM phase4_attempt_outcomes`: &outcomeCount,
		`SELECT COUNT(*) FROM phase4_results`:          &resultCount,
	} {
		if err := db.QueryRowContext(ctx, query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if taskCount != 2 || workerCount != 1 || turnCount != 1 || attemptCount != 2 || outcomeCount != 2 || resultCount != 1 {
		t.Fatalf("counts tasks=%d workers=%d turns=%d attempts=%d outcomes=%d results=%d", taskCount, workerCount, turnCount, attemptCount, outcomeCount, resultCount)
	}
	var interrupted int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_attempt_outcomes WHERE status = 'interrupted' AND classification = 'final'`).Scan(&interrupted); err != nil {
		t.Fatal(err)
	}
	if interrupted != 1 {
		t.Fatalf("unknown native session was not explicit interrupted: %d", interrupted)
	}
	var runtimeColumns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('workers') WHERE name = 'runtime_session_id'`).Scan(&runtimeColumns); err != nil {
		t.Fatal(err)
	}
	if runtimeColumns != 0 {
		t.Fatal("runtime_session_id leaked into server Worker schema")
	}
}

func TestMigrationSplitsLegacyRuntimeConfigAndPinsModels(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secretary.db")
	if err := seedPhase3Database(ctx, dbPath); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"secretary.md", "worker.md", "child-worker.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+" policy"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(dir, "config.toml")
	legacy := `[profiles]
secretary = "secretary.md"
worker = "worker.md"
child_worker = "child-worker.md"
[tools]
allow_tools = ["read"]
[models]
secretary = "provider/secretary"
fast = "fast"
smart = "smart"
cheap = "cheap"
[runtime]
harness = "fx"
reasoning = "high"
`
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: dbPath, DestinationPath: dbPath, ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if strings.Contains(text, "[runtime]") || strings.Contains(text, `fast = "fast"`) || strings.Contains(text, `smart = "smart"`) || strings.Contains(text, `cheap = "cheap"`) {
		t.Fatalf("legacy config contract remains: %s", text)
	}
	for _, required := range []string{"[secretary]", "harness = 'fx'", "model = 'provider/secretary'", "[worker_policy]", "default_harness = 'fx'", `fast = 'default'`} {
		if !strings.Contains(text, required) {
			t.Fatalf("migrated config misses %q: %s", required, text)
		}
	}
}

func TestMigratedStoreKeepsSecretaryIdentityAndRejectsNewLegacyTask(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "secretary.db")
	if err := seedPhase3Database(ctx, dbPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: dbPath, DestinationPath: dbPath}); err != nil {
		t.Fatal(err)
	}
	store, err := core.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity, err := store.SecretaryIdentity(ctx, "person-1")
	if err != nil || identity.ID == "" || identity.ConversationID != "conversation-1" {
		t.Fatalf("identity=%#v err=%v", identity, err)
	}
	if _, err := store.CreateTask(ctx, "conversation-1", "must not be created"); err == nil {
		t.Fatal("created a new legacy Task after migration")
	}
}

func TestInvalidMigrationLeavesActiveDatabaseUntouchedAfterBackup(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "secretary.db")
	if err := seedPhase3Database(ctx, dbPath); err != nil {
		t.Fatal(err)
	}
	before, err := fileDigest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[runtime]\nharness = \"not-a-harness\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configBefore, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: dbPath, DestinationPath: dbPath, ConfigPath: configPath}); err == nil {
		t.Fatal("invalid migration input unexpectedly succeeded")
	}
	after, err := fileDigest(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("active database changed after invalid migration")
	}
	if _, err := os.Stat(dbPath + ".backup"); err != nil {
		t.Fatalf("automatic backup missing: %v", err)
	}
	if _, err := os.Stat(configPath + ".backup"); err != nil {
		t.Fatalf("config backup missing: %v", err)
	}
	configAfter, err := fileDigest(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if configBefore != configAfter {
		t.Fatal("active config changed after invalid migration")
	}
}

func fileDigest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func fixtureCopyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o600)
}

func seedPhase3Database(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
CREATE TABLE persons (id TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE conversations (id TEXT PRIMARY KEY, person_id TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL);
CREATE TABLE conversation_entries (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, seq INTEGER NOT NULL, kind TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(conversation_id, seq));
CREATE TABLE tasks (id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, text TEXT NOT NULL, state TEXT NOT NULL, parent_task_id TEXT NOT NULL DEFAULT '', parent_attempt_id TEXT NOT NULL DEFAULT '', child_index INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE worker_bindings (id TEXT PRIMARY KEY, task_id TEXT NOT NULL UNIQUE, worker_ref TEXT NOT NULL UNIQUE, node_id TEXT NOT NULL, runtime_session_id TEXT NOT NULL, workspace TEXT NOT NULL DEFAULT '', parent_binding_id TEXT NOT NULL DEFAULT '', parent_attempt_id TEXT NOT NULL DEFAULT '', profile_version TEXT NOT NULL DEFAULT '', profile_name TEXT NOT NULL DEFAULT '', profile_hash TEXT NOT NULL DEFAULT '', runtime TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', reasoning TEXT NOT NULL DEFAULT '', allow_tools TEXT NOT NULL DEFAULT '', profile_delivery TEXT NOT NULL DEFAULT '', archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL);
CREATE TABLE attempts (id TEXT PRIMARY KEY, worker_binding_id TEXT NOT NULL, number INTEGER NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(worker_binding_id, number));
CREATE TABLE results (id TEXT PRIMARY KEY, attempt_id TEXT NOT NULL UNIQUE, status TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL);
INSERT INTO persons VALUES ('person-1', '2024-01-01T00:00:00Z');
INSERT INTO conversations VALUES ('conversation-1', 'person-1', '2024-01-01T00:00:00Z');
INSERT INTO conversation_entries VALUES ('entry-user', 'conversation-1', 1, 'user', 'ship it', '2024-01-01T00:00:00Z');
INSERT INTO conversation_entries VALUES ('entry-result', 'conversation-1', 2, 'worker_result', 'done', '2024-01-01T00:01:00Z');
INSERT INTO tasks VALUES ('task-1', 'conversation-1', 'ship it', 'open', '', '', 0, '2024-01-01T00:00:00Z', '2024-01-01T00:01:00Z');
INSERT INTO tasks VALUES ('task-child', 'conversation-1', 'child history', 'closed', 'task-1', 'attempt-2', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:01:00Z');
INSERT INTO worker_bindings VALUES ('binding-1', 'task-1', 'worker-1', 'node-1', 'native-session-1', '/work/project', '', '', 'cfg-1', 'worker', 'hash', 'fx', 'gpt-5.6-luna', 'high', 'read', 'metadata', 0, '2024-01-01T00:00:00Z');
INSERT INTO attempts VALUES ('attempt-1', 'binding-1', 1, 'succeeded', '2024-01-01T00:00:00Z', '2024-01-01T00:00:10Z');
INSERT INTO attempts VALUES ('attempt-2', 'binding-1', 2, 'active', '2024-01-01T00:01:00Z', '2024-01-01T00:01:10Z');
INSERT INTO results VALUES ('result-1', 'attempt-1', 'succeeded', 'done', '2024-01-01T00:00:10Z');
`)
	return err
}
