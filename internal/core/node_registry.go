package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNodeAlreadyEnrolled    = errors.New("core: node already enrolled")
	ErrNodeRevoked            = errors.New("core: node revoked")
	ErrNodePairingTokenUsed   = errors.New("core: node pairing token is invalid or already used")
	ErrNodePairingTokenAbsent = errors.New("core: node pairing token is not configured")
)

// NodeRecord is the server-owned durable view of one enrolled execution Node.
// Native runtime session identifiers and harness credentials never belong here.
type NodeActiveAttempt struct {
	WorkerRef string `json:"worker_ref"`
	TurnID    string `json:"turn_id"`
	AttemptID string `json:"attempt_id"`
}

type NodeHeartbeat struct {
	Capacity             int                 `json:"capacity"`
	ActiveAttempts       []NodeActiveAttempt `json:"active_attempts,omitempty"`
	LastProcessedCommand string              `json:"last_processed_command,omitempty"`
}

type NodeRecord struct {
	Node                 NodeReference            `json:"node"`
	Online               bool                     `json:"online"`
	Draining             bool                     `json:"draining"`
	Revoked              bool                     `json:"revoked"`
	EnrolledAt           time.Time                `json:"enrolled_at"`
	LastSeenAt           time.Time                `json:"last_seen_at,omitempty"`
	LastHeartbeatAt      time.Time                `json:"last_heartbeat_at,omitempty"`
	Capacity             int                      `json:"capacity"`
	ActiveAttempts       []NodeActiveAttempt      `json:"active_attempts,omitempty"`
	LastProcessedCommand string                   `json:"last_processed_command,omitempty"`
	Inventory            HarnessInventorySnapshot `json:"inventory,omitempty"`
	CredentialHash       string                   `json:"credential_hash,omitempty"`
}

func (s *Store) EnsureNodeRegistry(ctx context.Context) error {
	s.nodeRegistryMu.Lock()
	defer s.nodeRegistryMu.Unlock()
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS phase4_nodes (
  node_ref TEXT PRIMARY KEY,
  online INTEGER NOT NULL DEFAULT 0,
  draining INTEGER NOT NULL DEFAULT 0,
  revoked INTEGER NOT NULL DEFAULT 0,
  enrolled_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL DEFAULT '',
  last_heartbeat_at TEXT NOT NULL DEFAULT '',
  capacity INTEGER NOT NULL DEFAULT 0,
  active_attempts_json TEXT NOT NULL DEFAULT '',
  last_processed_command TEXT NOT NULL DEFAULT '',
  inventory_json TEXT NOT NULL DEFAULT '',
  credential_hash TEXT NOT NULL DEFAULT '',
  credential_secret TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS phase4_node_pairing_tokens (
  token_hash TEXT PRIMARY KEY,
  consumed_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS phase4_nodes_online ON phase4_nodes(online, revoked, draining);
`)
	if err != nil {
		return fmt.Errorf("core: migrate Node registry: %w", err)
	}
	for _, migration := range []string{
		"phase4_nodes last_heartbeat_at TEXT NOT NULL DEFAULT ''",
		"phase4_nodes capacity INTEGER NOT NULL DEFAULT 0",
		"phase4_nodes active_attempts_json TEXT NOT NULL DEFAULT ''",
		"phase4_nodes last_processed_command TEXT NOT NULL DEFAULT ''",
		"phase4_nodes credential_hash TEXT NOT NULL DEFAULT ''",
		"phase4_nodes credential_secret TEXT NOT NULL DEFAULT ''",
	} {
		parts := strings.SplitN(migration, " ", 2)
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE `+parts[0]+` ADD COLUMN `+parts[1]); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
			return fmt.Errorf("core: migrate Node registry column %s: %w", migration, err)
		}
	}
	return nil
}

