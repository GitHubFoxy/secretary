package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/config"
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
	phase4Store, err := core.Open(ctx, copyPath)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := phase4Store.ResolveWorkerBinding(ctx, "legacy-worker-binding-1")
	if err != nil {
		phase4Store.Close()
		t.Fatal(err)
	}
	if bound.Project.ID != "legacy-project-node-1" || bound.Project.Revision != 1 || bound.Node != "node-1" || bound.Workspace != "/work/project" || bound.HarnessInstance.ID != "legacy-fx" || bound.HarnessInstance.Node != "node-1" || bound.HarnessInstance.Kind != core.HarnessFX || bound.HarnessInstance.Status != core.HarnessUnavailable || bound.Snapshot.Policy.ModelPin() != "gpt-5.6-luna" || bound.Snapshot.Policy.Reasoning != "high" {
		phase4Store.Close()
		t.Fatalf("invalid migrated binding=%#v", bound)
	}
	if err := phase4Store.Close(); err != nil {
		t.Fatal(err)
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
	var childWorkers, childTurns int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers WHERE worker_ref = 'child-worker-1'`).Scan(&childWorkers); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE worker_id = 'legacy-worker-binding-child'`).Scan(&childTurns); err != nil {
		t.Fatal(err)
	}
	if childWorkers != 0 || childTurns != 0 {
		t.Fatalf("historical child was migrated: workers=%d turns=%d", childWorkers, childTurns)
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
fast = "provider/fast-model"
smart = "provider/smart-model"
cheap = "provider/cheap-model"
[runtime]
harness = "fx"
reasoning = "high"
`
	if err := os.WriteFile(configPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyDB.ExecContext(ctx, `UPDATE worker_bindings SET model = 'smart'`); err != nil {
		legacyDB.Close()
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
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
	if strings.Contains(text, "[runtime]") || strings.Contains(text, "\nfast =") || strings.Contains(text, "\nsmart =") || strings.Contains(text, "\ncheap =") {
		t.Fatalf("legacy config contract remains: %s", text)
	}
	if !strings.Contains(text, "model = 'provider/smart-model'") || !strings.Contains(text, "provider/fast-model") || !strings.Contains(text, "provider/cheap-model") {
		t.Fatalf("legacy model pins were lost: %s", text)
	}
	loaded, err := config.Load(configPath)
	if err != nil || loaded.Config.WorkerPolicy.Model != "provider/smart-model" || len(loaded.Config.WorkerPolicy.FallbackModels) != 2 {
		t.Fatalf("migrated config is not production-readable: snapshot=%#v err=%v", loaded.Config.WorkerPolicy, err)
	}
	var snapshotModel string
	if err := queryOne(dbPath, `SELECT json_extract(policy_snapshot, '$.model') FROM workers LIMIT 1`, &snapshotModel); err != nil || snapshotModel != "provider/smart-model" {
		t.Fatalf("snapshot model=%q err=%v", snapshotModel, err)
	}
	for _, required := range []string{"[secretary]", "harness = 'fx'", "model = 'provider/secretary'", "[worker_policy]", "default_harness = 'fx'"} {
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
	if _, err := store.CreateTask(ctx, "conversation-1", "must not be created"); !errors.Is(err, core.ErrLegacyTaskReadOnly) {
		t.Fatalf("legacy Task creation error=%v", err)
	}
	if _, err := store.MarkDispatchFailed(ctx, "task-1"); !errors.Is(err, core.ErrLegacyTaskReadOnly) {
		t.Fatalf("legacy Task transition error=%v", err)
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

func TestManualCopyMigrationPreservesRealisticPhase3Fixture(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "secretary.db")
	if err := seedPhase3Database(ctx, source); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE secretary_capabilities (id TEXT PRIMARY KEY, person_id TEXT NOT NULL, token_hash TEXT NOT NULL, created_at TEXT NOT NULL); CREATE TABLE events (id TEXT PRIMARY KEY, seq INTEGER, kind TEXT NOT NULL, aggregate_type TEXT NOT NULL DEFAULT '', aggregate_id TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL DEFAULT '', causation_id TEXT NOT NULL DEFAULT '', worker_ref TEXT NOT NULL DEFAULT '', attempt_id TEXT NOT NULL DEFAULT '', runtime_session_id TEXT NOT NULL DEFAULT '', payload_json TEXT NOT NULL, created_at TEXT NOT NULL); INSERT INTO secretary_capabilities VALUES ('cap-1', 'person-1', 'hash', '2024-01-01T00:00:00Z'); INSERT INTO events VALUES ('event-1', 1, 'worker.completed', 'worker', 'worker-1', 'legacy', '', '', 'worker-1', '', '', '{"worker_ref":"worker-1"}', '2024-01-01T00:01:00Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := fileDigest(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "copy", "secretary.db")
	if _, err := Run(ctx, Options{SourcePath: source, DestinationPath: destination}); err != nil {
		t.Fatal(err)
	}
	after, err := fileDigest(source)
	if err != nil || before != after {
		t.Fatalf("source fixture changed: before=%q after=%q err=%v", before, after, err)
	}
	var capabilityCount, eventCount, childWorkers int
	if err := queryOne(destination, `SELECT COUNT(*) FROM secretary_capabilities`, &capabilityCount); err != nil {
		t.Fatal(err)
	}
	if err := queryOne(destination, `SELECT COUNT(*) FROM events`, &eventCount); err != nil {
		t.Fatal(err)
	}
	if err := queryOne(destination, `SELECT COUNT(*) FROM workers WHERE worker_ref = 'child-worker-1'`, &childWorkers); err != nil {
		t.Fatal(err)
	}
	if capabilityCount != 1 || eventCount != 1 || childWorkers != 0 {
		t.Fatalf("realistic fixture projection lost data: capabilities=%d events=%d childWorkers=%d", capabilityCount, eventCount, childWorkers)
	}
}

func TestMigrationDerivesOnlyValidFinalAttemptResult(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "secretary.db")
	if err := seedPhase3Database(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = 'completed' WHERE id = 'task-1'; DELETE FROM results WHERE id = 'result-1'; UPDATE attempts SET state = 'succeeded' WHERE id = 'attempt-2'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: path, DestinationPath: path}); err != nil {
		t.Fatal(err)
	}
	var turnState, resultID, resultAttempt, resultStatus, summary string
	if err := queryOne(path, `SELECT state, COALESCE(result_id, '') FROM turns WHERE id = 'legacy-turn-binding-1'`, &turnState, &resultID); err != nil {
		t.Fatal(err)
	}
	if err := queryOne(path, `SELECT attempt_id, status, summary FROM phase4_results WHERE turn_id = 'legacy-turn-binding-1'`, &resultAttempt, &resultStatus, &summary); err != nil {
		t.Fatal(err)
	}
	if turnState != "succeeded" || resultID == "" || resultAttempt != "legacy-attempt-attempt-2" || resultStatus != "succeeded" || summary == "" {
		t.Fatalf("final result was not derived from final attempt: state=%q result=%q attempt=%q status=%q summary=%q", turnState, resultID, resultAttempt, resultStatus, summary)
	}
}

func TestMigrationNeverMarksCompletedTurnSucceededWithoutResult(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	if err := seedPhase3Database(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = 'completed' WHERE id = 'task-1'; DELETE FROM results WHERE attempt_id IN ('attempt-1', 'attempt-2'); DELETE FROM attempts WHERE worker_binding_id = 'binding-1'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: path, DestinationPath: path}); err != nil {
		t.Fatal(err)
	}
	var state, resultID string
	if err := queryOne(path, `SELECT state, COALESCE(result_id, '') FROM turns WHERE id = 'legacy-turn-binding-1'`, &state, &resultID); err != nil {
		t.Fatal(err)
	}
	if state != "interrupted" || resultID == "" {
		t.Fatalf("terminal Turn must have interrupted Result: state=%q result=%q", state, resultID)
	}
	var resultAttempt, resultStatus string
	if err := queryOne(path, `SELECT attempt_id, status FROM phase4_results WHERE turn_id = 'legacy-turn-binding-1'`, &resultAttempt, &resultStatus); err != nil {
		t.Fatal(err)
	}
	if resultAttempt != "legacy-attempt-binding-1-missing" || resultStatus != "interrupted" {
		t.Fatalf("derived terminal result=%q %q", resultAttempt, resultStatus)
	}
}

func TestMigrationSelectsFinalResultAfterRetryableHistory(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "secretary.db")
	if err := seedPhase3Database(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET state = 'completed' WHERE id = 'task-1'; UPDATE attempts SET state = 'failed' WHERE id = 'attempt-2'; INSERT INTO results VALUES ('result-2', 'attempt-2', 'failed', 'final failure', '2024-01-01T00:01:10Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: path, DestinationPath: path}); err != nil {
		t.Fatal(err)
	}
	var resultAttempt, resultStatus, summary string
	if err := queryOne(path, `SELECT attempt_id, status, summary FROM phase4_results WHERE turn_id = 'legacy-turn-binding-1'`, &resultAttempt, &resultStatus, &summary); err != nil {
		t.Fatal(err)
	}
	if resultAttempt != "legacy-attempt-attempt-2" || resultStatus != "failed" || summary != "final failure" {
		t.Fatalf("wrong final result=%q %q %q", resultAttempt, resultStatus, summary)
	}
	var priorClassification, finalClassification string
	if err := queryOne(path, `SELECT classification FROM phase4_attempt_outcomes WHERE attempt_id = 'legacy-attempt-attempt-1'`, &priorClassification); err != nil {
		t.Fatal(err)
	}
	if err := queryOne(path, `SELECT classification FROM phase4_attempt_outcomes WHERE attempt_id = 'legacy-attempt-attempt-2'`, &finalClassification); err != nil {
		t.Fatal(err)
	}
	if priorClassification != "retryable" || finalClassification != "final" {
		t.Fatalf("retry history classification prior=%q final=%q", priorClassification, finalClassification)
	}
}

func TestInvalidSchemaMigrationRestoresActiveDatabaseFromBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE persons (id TEXT PRIMARY KEY, created_at TEXT NOT NULL); INSERT INTO persons VALUES ('person-1', '2024-01-01T00:00:00Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: path, DestinationPath: path}); err == nil {
		t.Fatal("invalid schema migration unexpectedly succeeded")
	}
	after, err := fileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("active database changed after schema rollback")
	}
	if _, err := os.Stat(path + ".backup"); err != nil {
		t.Fatalf("schema backup missing: %v", err)
	}
}

func TestManualCopyMigrationIncludesSQLiteWALState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "secretary.db")
	if err := seedPhase3Database(ctx, source); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL; INSERT INTO conversation_entries VALUES ('entry-wal', 'conversation-1', 3, 'secretary_reply', 'written in WAL', '2024-01-01T00:02:00Z')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "copy", "secretary.db")
	if err := fixtureCopyFile(source, destination); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: destination, DestinationPath: destination}); err != nil {
		t.Fatal(err)
	}
	var copied int
	if err := queryOne(destination, `SELECT COUNT(*) FROM conversation_entries WHERE id = 'entry-wal'`, &copied); err != nil {
		t.Fatal(err)
	}
	if copied != 1 {
		t.Fatal("manual copy dropped committed WAL row")
	}
}

func queryOne(path, query string, destinations ...any) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.QueryRow(query).Scan(destinations...)
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
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar, sidecarErr := os.ReadFile(from + suffix)
		if errors.Is(sidecarErr, os.ErrNotExist) {
			continue
		}
		if sidecarErr != nil {
			return sidecarErr
		}
		if err := os.WriteFile(to+suffix, sidecar, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func TestMigrationPinsLegacyAliasesInWorkerAndProjectSnapshots(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	if err := seedPhase3Database(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE worker_bindings SET model = 'smart'; UPDATE worker_bindings SET runtime = 'codex' WHERE id = 'binding-1'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: path, DestinationPath: path}); err != nil {
		t.Fatal(err)
	}
	var policy, project string
	if err := queryOne(path, `SELECT policy_snapshot, project_snapshot FROM workers WHERE worker_ref = 'worker-1'`, &policy, &project); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(policy), `"model":"smart"`) || strings.Contains(strings.ToLower(project), `"model":"smart"`) || strings.Contains(strings.ToLower(project), `"model_id":"smart"`) {
		t.Fatalf("legacy model alias survived snapshots: policy=%s project=%s", policy, project)
	}
	var model string
	if err := queryOne(path, `SELECT json_extract(project_snapshot, '$.policy.model_id') FROM workers WHERE worker_ref = 'worker-1'`, &model); err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.6-luna" {
		t.Fatalf("adapter default model pin=%q", model)
	}
}

func TestMigrationPreservesFollowUpHistoryAsDistinctTurns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	if err := seedPhase3Database(ctx, path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO conversation_entries VALUES ('entry-followup-1', 'conversation-1', 3, 'worker_input', 'continue tests', '2024-01-01T00:01:20Z');
INSERT INTO conversation_entries VALUES ('entry-followup-2', 'conversation-1', 4, 'worker_input', 'then update docs', '2024-01-01T00:01:40Z');
INSERT INTO attempts VALUES ('attempt-3', 'binding-1', 3, 'succeeded', '2024-01-01T00:01:30Z', '2024-01-01T00:01:35Z');
INSERT INTO attempts VALUES ('attempt-4', 'binding-1', 4, 'failed', '2024-01-01T00:01:50Z', '2024-01-01T00:01:55Z');
INSERT INTO results VALUES ('result-3', 'attempt-3', 'succeeded', 'tests done', '2024-01-01T00:01:35Z');
INSERT INTO results VALUES ('result-4', 'attempt-4', 'failed', 'docs failed', '2024-01-01T00:01:55Z');`)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, Options{SourcePath: path, DestinationPath: path}); err != nil {
		t.Fatal(err)
	}
	var turns int
	if err := queryOne(path, `SELECT COUNT(*) FROM turns WHERE worker_id = 'legacy-worker-binding-1'`, &turns); err != nil {
		t.Fatal(err)
	}
	if turns != 3 {
		t.Fatalf("follow-up directions collapsed into %d turns", turns)
	}
	rows, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	resultRows, err := rows.QueryContext(ctx, `SELECT t.input, r.attempt_id, r.status FROM turns t JOIN phase4_results r ON r.turn_id = t.id WHERE t.worker_id = 'legacy-worker-binding-1' ORDER BY t.created_at, t.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer resultRows.Close()
	want := []struct{ input, attempt, status string }{
		{"ship it", "legacy-attempt-attempt-2", "interrupted"},
		{"continue tests", "legacy-attempt-attempt-3", "succeeded"},
		{"then update docs", "legacy-attempt-attempt-4", "failed"},
	}
	for _, expected := range want {
		if !resultRows.Next() {
			t.Fatalf("missing migrated turn for %#v", expected)
		}
		var input, attempt, status string
		if err := resultRows.Scan(&input, &attempt, &status); err != nil {
			t.Fatal(err)
		}
		if input != expected.input || attempt != expected.attempt || status != expected.status {
			t.Fatalf("migrated turn=%q result=%q/%q, want %#v", input, attempt, status, expected)
		}
	}
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
INSERT INTO worker_bindings VALUES ('binding-child', 'task-child', 'child-worker-1', 'node-1', 'native-child-session', '/work/project/child', 'binding-1', 'attempt-2', 'cfg-child', 'child', 'child-hash', 'fx', 'gpt-5.6-luna', 'high', 'read', 'metadata', 0, '2024-01-01T00:01:00Z');
INSERT INTO attempts VALUES ('attempt-1', 'binding-1', 1, 'succeeded', '2024-01-01T00:00:00Z', '2024-01-01T00:00:10Z');
INSERT INTO attempts VALUES ('attempt-2', 'binding-1', 2, 'active', '2024-01-01T00:01:00Z', '2024-01-01T00:01:10Z');
INSERT INTO results VALUES ('result-1', 'attempt-1', 'succeeded', 'done', '2024-01-01T00:00:10Z');
`)
	return err
}
