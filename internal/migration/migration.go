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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
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
	rollbackPath := options.DestinationPath + ".rollback"
	if err := saveRollbackDatabase(options.DestinationPath, rollbackPath); err != nil {
		return Report{}, err
	}
	defer removeDatabaseFiles(rollbackPath)
	report := Report{BackupPath: backupPath}
	configBackupPath := ""
	if options.ConfigPath != "" {
		configBackupPath = options.ConfigPath + ".backup"
		if err := backupFileIfMissing(options.ConfigPath, configBackupPath); err != nil {
			return report, err
		}
	}

	canonicalConfig, legacyModels, err := prepareConfig(options.ConfigPath)
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
	migrated, err := migrateDatabase(ctx, db, legacyModels)
	rollback := func() {
		_ = db.Close()
		_ = restoreRollbackDatabase(rollbackPath, options.DestinationPath)
		if configBackupPath != "" {
			_ = restoreFile(configBackupPath, options.ConfigPath)
		}
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

func prepareConfig(path string) ([]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read migration config: %w", err)
	}
	if _, err := config.Load(path); err != nil {
		return nil, nil, fmt.Errorf("%w: config validation failed: %v", ErrInvalid, err)
	}
	var tree map[string]any
	if err := toml.Unmarshal(raw, &tree); err != nil {
		return nil, nil, fmt.Errorf("%w: parse migration config: %v", ErrInvalid, err)
	}
	secretary := table(tree, "secretary")
	workerPolicy := table(tree, "worker_policy")
	runtime := table(tree, "runtime")
	models := table(tree, "models")
	if stringValue(secretary["harness"]) == "" {
		secretary["harness"] = runtime["harness"]
	}
	secretaryModel := canonicalLegacyModel(stringValue(models["secretary"]), "default")
	if stringValue(secretary["model"]) == "" {
		secretary["model"] = secretaryModel
	}
	if stringValue(secretary["reasoning"]) == "" {
		secretary["reasoning"] = runtime["reasoning"]
	}
	if stringValue(workerPolicy["default_harness"]) == "" {
		workerPolicy["default_harness"] = runtime["harness"]
	}
	workerDefaultModel := stringValue(secretary["model"])
	if workerDefaultModel == "" {
		workerDefaultModel = secretaryModel
	}
	if stringValue(workerPolicy["model"]) == "" {
		workerPolicy["model"] = canonicalLegacyModel(stringValue(models["smart"]), workerDefaultModel)
	}
	fallbackModels := []any{}
	for _, alias := range []string{"fast", "cheap"} {
		if value := stringValue(models[alias]); value != "" {
			fallbackModels = append(fallbackModels, canonicalLegacyModel(value, secretaryModel))
		}
	}
	if len(fallbackModels) > 0 {
		workerPolicy["fallback_models"] = fallbackModels
	}
	legacyModels := map[string]string{}
	for _, alias := range []string{"secretary", "fast", "smart", "cheap"} {
		if value := stringValue(models[alias]); value != "" {
			legacyModels[alias] = value
		}
	}
	legacyModels["worker_policy"] = stringValue(workerPolicy["model"])
	for _, alias := range []string{"fast", "smart", "cheap"} {
		delete(models, alias)
	}
	delete(tree, "runtime")
	tree["secretary"] = secretary
	tree["worker_policy"] = workerPolicy
	delete(tree, "models")
	canonical, err := toml.Marshal(tree)
	if err != nil {
		return nil, nil, fmt.Errorf("encode migrated config: %w", err)
	}
	return canonical, legacyModels, nil
}

func canonicalLegacyModel(value, fallback string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "fast", "smart", "cheap", "default":
		return fallback
	default:
		return value
	}
}

func resolveLegacyModel(value string, models map[string]string, runtime string) string {
	value = strings.TrimSpace(value)
	for i := 0; i < 5; i++ {
		if value == "" {
			break
		}
		mapped, ok := models[strings.ToLower(value)]
		if !ok || strings.TrimSpace(mapped) == value {
			break
		}
		value = strings.TrimSpace(mapped)
	}
	if value == "" || isLegacyModelAlias(value) {
		if fallback := strings.TrimSpace(models["worker_policy"]); fallback != "" && !isLegacyModelAlias(fallback) {
			return fallback
		}
		return legacyAdapterDefaultModel(runtime)
	}
	return value
}