// EnsureNodeTransportSecret returns the server-only master used to derive a
// different protocol credential for every enrolled Node. The derived secret is
// handed to the Node once during pairing; the master never leaves the server.
func (s *Store) EnsureNodeTransportSecret(ctx context.Context) ([]byte, error) {
	const key = "phase4.node_transport_secret.v1"
	if encoded, ok, err := s.GetSetting(ctx, key); err != nil {
		return nil, err
	} else if ok {
		secret, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(secret) < 32 {
			return nil, errors.New("core: invalid persisted Node transport secret")
		}
		return secret, nil
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("core: generate Node transport secret: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(secret)
	if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key, value, updated_at) VALUES(?, ?, ?)`, key, encoded, timestamp(s.now())); err != nil {
		return nil, err
	}
	var persisted string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&persisted); err != nil {
		return nil, err
	}
	secret, err := base64.RawURLEncoding.DecodeString(persisted)
	if err != nil || len(secret) < 32 {
		return nil, errors.New("core: invalid persisted Node transport secret")
	}
	return secret, nil
}

func (s *Store) EnrollNode(ctx context.Context, node NodeReference) (NodeRecord, error) {
	return s.enrollNode(ctx, node, nil)
}

func (s *Store) enrollNode(ctx context.Context, node NodeReference, credential []byte) (NodeRecord, error) {
	if strings.TrimSpace(string(node)) == "" {
		return NodeRecord{}, errors.New("core: Node reference is required")
	}
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return NodeRecord{}, err
	}
	now := s.now()
	hash := credentialHash(credential)
	secret, err := s.encryptNodeCredential(ctx, credential)
	if err != nil {
		return NodeRecord{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO phase4_nodes(node_ref, enrolled_at, credential_hash, credential_secret) VALUES(?, ?, ?, ?)`, node, timestamp(now), hash, secret)
	if err != nil {
		if current, lookupErr := s.NodeRecord(ctx, node); lookupErr == nil {
			return current, ErrNodeAlreadyEnrolled
		}
		return NodeRecord{}, err
	}
	return NodeRecord{Node: node, EnrolledAt: now, CredentialHash: hash}, nil
}

// ConfigureNodePairingToken stores only a digest. The raw token remains in the
// operator configuration and is consumed atomically by EnrollNodeWithPairing.
func (s *Store) ConfigureNodePairingToken(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrNodePairingTokenAbsent
	}
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO phase4_node_pairing_tokens(token_hash) VALUES(?)`, credentialHash([]byte(token)))
	return err
}

// EnrollNodeWithPairing consumes the pairing token and creates the Node
// credential in one transaction. A retry cannot enroll another Node.
func (s *Store) EnrollNodeWithPairing(ctx context.Context, token string, node NodeReference, credential []byte) (NodeRecord, error) {
	s.nodeEnrollmentMu.Lock()
	defer s.nodeEnrollmentMu.Unlock()
	token = strings.TrimSpace(token)
	if token == "" {
		return NodeRecord{}, ErrNodePairingTokenAbsent
	}
	if strings.TrimSpace(string(node)) == "" {
		return NodeRecord{}, errors.New("core: Node reference is required")
	}
	if len(credential) < 32 {
		return NodeRecord{}, errors.New("core: Node credential is too short")
	}
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return NodeRecord{}, err
	}
	secret, err := s.encryptNodeCredential(ctx, credential)
	if err != nil {
		return NodeRecord{}, err
	}
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (NodeRecord, error) {
		now := s.now()
		result, err := tx.ExecContext(ctx, `UPDATE phase4_node_pairing_tokens SET consumed_at = ? WHERE token_hash = ? AND consumed_at = ''`, timestamp(now), credentialHash([]byte(token)))
		if err != nil {
			return NodeRecord{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return NodeRecord{}, ErrNodePairingTokenUsed
		}
		hash := credentialHash(credential)
		_, err = tx.ExecContext(ctx, `INSERT INTO phase4_nodes(node_ref, enrolled_at, credential_hash, credential_secret) VALUES(?, ?, ?, ?)`, node, timestamp(now), hash, secret)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return NodeRecord{}, ErrNodeAlreadyEnrolled
			}
			return NodeRecord{}, err
		}
		return NodeRecord{Node: node, EnrolledAt: now, CredentialHash: hash}, nil
	})
	return returnValue, err
}

func credentialHash(secret []byte) string {
	digest := sha256.Sum256(secret)
	return hex.EncodeToString(digest[:])
}

func (s *Store) NodeRecord(ctx context.Context, node NodeReference) (NodeRecord, error) {
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return NodeRecord{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT node_ref, online, draining, revoked, enrolled_at, last_seen_at, last_heartbeat_at, capacity, active_attempts_json, last_processed_command, inventory_json, credential_hash, credential_secret FROM phase4_nodes WHERE node_ref = ?`, node)
	return scanNodeRecord(row)
}

func (s *Store) NodeRecords(ctx context.Context) ([]NodeRecord, error) {
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_ref, online, draining, revoked, enrolled_at, last_seen_at, last_heartbeat_at, capacity, active_attempts_json, last_processed_command, inventory_json, credential_hash, credential_secret FROM phase4_nodes ORDER BY enrolled_at, node_ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []NodeRecord
	for rows.Next() {
		record, err := scanNodeRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) MarkAllNodesOffline(ctx context.Context) error {
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET online = 0`)
	return err
}

func (s *Store) MarkNodeConnected(ctx context.Context, node NodeReference, inventory HarnessInventorySnapshot) error {
	if err := inventory.Validate(); err != nil {
		return err
	}
	if inventory.Node != node {
		return errors.New("core: Node inventory identity mismatch")
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	now := s.now()
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET online = 1, last_seen_at = ?, inventory_json = ? WHERE node_ref = ? AND revoked = 0`, timestamp(now), string(encoded), node)
	if err != nil {
		return err
	}
	return s.requireActiveNode(ctx, node, result)
}

