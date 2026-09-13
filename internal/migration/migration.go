// Package migration upgrades the durable Phase 3 SQLite state to the Phase 4
// Worker-first schema. It deliberately does not use core.Open: opening a
// legacy database must not first manufacture partially migrated state.
package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/config"
	"github.com/pelletier/go-toml/v2"

	_ "modernc.org/sqlite"
)

type Options struct {
	SourcePath                  string
	DestinationPath             string
	ConfigPath                  string
	SourceUserDocumentPath      string
	DestinationUserDocumentPath string
}

type Report struct {
	BackupPath string
	Workers    int
	Turns      int
	Attempts   int
	Outcomes   int
	Results    int
	Idempotent bool
}

var ErrInvalid = errors.New("migration: invalid input")

func Run(ctx context.Context, options Options) (Report, error) {
	if strings.TrimSpace(options.SourcePath) == "" {
		return Report{}, fmt.Errorf("%w: source database is required", ErrInvalid)
	}
	if options.DestinationPath == "" {
		options.DestinationPath = options.SourcePath
	}
	if options.SourceUserDocumentPath == "" {
		candidate := filepath.Join(filepath.Dir(options.SourcePath), "user.md")
		if _, err := os.Stat(candidate); err == nil {
			options.SourceUserDocumentPath = candidate
		}
	}
	if options.SourceUserDocumentPath != "" && options.DestinationUserDocumentPath == "" {
		options.DestinationUserDocumentPath = filepath.Join(filepath.Dir(options.DestinationPath), "user.md")
	}
	if _, err := os.Stat(options.SourcePath); err != nil {
		return Report{}, fmt.Errorf("%w: source database: %w", ErrInvalid, err)
	}

	if options.SourcePath != options.DestinationPath {
		if err := copyDatabase(options.SourcePath, options.DestinationPath); err != nil {
			return Report{}, err
		}
	}
	backupPath := options.DestinationPath + ".backup"
	if err := backupDatabase(options.DestinationPath, backupPath); err != nil {
		return Report{}, err
	}
	report := Report{BackupPath: backupPath}

	canonicalConfig, err := prepareConfig(options.ConfigPath)
	if err != nil {
		return report, err
	}
	userCopy, err := prepareUserCopy(options)
	if err != nil {
		return report, err
	}

	db, err := sql.Open("sqlite", options.DestinationPath)
	if err != nil {
		return report, fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000`); err != nil {
		return report, err
	}
	migrated, err := migrateDatabase(ctx, db)
	rollback := func() {
		_ = db.Close()
		_ = restoreFile(backupPath, options.DestinationPath)
	}
	if err != nil {
		rollback()
		return report, err
	}
	report.Workers, report.Turns, report.Attempts, report.Outcomes, report.Results = migrated.Workers, migrated.Turns, migrated.Attempts, migrated.Outcomes, migrated.Results
	report.Idempotent = migrated.Idempotent
	if canonicalConfig != nil {
		if err := atomicWrite(options.ConfigPath, canonicalConfig, 0o600); err != nil {
			rollback()
			return report, fmt.Errorf("write migrated config: %w", err)
		}
	}
	if userCopy != nil {
		if err := installUserCopy(userCopy); err != nil {
			rollback()
			return report, err
		}
	}
	return report, nil
}

func Migrate(ctx context.Context, sourcePath string) (Report, error) {
	return Run(ctx, Options{SourcePath: sourcePath, DestinationPath: sourcePath})
}

type userCopy struct {
	destination string
	content     []byte
	revision    []byte
}

func prepareUserCopy(options Options) (*userCopy, error) {
	if options.SourceUserDocumentPath == "" {
		return nil, nil
	}
	content, err := os.ReadFile(options.SourceUserDocumentPath)
	if err != nil {
		return nil, fmt.Errorf("read source user.md: %w", err)
	}
	revision, err := os.ReadFile(options.SourceUserDocumentPath + ".revision")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read source user.md revision: %w", err)
	}
	if len(revision) == 0 {
		revision = []byte("1\n")
	}
	if parsed, parseErr := strconv.ParseInt(strings.TrimSpace(string(revision)), 10, 64); parseErr != nil || parsed < 1 {
		return nil, fmt.Errorf("%w: invalid user.md revision", ErrInvalid)
	}
	return &userCopy{destination: options.DestinationUserDocumentPath, content: content, revision: revision}, nil
}

func installUserCopy(copy *userCopy) error {
	if err := atomicWrite(copy.destination, copy.content, 0o600); err != nil {
		return fmt.Errorf("write migrated user.md: %w", err)
	}
	if err := atomicWrite(copy.destination+".revision", copy.revision, 0o600); err != nil {
		return fmt.Errorf("write migrated user.md revision: %w", err)
	}
	return nil
}

func prepareConfig(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read migration config: %w", err)
	}
	if _, err := config.Load(path); err != nil {
		return nil, fmt.Errorf("%w: config validation failed: %v", ErrInvalid, err)
	}
	var tree map[string]any
	if err := toml.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("%w: parse migration config: %v", ErrInvalid, err)
	}
	secretary := table(tree, "secretary")
	workerPolicy := table(tree, "worker_policy")
	runtime := table(tree, "runtime")
	models := table(tree, "models")
	if stringValue(secretary["harness"]) == "" {
		secretary["harness"] = runtime["harness"]
	}
	if stringValue(secretary["model"]) == "" {
		secretary["model"] = models["secretary"]
	}
	if stringValue(secretary["reasoning"]) == "" {
		secretary["reasoning"] = runtime["reasoning"]
	}
	if stringValue(workerPolicy["default_harness"]) == "" {
		workerPolicy["default_harness"] = runtime["harness"]
	}
	for _, alias := range []string{"fast", "smart", "cheap"} {
		if stringValue(models[alias]) == alias {
			models[alias] = "default"
		}
	}
	delete(tree, "runtime")
	tree["secretary"] = secretary
	tree["worker_policy"] = workerPolicy
	tree["models"] = models
	canonical, err := toml.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("encode migrated config: %w", err)
	}
	return canonical, nil
}

func table(tree map[string]any, key string) map[string]any {
	if value, ok := tree[key].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func migrateDatabase(ctx context.Context, db *sql.DB) (Report, error) {
	if err := installSchema(ctx, db); err != nil {
		return Report{}, err
	}
	if err := ensureSecretaryIdentity(ctx, db); err != nil {
		return Report{}, err
	}
	var already int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM phase4_migration_meta WHERE key = 'phase3_complete'`).Scan(&already); err != nil {
		return Report{}, err
	}
	if already > 0 {
		return Report{Idempotent: true}, nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()
	var report Report
	rows, err := tx.QueryContext(ctx, `SELECT t.id, t.conversation_id, t.text, t.state, t.created_at, t.updated_at,
		w.id, w.worker_ref, w.node_id, w.runtime_session_id, w.workspace, w.profile_version, w.profile_name, w.profile_hash,
		w.runtime, w.model, w.reasoning, w.allow_tools, w.profile_delivery, w.archived
		FROM tasks t JOIN worker_bindings w ON w.task_id = t.id ORDER BY t.created_at, t.id`)
	if err != nil {
		return Report{}, fmt.Errorf("read Phase 3 bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, conversationID, text, taskState, taskCreated, taskUpdated string
		var bindingID, workerRef, nodeID, nativeSession, workspace, profileVersion, profileName, profileHash, runtime, model, reasoning, allowTools, delivery string
		var archived int
		if err := rows.Scan(&taskID, &conversationID, &text, &taskState, &taskCreated, &taskUpdated, &bindingID, &workerRef, &nodeID, &nativeSession, &workspace, &profileVersion, &profileName, &profileHash, &runtime, &model, &reasoning, &allowTools, &delivery, &archived); err != nil {
			return Report{}, err
		}
		var existing string
		if err := tx.QueryRowContext(ctx, `SELECT worker_id FROM phase4_migration_records WHERE legacy_task_id = ?`, taskID).Scan(&existing); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Report{}, err
		}
		workerID := "legacy-worker-" + bindingID
		turnID := "legacy-turn-" + bindingID
		if err := insertWorker(ctx, tx, workerID, workerRef, conversationID, text, nodeID, workspace, profileVersion, profileName, profileHash, runtime, model, reasoning, allowTools, delivery, taskState, taskCreated, taskUpdated); err != nil {
			return Report{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_migration_records(legacy_task_id, legacy_binding_id, worker_id, turn_id) VALUES(?, ?, ?, ?)`, taskID, bindingID, workerID, turnID); err != nil {
			return Report{}, err
		}
		if err := insertTurn(ctx, tx, turnID, workerID, text, taskState, taskCreated, taskUpdated); err != nil {
			return Report{}, err
		}
		report.Workers++
		report.Turns++
		if err := migrateAttempts(ctx, tx, bindingID, workerID, turnID, nativeSession, nodeID, workspace, conversationID, workerRef, taskState, &report); err != nil {
			return Report{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return Report{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_migration_meta(key, value) VALUES('phase3_complete', '1')`); err != nil {
		return Report{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at) VALUES('phase4.migration_complete', '1', ?) ON CONFLICT(key) DO UPDATE SET value = '1', updated_at = excluded.updated_at`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Report{}, err
	}
	if err := tx.Commit(); err != nil {
		return Report{}, err
	}
	return report, nil
}

func insertWorker(ctx context.Context, tx *sql.Tx, id, ref, conversation, intent, node, workspace, version, name, hash, runtime, model, reasoning, tools, delivery, taskState, created, updated string) error {
	status := "idle"
	if taskState == "closed" || taskState == "closing" {
		status = "closed"
	} else if taskState == "dispatching" || taskState == "dispatch_failed" {
		status = "queued"
	}
	projectSnapshot, _ := json.Marshal(map[string]string{"legacy_task_state": taskState, "workspace": workspace, "profile_version": version, "profile_name": name, "profile_hash": hash, "runtime": runtime, "model": model, "reasoning": reasoning, "allow_tools": tools, "profile_delivery": delivery})
	_, err := tx.ExecContext(ctx, `INSERT INTO workers(id, worker_ref, conversation_id, title, intent, project_id, node_id, harness_instance_id, policy_snapshot, project_snapshot, workspace, status, archived, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, ref, conversation, intent, intent, "legacy-project-"+node, node, "legacy-"+runtime, string(projectSnapshot), string(projectSnapshot), workspace, status, archivedValue(taskState), created, updated)
	return err
}

func archivedValue(state string) int {
	if state == "closed" {
		return 1
	}
	return 0
}

func insertTurn(ctx context.Context, tx *sql.Tx, id, workerID, input, state, created, updated string) error {
	turnState := "succeeded"
	switch state {
	case "dispatching", "dispatch_failed":
		turnState = "queued"
	case "open", "closing":
		turnState = "active"
	case "closed":
		turnState = "succeeded"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO turns(id, worker_id, input, normalized_intent, context_snapshot, state, created_at, updated_at) VALUES(?, ?, ?, '', '', ?, ?, ?)`, id, workerID, input, turnState, created, updated)
	return err
}

func migrateAttempts(ctx context.Context, tx *sql.Tx, bindingID, workerID, turnID, nativeSession, nodeID, workspace, conversationID, workerRef, taskState string, report *Report) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, number, state, created_at, updated_at FROM attempts WHERE worker_binding_id = ? ORDER BY number`, bindingID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type oldAttempt struct {
		id                      string
		number                  int
		state, created, updated string
	}
	var attempts []oldAttempt
	for rows.Next() {
		var item oldAttempt
		if err := rows.Scan(&item.id, &item.number, &item.state, &item.created, &item.updated); err != nil {
			return err
		}
		attempts = append(attempts, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	interrupted := false
	for index, old := range attempts {
		attemptID := "legacy-attempt-" + old.id
		state := old.state
		if state == "starting" || state == "active" {
			interrupted = true
			// The native ID is intentionally not persisted. Without a Node-local
			// proof, resuming would be a silent and unsafe execution fork.
			state = "interrupted"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, attemptID, workerID, turnID, old.number, nodeID, "legacy-harness-"+nodeID, state, turnID, old.created, old.updated); err != nil {
			return err
		}
		classification := "final"
		if index < len(attempts)-1 {
			classification = "retryable"
		}
		status := state
		if status != "succeeded" && status != "failed" && status != "canceled" && status != "interrupted" {
			status = "interrupted"
			classification = "final"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempt_outcomes(id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at) VALUES(?, ?, ?, ?, '', '', ?, ?)`, "legacy-outcome-"+old.id, attemptID, status, classification, `{"legacy_attempt_id":"`+old.id+`"}`, old.updated); err != nil {
			return err
		}
		report.Attempts++
		report.Outcomes++
		var resultID, resultStatus, summary, created string
		err := tx.QueryRowContext(ctx, `SELECT id, status, summary, created_at FROM results WHERE attempt_id = ?`, old.id).Scan(&resultID, &resultStatus, &summary, &created)
		if err == nil {
			var existingResult string
			existingErr := tx.QueryRowContext(ctx, `SELECT id FROM phase4_results WHERE turn_id = ?`, turnID).Scan(&existingResult)
			if errors.Is(existingErr, sql.ErrNoRows) {
				phase4Status := resultStatus
				if phase4Status != "succeeded" && phase4Status != "failed" && phase4Status != "canceled" {
					phase4Status = "failed"
				}
				newResultID := "legacy-result-" + resultID
				if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_results(id, worker_id, turn_id, attempt_id, status, summary, failure_code, artifact_refs, correlation_id, created_at) VALUES(?, ?, ?, ?, ?, ?, '', '', ?, ?)`, newResultID, workerID, turnID, attemptID, phase4Status, summary, turnID, created); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE turns SET result_id = ?, state = ? WHERE id = ?`, newResultID, phase4TurnState(phase4Status), turnID); err != nil {
					return err
				}
				if err := attachVisibleResult(ctx, tx, conversationID, workerRef, turnID, newResultID, summary); err != nil {
					return err
				}
				report.Results++
			} else if existingErr != nil {
				return existingErr
			}
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if len(attempts) > 0 {
		currentAttemptID := "legacy-attempt-" + attempts[len(attempts)-1].id
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET current_attempt_id = ? WHERE id = ?`, currentAttemptID, turnID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET current_turn_id = ? WHERE id = ?`, turnID, workerID); err != nil {
			return err
		}
	}
	if interrupted {
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = 'interrupted' WHERE id = ?`, turnID); err != nil {
			return err
		}
	}
	return nil
}

func phase4TurnState(status string) string {
	switch status {
	case "succeeded":
		return "succeeded"
	case "canceled":
		return "canceled"
	default:
		return "failed"
	}
}

func attachVisibleResult(ctx context.Context, tx *sql.Tx, conversationID, workerRef, turnID, resultID, summary string) error {
	var entryID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM conversation_entries WHERE conversation_id = ? AND kind = 'worker_result' AND body = ? AND result_id = '' ORDER BY seq LIMIT 1`, conversationID, summary).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		var seq int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM conversation_entries WHERE conversation_id = ?`, conversationID).Scan(&seq); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO conversation_entries(id, conversation_id, seq, kind, body, worker_ref, turn_id, result_id, created_at) VALUES(?, ?, ?, 'worker_result', ?, ?, ?, ?, ?)`, "legacy-entry-"+resultID, conversationID, seq, summary, workerRef, turnID, resultID, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE conversation_entries SET worker_ref = ?, turn_id = ?, result_id = ? WHERE id = ?`, workerRef, turnID, resultID, entryID)
	return err
}

func ensureSecretaryIdentity(ctx context.Context, db *sql.DB) error {
	var personID, conversationID string
	err := db.QueryRowContext(ctx, `SELECT p.id, c.id FROM persons p JOIN conversations c ON c.person_id = p.id ORDER BY p.created_at, p.id LIMIT 1`).Scan(&personID, &conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: database has no Person and Personal Conversation", ErrInvalid)
	}
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO secretary_identities(id, person_id, conversation_id, runtime_generation, runtime_harness, runtime_model, runtime_reasoning, created_at, updated_at) VALUES(?, ?, ?, 0, '', '', '', ?, ?)`, "legacy-secretary-"+personID, personID, conversationID, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func installSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS secretary_identities (id TEXT PRIMARY KEY, person_id TEXT NOT NULL UNIQUE, conversation_id TEXT NOT NULL UNIQUE, runtime_generation INTEGER NOT NULL DEFAULT 0, runtime_harness TEXT NOT NULL DEFAULT '', runtime_model TEXT NOT NULL DEFAULT '', runtime_reasoning TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS workers (id TEXT PRIMARY KEY, worker_ref TEXT NOT NULL UNIQUE, conversation_id TEXT NOT NULL, title TEXT NOT NULL, intent TEXT NOT NULL, project_id TEXT NOT NULL, node_id TEXT NOT NULL, harness_instance_id TEXT NOT NULL, policy_snapshot TEXT NOT NULL, project_snapshot TEXT NOT NULL DEFAULT '', workspace TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, current_turn_id TEXT, last_result_summary TEXT NOT NULL DEFAULT '', archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, closed_at TEXT);
CREATE TABLE IF NOT EXISTS turns (id TEXT PRIMARY KEY, worker_id TEXT NOT NULL, input TEXT NOT NULL, normalized_intent TEXT NOT NULL DEFAULT '', context_snapshot TEXT NOT NULL DEFAULT '', state TEXT NOT NULL, current_attempt_id TEXT, result_id TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS phase4_attempts (id TEXT PRIMARY KEY, worker_id TEXT NOT NULL, turn_id TEXT NOT NULL, number INTEGER NOT NULL, node_id TEXT NOT NULL, harness_instance_id TEXT NOT NULL, state TEXT NOT NULL, correlation_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(turn_id, number));
CREATE TABLE IF NOT EXISTS phase4_attempt_outcomes (id TEXT PRIMARY KEY, attempt_id TEXT NOT NULL UNIQUE, status TEXT NOT NULL, classification TEXT NOT NULL, error_code TEXT NOT NULL DEFAULT '', error_message TEXT NOT NULL DEFAULT '', diagnostics TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS phase4_results (id TEXT PRIMARY KEY, worker_id TEXT NOT NULL, turn_id TEXT NOT NULL UNIQUE, attempt_id TEXT NOT NULL UNIQUE, status TEXT NOT NULL, summary TEXT NOT NULL, failure_code TEXT NOT NULL DEFAULT '', artifact_refs TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS phase4_migration_records (legacy_task_id TEXT PRIMARY KEY, legacy_binding_id TEXT NOT NULL UNIQUE, worker_id TEXT NOT NULL UNIQUE, turn_id TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS phase4_migration_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);
`)
	if err != nil {
		return fmt.Errorf("create Phase 4 migration schema: %w", err)
	}
	for _, item := range []struct{ table, column string }{
		{"conversation_entries", "worker_ref TEXT NOT NULL DEFAULT ''"},
		{"conversation_entries", "turn_id TEXT NOT NULL DEFAULT ''"},
		{"conversation_entries", "result_id TEXT NOT NULL DEFAULT ''"},
	} {
		if _, err := db.ExecContext(ctx, `ALTER TABLE `+item.table+` ADD COLUMN `+item.column); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("add migration column %s: %w", item.column, err)
		}
	}
	return nil
}

func backupDatabase(path, backup string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if _, err := os.Stat(backup); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := snapshotDatabase(path, backup); err == nil {
		return nil
	}
	return copyFile(path, backup)
}

func copyDatabase(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := snapshotDatabase(source, destination); err == nil {
		return nil
	}
	return copyFile(source, destination)
}

func snapshotDatabase(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tmp := destination + ".snapshot"
	_ = os.Remove(tmp)
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return err
	}
	_, err = db.Exec(`PRAGMA busy_timeout = 5000; VACUUM INTO ` + sqliteStringLiteral(tmp))
	closeErr := db.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func restoreFile(source, destination string) error { return copyFile(source, destination) }

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".migration-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type _ = time.Time