func isLegacyModelAlias(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "default", "fast", "smart", "cheap":
		return true
	default:
		return false
	}
}

func legacyAdapterDefaultModel(_ string) string { return "gpt-5.6-luna" }

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

func migrateDatabase(ctx context.Context, db *sql.DB, legacyModels map[string]string) (Report, error) {
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
	followUpCache := make(map[string]map[string][]legacyMigrationDirection)
	rows, err := tx.QueryContext(ctx, `SELECT t.id, t.conversation_id, t.text, t.state, t.created_at, t.updated_at,
		w.id, w.worker_ref, w.node_id, w.runtime_session_id, w.workspace, w.profile_version, w.profile_name, w.profile_hash,
		w.runtime, w.model, w.reasoning, w.allow_tools, w.profile_delivery, w.archived
		FROM tasks t JOIN worker_bindings w ON w.task_id = t.id
		WHERE COALESCE(t.parent_task_id, '') = '' AND COALESCE(w.parent_binding_id, '') = '' AND COALESCE(w.parent_attempt_id, '') = ''
		ORDER BY t.created_at, t.id`)
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
		followUps, ok := followUpCache[conversationID]
		if !ok {
			followUps, err = legacyFollowUpDirections(ctx, tx, conversationID)
			if err != nil {
				return Report{}, err
			}
			followUpCache[conversationID] = followUps
		}
		model = resolveLegacyModel(model, legacyModels, runtime)
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
		if err := migrateAttempts(ctx, tx, bindingID, workerID, turnID, nativeSession, nodeID, workspace, runtime, conversationID, workerRef, taskState, text, taskCreated, taskUpdated, followUps[workerRef], &report); err != nil {
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
	projectID := "legacy-project-" + node
	harness := legacyHarnessInstance(node, runtime, model, reasoning)
	project := core.ProjectSnapshot{ID: projectID, Name: projectID, Mappings: []core.ProjectPathMapping{{Node: core.NodeReference(node), Path: workspace}}, Policy: core.ProjectPolicy{ModelID: model, Reasoning: reasoning}, Revision: 1, Node: core.NodeReference(node), Workspace: workspace, HarnessInstance: harness}
	projectSnapshot, err := json.Marshal(project)
	if err != nil {
		return err
	}
	policySnapshot, err := json.Marshal(map[string]string{"legacy_task_state": taskState, "profile_version": version, "profile_name": name, "profile_hash": hash, "runtime": runtime, "model": model, "reasoning": reasoning, "allow_tools": tools, "profile_delivery": delivery})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workers(id, worker_ref, conversation_id, title, intent, project_id, node_id, harness_instance_id, policy_snapshot, project_snapshot, workspace, status, archived, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, ref, conversation, intent, intent, projectID, node, string(harness.ID), string(policySnapshot), string(projectSnapshot), workspace, status, archivedValue(taskState), created, updated)
	return err
}

func legacyHarnessInstance(node, runtime, model, reasoning string) core.HarnessInstance {
	kind := core.HarnessKind(strings.TrimSpace(runtime))
	switch kind {
	case core.HarnessClaudeCode, core.HarnessCodex, core.HarnessFX, core.HarnessOpenCode:
	default:
		kind = core.HarnessFX
	}
	instance := core.HarnessInstance{ID: core.HarnessInstanceID("legacy-" + strings.TrimSpace(runtime)), Node: core.NodeReference(node), Kind: kind, Version: "legacy", Status: core.HarnessUnavailable}
	if instance.ID == "legacy-" {
		instance.ID = "legacy-unavailable"
	}
	if model != "" {
		instance.ModelIDs = []core.ObservedModelID{core.ObservedModelID(model)}
	}
	if reasoning != "" {
		instance.ReasoningLevels = []core.ObservedReasoningLevel{core.ObservedReasoningLevel(reasoning)}
	}
	return instance
}

func archivedValue(state string) int {
	if state == "closed" {
		return 1
	}
	return 0
}

func insertTurn(ctx context.Context, tx *sql.Tx, id, workerID, input, state, created, updated string) error {
	turnState := "interrupted"
	switch state {
	case "dispatching", "dispatch_failed":
		turnState = "queued"
	case "open", "closing":
		turnState = "active"
	case "closed", "completed":
		// A terminal legacy Task is not evidence of a successful Turn. The
		// terminal Attempt projection below must supply exactly one Result.
		turnState = "interrupted"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO turns(id, worker_id, input, normalized_intent, context_snapshot, state, created_at, updated_at) VALUES(?, ?, ?, '', '', ?, ?, ?)`, id, workerID, input, turnState, created, updated)
	return err
}

type legacyMigrationAttempt struct {
	id                      string
	number                  int
	state, created, updated string
}

type legacyMigrationResult struct {
	id, status, summary, created string
}

type legacyMigrationDirection struct {
	id, entryID, input, created, updated string
}

func migrateAttempts(ctx context.Context, tx *sql.Tx, bindingID, workerID, turnID, nativeSession, nodeID, workspace, runtime, conversationID, workerRef, taskState, initialInput, initialCreated, initialUpdated string, followups []legacyMigrationDirection, report *Report) error {
	_ = nativeSession
	rows, err := tx.QueryContext(ctx, `SELECT id, number, state, created_at, updated_at FROM attempts WHERE worker_binding_id = ? ORDER BY number`, bindingID)
	if err != nil {
		return err
	}
	var attempts []legacyMigrationAttempt
	for rows.Next() {
		var item legacyMigrationAttempt
		if err := rows.Scan(&item.id, &item.number, &item.state, &item.created, &item.updated); err != nil {
			rows.Close()
			return err
		}
		attempts = append(attempts, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(attempts) == 0 && (taskState == "closed" || taskState == "completed") {
		attempts = append(attempts, legacyMigrationAttempt{id: bindingID + "-missing", number: 1, state: "interrupted", created: initialUpdated, updated: initialUpdated})
	}

	results := make(map[string]legacyMigrationResult)
	for _, old := range attempts {
		var result legacyMigrationResult
		resultErr := tx.QueryRowContext(ctx, `SELECT id, status, summary, created_at FROM results WHERE attempt_id = ?`, old.id).Scan(&result.id, &result.status, &result.summary, &result.created)
		if resultErr == nil {
			results[old.id] = result
		} else if !errors.Is(resultErr, sql.ErrNoRows) {
			return resultErr
		}
	}

	directions := []legacyMigrationDirection{{id: turnID, input: initialInput, created: initialCreated, updated: initialUpdated}}
	directions = append(directions, followups...)
	starts := make([]int, len(directions))
	starts[0] = 0
	for i := 1; i < len(directions); i++ {
		starts[i] = len(attempts)
		for j := starts[i-1] + 1; j < len(attempts); j++ {
			if attempts[j].created >= directions[i].created {
				starts[i] = j
				break
			}
		}
		if starts[i] == len(attempts) && i < len(attempts) {
			starts[i] = i
		}
	}
	for i, direction := range directions {
		if i > 0 {
			if err := insertTurn(ctx, tx, direction.id, workerID, direction.input, "queued", direction.created, direction.updated); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE conversation_entries SET worker_ref = ?, turn_id = ? WHERE id = ?`, workerRef, direction.id, direction.entryID); err != nil {
				return err
			}
			report.Turns++
		}
		start := starts[i]
		end := len(attempts)
		if i+1 < len(starts) && starts[i+1] < end {
			end = starts[i+1]
		}
		if start >= end {
			continue
		}
		for j := start; j < end; j++ {
			old := attempts[j]
			attemptID := "legacy-attempt-" + old.id
			state := migratedAttemptState(old.state)
			harnessID := legacyHarnessInstance(nodeID, runtime, "", "").ID
			if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempts(id, worker_id, turn_id, number, node_id, harness_instance_id, state, correlation_id, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, attemptID, workerID, direction.id, j-start+1, nodeID, harnessID, state, direction.id, old.created, old.updated); err != nil {
				return err
			}
			classification := "final"
			if j < end-1 {
				classification = "retryable"
			}
			status := state
			if status == "interrupted" {
				classification = "final"
			} else if !validAttemptResultState(status) {
				status = "interrupted"
				classification = "final"
			}
			errorCode := ""
			if status == "interrupted" {
				errorCode = "execution_state_unknown"
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_attempt_outcomes(id, attempt_id, status, classification, error_code, error_message, diagnostics, created_at) VALUES(?, ?, ?, ?, ?, '', ?, ?)`, "legacy-outcome-"+old.id, attemptID, status, classification, errorCode, `{"legacy_attempt_id":"`+old.id+`"}`, old.updated); err != nil {
				return err
			}
			report.Attempts++
			report.Outcomes++
		}
		final := attempts[end-1]
		finalState := migratedAttemptState(final.state)
		finalResult, hasResult := results[final.id]
		resultStatus := finalState
		summary := "legacy attempt " + final.id + " completed with status " + finalState
		created := final.updated
		if hasResult && validResultStatus(finalResult.status) {
			resultStatus, summary, created = finalResult.status, finalResult.summary, finalResult.created
		} else if !hasResult && finalState == "interrupted" {
			for j := end - 2; j >= start; j-- {
				if prior, ok := results[attempts[j].id]; ok && strings.TrimSpace(prior.summary) != "" {
					summary, created = prior.summary, prior.created
					break
				}
			}
		}
		if err := insertMigratedResult(ctx, tx, workerID, direction.id, conversationID, workerRef, "legacy-attempt-"+final.id, resultIDForMigration(finalResult, final.id), resultStatus, summary, created, report); err != nil {
			return err
		}
		currentAttemptID := "legacy-attempt-" + final.id
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET current_attempt_id = ? WHERE id = ?`, currentAttemptID, direction.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workers SET current_turn_id = ? WHERE id = ?`, direction.id, workerID); err != nil {
			return err
		}
	}
	return nil
}

func migratedAttemptState(state string) string {
	if state == "starting" || state == "active" {
		return "interrupted"
	}
	if validAttemptResultState(state) || state == "interrupted" {
		return state
	}
	return "interrupted"
}

type legacyFollowUpAttempt struct {
	id      string
	number  int
	created string
}

type legacyFollowUpWorker struct {
	workerRef    string
	taskCreated  string
	activities   []string
	nextAttempts []legacyFollowUpAttempt
}

// legacyFollowUpOwner uses the durable Phase 3 timeline when worker_ref was
// not stored on a conversation entry. A follow-up Attempt and its worker_input
// are created with the same now, so equality is a valid match. Each candidate
// Attempt is consumed once while unscoped entries are assigned in seq order.
// Equal-time candidates belonging to different roots are not a discriminator:
// fail closed instead of using worker_ref or conversation seq to guess.
func legacyFollowUpOwner(created string, workers []legacyFollowUpWorker, used map[string]struct{}) (string, error) {
	if len(workers) == 0 {
		return "", nil
	}
	bestCreated := ""
	bestWorker := -1
	bestAttempt := -1
	candidateWorkers := make(map[string]struct{})
	for i, worker := range workers {
		if worker.taskCreated > created {
			continue
		}
		for j, attempt := range worker.nextAttempts {
			if attempt.number <= 1 || attempt.created < created {
				continue
			}
			if _, ok := used[attempt.id]; ok {
				continue
			}
			if bestWorker < 0 || attempt.created < bestCreated {
				bestCreated = attempt.created
				bestWorker, bestAttempt = i, j
				candidateWorkers = map[string]struct{}{worker.workerRef: {}}
				continue
			}
			if attempt.created == bestCreated {
				candidateWorkers[worker.workerRef] = struct{}{}
				if attempt.number < workers[bestWorker].nextAttempts[bestAttempt].number {
					bestWorker, bestAttempt = i, j
				}
			}
		}
	}
	if bestWorker >= 0 {
		if len(candidateWorkers) > 1 {
			return "", fmt.Errorf("%w: ambiguous unbound follow-up at %s: equal-time attempts belong to root workers %s", ErrInvalid, created, strings.Join(sortedKeys(candidateWorkers), ", "))
		}
		used[workers[bestWorker].nextAttempts[bestAttempt].id] = struct{}{}
		return workers[bestWorker].workerRef, nil
	}
	bestAnchor := ""
	bestPrior := false
	anchorWorkers := make(map[string]struct{})
	for i, worker := range workers {
		anchor := worker.taskCreated
		for _, activity := range worker.activities {
			if activity <= created && activity > anchor {
				anchor = activity
			}
		}
		prior := anchor <= created
		better := bestWorker < 0 || prior && !bestPrior || prior == bestPrior && (prior && anchor > bestAnchor || !prior && anchor < bestAnchor)
		if better {
			bestWorker, bestAnchor, bestPrior = i, anchor, prior
			anchorWorkers = map[string]struct{}{worker.workerRef: {}}
		} else if bestWorker >= 0 && prior == bestPrior && anchor == bestAnchor {
			anchorWorkers[worker.workerRef] = struct{}{}
		}
	}
	if len(anchorWorkers) > 1 {
		return "", fmt.Errorf("%w: ambiguous unbound follow-up at %s: equal-time timeline anchors belong to root workers %s", ErrInvalid, created, strings.Join(sortedKeys(anchorWorkers), ", "))
	}
	return workers[bestWorker].workerRef, nil
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func legacyFollowUpDirections(ctx context.Context, tx *sql.Tx, conversationID string) (map[string][]legacyMigrationDirection, error) {
	workersRows, err := tx.QueryContext(ctx, `SELECT w.id, w.worker_ref, t.created_at
		FROM worker_bindings w JOIN tasks t ON t.id = w.task_id
		WHERE t.conversation_id = ? AND COALESCE(t.parent_task_id, '') = ''
			AND COALESCE(w.parent_binding_id, '') = '' AND COALESCE(w.parent_attempt_id, '') = ''
		ORDER BY w.worker_ref`, conversationID)
	if err != nil {
		return nil, err
	}
	workers := make([]legacyFollowUpWorker, 0)
	for workersRows.Next() {
		var worker legacyFollowUpWorker
		var bindingID string
		if err := workersRows.Scan(&bindingID, &worker.workerRef, &worker.taskCreated); err != nil {
			workersRows.Close()
			return nil, err
		}
		attemptRows, err := tx.QueryContext(ctx, `SELECT id, number, created_at, updated_at FROM attempts WHERE worker_binding_id = ? ORDER BY number`, bindingID)
		if err != nil {
			workersRows.Close()
			return nil, err
		}
		for attemptRows.Next() {
			var attempt legacyFollowUpAttempt
			var updated string
			if err := attemptRows.Scan(&attempt.id, &attempt.number, &attempt.created, &updated); err != nil {
				attemptRows.Close()
				workersRows.Close()
				return nil, err
			}
			worker.activities = append(worker.activities, updated)
			worker.nextAttempts = append(worker.nextAttempts, attempt)
		}
		if err := attemptRows.Close(); err != nil {
			workersRows.Close()
			return nil, err
		}
		if err := attemptRows.Err(); err != nil {
			workersRows.Close()
			return nil, err
		}
		workers = append(workers, worker)
	}
	if err := workersRows.Close(); err != nil {
		return nil, err
	}
	if err := workersRows.Err(); err != nil {
		return nil, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, body, worker_ref, created_at FROM conversation_entries
		WHERE conversation_id = ? AND kind IN ('worker_input', 'worker_follow_up', 'follow_up') ORDER BY seq, id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	directions := make(map[string][]legacyMigrationDirection)
	usedAttempts := make(map[string]struct{})
	for rows.Next() {
		var id, body, entryWorkerRef, created string
		if err := rows.Scan(&id, &body, &entryWorkerRef, &created); err != nil {
			return nil, err
		}
		owner := entryWorkerRef
		if owner == "" {
			var ownerErr error
			owner, ownerErr = legacyFollowUpOwner(created, workers, usedAttempts)
			if ownerErr != nil {
				return nil, fmt.Errorf("conversation %q follow-up ownership: %w", conversationID, ownerErr)
			}
		}
		if owner == "" {
			continue
		}
		directions[owner] = append(directions[owner], legacyMigrationDirection{id: "legacy-turn-" + strings.ReplaceAll(id, " ", "_"), entryID: id, input: body, created: created, updated: created})
	}
	return directions, rows.Err()
}

func resultIDForMigration(result legacyMigrationResult, finalAttemptID string) string {
	if result.id != "" {
		return result.id
	}
	return "legacy-derived-result-" + finalAttemptID
}

func validResultStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "canceled" || status == "interrupted"
}

func validAttemptResultState(state string) bool {
	return validResultStatus(state)
}

func insertMigratedResult(ctx context.Context, tx *sql.Tx, workerID, turnID, conversationID, workerRef, attemptID, resultID, status, summary, created string, report *Report) error {
	if !validResultStatus(status) {
		return nil
	}
	newResultID := "legacy-result-" + resultID
	failureCode := ""
	if status != "succeeded" {
		failureCode = status
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_results(id, worker_id, turn_id, attempt_id, status, summary, failure_code, artifact_refs, correlation_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, '', ?, ?)`, newResultID, workerID, turnID, attemptID, status, summary, failureCode, turnID, created); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET result_id = ?, state = ? WHERE id = ?`, newResultID, phase4TurnState(status), turnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workers SET last_result_summary = ? WHERE id = ?`, summary, workerID); err != nil {
		return err
	}
	if err := attachVisibleResult(ctx, tx, conversationID, workerRef, turnID, newResultID, summary); err != nil {
		return err
	}
	report.Results++
	return nil
}