func (s *Store) MarkSilentNodesOffline(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET online = 0 WHERE online = 1 AND revoked = 0 AND COALESCE(NULLIF(last_heartbeat_at, ''), last_seen_at) <> '' AND COALESCE(NULLIF(last_heartbeat_at, ''), last_seen_at) < ?`, timestamp(before))
	return err
}

func (s *Store) NodeCredential(ctx context.Context, node NodeReference) ([]byte, error) {
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return nil, err
	}
	var encoded string
	if err := s.db.QueryRowContext(ctx, `SELECT credential_secret FROM phase4_nodes WHERE node_ref = ?`, node).Scan(&encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if encoded == "" {
		return nil, ErrNotFound
	}
	return s.decryptNodeCredential(ctx, encoded)
}

func (s *Store) encryptNodeCredential(ctx context.Context, credential []byte) (string, error) {
	if len(credential) == 0 {
		return "", nil
	}
	secret, err := s.EnsureNodeTransportSecret(ctx)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, credential, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Store) decryptNodeCredential(ctx context.Context, encoded string) ([]byte, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("core: invalid persisted Node credential")
	}
	secret, err := s.EnsureNodeTransportSecret(ctx)
	if err != nil {
		return nil, err
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("core: invalid persisted Node credential")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	credential, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("core: invalid persisted Node credential")
	}
	return credential, nil
}

func (s *Store) MarkNodeDisconnected(ctx context.Context, node NodeReference) error {
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET online = 0, last_seen_at = ? WHERE node_ref = ?`, timestamp(s.now()), node)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateNodeHeartbeat(ctx context.Context, node NodeReference, inventory HarnessInventorySnapshot, statuses ...NodeHeartbeat) error {
	if err := inventory.Validate(); err != nil {
		return err
	}
	if inventory.Node != node {
		return errors.New("core: Node heartbeat identity mismatch")
	}
	if len(statuses) > 1 {
		return errors.New("core: at most one Node heartbeat status is allowed")
	}
	status := NodeHeartbeat{}
	if len(statuses) == 1 {
		status = statuses[0]
	}
	if status.Capacity < 0 {
		return errors.New("core: Node heartbeat capacity cannot be negative")
	}
	encodedInventory, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	encodedAttempts, err := json.Marshal(status.ActiveAttempts)
	if err != nil {
		return err
	}
	now := s.now()
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET online = 1, last_seen_at = ?, last_heartbeat_at = ?, capacity = ?, active_attempts_json = ?, last_processed_command = ?, inventory_json = ? WHERE node_ref = ? AND revoked = 0`, timestamp(now), timestamp(now), status.Capacity, string(encodedAttempts), status.LastProcessedCommand, string(encodedInventory), node)
	if err != nil {
		return err
	}
	return s.requireActiveNode(ctx, node, result)
}

func (s *Store) UpdateNodeInventory(ctx context.Context, node NodeReference, inventory HarnessInventorySnapshot) error {
	if err := inventory.Validate(); err != nil {
		return err
	}
	if inventory.Node != node {
		return errors.New("core: Node inventory identity mismatch")
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET last_seen_at = ?, inventory_json = ? WHERE node_ref = ? AND revoked = 0`, timestamp(s.now()), string(encoded), node)
	if err != nil {
		return err
	}
	return s.requireActiveNode(ctx, node, result)
}

func (s *Store) SetNodeDraining(ctx context.Context, node NodeReference, draining bool, idempotencyKeys ...string) (NodeRecord, error) {
	if len(idempotencyKeys) > 1 {
		return NodeRecord{}, errors.New("core: at most one Node idempotency key is allowed")
	}
	key := ""
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	requestHash, err := idempotencyRequestHash(struct {
		Node     NodeReference
		Draining bool
	}{node, draining})
	if err != nil {
		return NodeRecord{}, err
	}
	return withTx(s, ctx, func(tx *sql.Tx) (NodeRecord, error) {
		if key != "" {
			var stored NodeRecord
			found, err := lookupIdempotencyTx(ctx, tx, "node.drain:"+string(node), key, requestHash, &stored)
			if err != nil || found {
				return stored, err
			}
		}
		value := 0
		if draining {
			value = 1
		}
		result, err := tx.ExecContext(ctx, `UPDATE phase4_nodes SET draining = ? WHERE node_ref = ? AND revoked = 0`, value, node)
		if err != nil {
			return NodeRecord{}, err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return NodeRecord{}, ErrNotFound
		}
		record, err := scanNodeRecord(tx.QueryRowContext(ctx, `SELECT node_ref, online, draining, revoked, enrolled_at, last_seen_at, last_heartbeat_at, capacity, active_attempts_json, last_processed_command, inventory_json, credential_hash, credential_secret FROM phase4_nodes WHERE node_ref = ?`, node))
		if err != nil {
			return NodeRecord{}, err
		}
		if key != "" {
			encoded, err := json.Marshal(record)
			if err != nil {
				return NodeRecord{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, "node.drain:"+string(node), key, requestHash, string(encoded), timestamp(s.now())); err != nil {
				return NodeRecord{}, err
			}
		}
		return record, nil
	})
}

func (s *Store) RevokeNode(ctx context.Context, node NodeReference, idempotencyKeys ...string) (NodeRecord, error) {
	if len(idempotencyKeys) > 1 {
		return NodeRecord{}, errors.New("core: at most one Node idempotency key is allowed")
	}
	key := ""
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	requestHash, err := idempotencyRequestHash(struct{ Node NodeReference }{node})
	if err != nil {
		return NodeRecord{}, err
	}
	return withTx(s, ctx, func(tx *sql.Tx) (NodeRecord, error) {
		if key != "" {
			var stored NodeRecord
			found, err := lookupIdempotencyTx(ctx, tx, "node.revoke:"+string(node), key, requestHash, &stored)
			if err != nil || found {
				return stored, err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE phase4_nodes SET revoked = 1, online = 0, draining = 1, last_seen_at = ? WHERE node_ref = ?`, timestamp(s.now()), node)
		if err != nil {
			return NodeRecord{}, err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return NodeRecord{}, ErrNotFound
		}
		record, err := scanNodeRecord(tx.QueryRowContext(ctx, `SELECT node_ref, online, draining, revoked, enrolled_at, last_seen_at, last_heartbeat_at, capacity, active_attempts_json, last_processed_command, inventory_json, credential_hash, credential_secret FROM phase4_nodes WHERE node_ref = ?`, node))
		if err != nil {
			return NodeRecord{}, err
		}
		if key != "" {
			encoded, err := json.Marshal(record)
			if err != nil {
				return NodeRecord{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, "node.revoke:"+string(node), key, requestHash, string(encoded), timestamp(s.now())); err != nil {
				return NodeRecord{}, err
			}
		}
		return record, nil
	})
}

func (s *Store) requireActiveNode(ctx context.Context, node NodeReference, result sql.Result) error {
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	record, err := s.NodeRecord(ctx, node)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if record.Revoked {
		return ErrNodeRevoked
	}
	return ErrNotFound
}

type nodeRowScanner interface {
	Scan(...any) error
}

func scanNodeRecord(row nodeRowScanner) (NodeRecord, error) {
	var record NodeRecord
	var online, draining, revoked int
	var enrolledAt, lastSeenAt, lastHeartbeatAt, activeAttemptsJSON, lastProcessedCommand, inventoryJSON, credentialHashValue, credentialSecret string
	if err := row.Scan(&record.Node, &online, &draining, &revoked, &enrolledAt, &lastSeenAt, &lastHeartbeatAt, &record.Capacity, &activeAttemptsJSON, &lastProcessedCommand, &inventoryJSON, &credentialHashValue, &credentialSecret); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeRecord{}, ErrNotFound
		}
		return NodeRecord{}, err
	}
	record.Online, record.Draining, record.Revoked = online != 0, draining != 0, revoked != 0
	record.LastProcessedCommand = lastProcessedCommand
	record.CredentialHash = credentialHashValue
	if activeAttemptsJSON != "" {
		if err := json.Unmarshal([]byte(activeAttemptsJSON), &record.ActiveAttempts); err != nil {
			return NodeRecord{}, fmt.Errorf("core: decode Node active attempts: %w", err)
		}
	}
	_ = credentialSecret
	var err error
	if record.EnrolledAt, err = parseNodeTime(enrolledAt); err != nil {
		return NodeRecord{}, err
	}
	if lastSeenAt != "" {
		if record.LastSeenAt, err = parseNodeTime(lastSeenAt); err != nil {
			return NodeRecord{}, err
		}
	}
	if lastHeartbeatAt != "" {
		if record.LastHeartbeatAt, err = parseNodeTime(lastHeartbeatAt); err != nil {
			return NodeRecord{}, err
		}
	}
	if inventoryJSON != "" {
		if err := json.Unmarshal([]byte(inventoryJSON), &record.Inventory); err != nil {
			return NodeRecord{}, fmt.Errorf("core: decode Node inventory: %w", err)
		}
	}
	return record, nil
}

func parseNodeTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("core: decode Node timestamp: %w", err)
	}
	return parsed, nil
}