func phase4TurnState(status string) string {
	switch status {
	case "succeeded":
		return "succeeded"
	case "canceled":
		return "canceled"
	case "interrupted":
		return "interrupted"
	default:
		return "failed"
	}
}

func attachVisibleResult(ctx context.Context, tx *sql.Tx, conversationID, workerRef, turnID, resultID, summary string) error {
	var entryID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM conversation_entries WHERE conversation_id = ? AND kind = 'worker_result' AND body = ? AND result_id = '' ORDER BY seq LIMIT 1`, conversationID, summary).Scan(&entryID)
	if errors.Is(err, sql.ErrNoRows) {
		var orphanCount int
		if countErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_entries WHERE conversation_id = ? AND kind = 'worker_result' AND worker_ref = '' AND turn_id = '' AND result_id = ''`, conversationID).Scan(&orphanCount); countErr != nil {
			return countErr
		}
		if orphanCount == 1 {
			if scanErr := tx.QueryRowContext(ctx, `SELECT id FROM conversation_entries WHERE conversation_id = ? AND kind = 'worker_result' AND worker_ref = '' AND turn_id = '' AND result_id = '' LIMIT 1`, conversationID).Scan(&entryID); scanErr != nil {
				return scanErr
			}
			_, updateErr := tx.ExecContext(ctx, `UPDATE conversation_entries SET worker_ref = ?, turn_id = ?, result_id = ? WHERE id = ?`, workerRef, turnID, resultID, entryID)
			return updateErr
		}
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
CREATE TABLE IF NOT EXISTS phase4_projects (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', mappings_json TEXT NOT NULL, policy_json TEXT NOT NULL, revision INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS phase4_nodes (node_ref TEXT PRIMARY KEY, online INTEGER NOT NULL DEFAULT 0, draining INTEGER NOT NULL DEFAULT 0, revoked INTEGER NOT NULL DEFAULT 0, enrolled_at TEXT NOT NULL, last_seen_at TEXT NOT NULL DEFAULT '', last_heartbeat_at TEXT NOT NULL DEFAULT '', capacity INTEGER NOT NULL DEFAULT 0, active_attempts_json TEXT NOT NULL DEFAULT '', last_processed_command TEXT NOT NULL DEFAULT '', inventory_json TEXT NOT NULL DEFAULT '', credential_hash TEXT NOT NULL DEFAULT '', credential_secret TEXT NOT NULL DEFAULT '', workspaces_json TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS phase4_node_pairing_tokens (token_hash TEXT PRIMARY KEY, consumed_at TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, person_id TEXT NOT NULL, device_id TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL, platform TEXT NOT NULL, scopes_json TEXT NOT NULL, status TEXT NOT NULL, credential_hash TEXT NOT NULL UNIQUE, credential_secret TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, revoked_at TEXT);
CREATE TABLE IF NOT EXISTS client_pairings (id TEXT PRIMARY KEY, client_id TEXT NOT NULL UNIQUE, pending_token_hash TEXT NOT NULL DEFAULT '', pending_token_secret TEXT NOT NULL DEFAULT '', pending_token_redeemed INTEGER NOT NULL DEFAULT 0, generation INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS phase4_approvals (id TEXT PRIMARY KEY, request_id TEXT NOT NULL UNIQUE, worker_id TEXT NOT NULL, turn_id TEXT NOT NULL, attempt_id TEXT NOT NULL, node_id TEXT NOT NULL, project_id TEXT NOT NULL, kind TEXT NOT NULL, action_summary TEXT NOT NULL, risk_category TEXT NOT NULL DEFAULT '', requested_at TEXT NOT NULL, expires_at TEXT, state TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', resolved_by TEXT NOT NULL DEFAULT '', resolved_at TEXT, audit_event_id TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS events (id TEXT PRIMARY KEY, seq INTEGER, kind TEXT NOT NULL, aggregate_type TEXT NOT NULL DEFAULT '', aggregate_id TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', correlation_id TEXT NOT NULL DEFAULT '', causation_id TEXT NOT NULL DEFAULT '', worker_ref TEXT NOT NULL DEFAULT '', attempt_id TEXT NOT NULL DEFAULT '', runtime_session_id TEXT NOT NULL DEFAULT '', payload_json TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS event_sequence (id INTEGER PRIMARY KEY CHECK(id = 1), next_seq INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS deliveries (id TEXT PRIMARY KEY, event_id TEXT NOT NULL, entry_id TEXT, target TEXT NOT NULL, idempotency_key TEXT NOT NULL UNIQUE, state TEXT NOT NULL, retry_count INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, delivered_at TEXT);
CREATE TABLE IF NOT EXISTS config_versions (version TEXT PRIMARY KEY, source_path TEXT NOT NULL, compiled_json TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS config_events (id TEXT PRIMARY KEY, previous_version TEXT NOT NULL, next_version TEXT NOT NULL, diff_json TEXT NOT NULL, created_at TEXT NOT NULL);
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
	// VACUUM INTO reads the database through SQLite, so committed WAL pages
	// are included and the backup is a standalone, queryable database.
	return snapshotDatabase(path, backup)
}

func backupFileIfMissing(path, backup string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if _, err := os.Stat(backup); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Keep the original bytes. A VACUUM snapshot is semantically equivalent
	// but changes SQLite page layout, which makes rollback observably mutate an
	// otherwise untouched active database.
	return copyFile(path, backup)
}

func copyDatabase(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return snapshotDatabase(source, destination)
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
	if err := removeDatabaseFiles(destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func saveRollbackDatabase(path, rollback string) error {
	if err := removeDatabaseFiles(rollback); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("lock database for rollback snapshot: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = db.Exec("ROLLBACK")
		}
	}()
	if err := copyFile(path, rollback); err != nil {
		return fmt.Errorf("copy database rollback snapshot: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyOptionalFile(path+suffix, rollback+suffix); err != nil {
			return fmt.Errorf("copy database rollback sidecar %s: %w", suffix, err)
		}
	}
	if _, err := db.Exec("COMMIT"); err != nil {
		return fmt.Errorf("commit database rollback snapshot: %w", err)
	}
	committed = true
	return nil
}

func restoreRollbackDatabase(rollback, destination string) error {
	if err := removeDatabaseFiles(destination); err != nil {
		return err
	}
	if err := copyFile(rollback, destination); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyOptionalFile(rollback+suffix, destination+suffix); err != nil {
			return err
		}
	}
	return nil
}

func removeDatabaseFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func copyOptionalFile(source, destination string) error {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return removeDatabaseFiles(destination)
	} else if err != nil {
		return err
	}
	return copyFile(source, destination)
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
