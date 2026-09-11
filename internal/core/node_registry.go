package core

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNodeAlreadyEnrolled = errors.New("core: node already enrolled")
	ErrNodeRevoked         = errors.New("core: node revoked")
)

// NodeRecord is the server-owned durable view of one enrolled execution Node.
// Native runtime session identifiers and harness credentials never belong here.
type NodeRecord struct {
	Node            NodeReference            `json:"node"`
	Online          bool                     `json:"online"`
	Draining        bool                     `json:"draining"`
	Revoked         bool                     `json:"revoked"`
	EnrolledAt      time.Time                `json:"enrolled_at"`
	LastSeenAt      time.Time                `json:"last_seen_at,omitempty"`
	LastHeartbeatAt time.Time                `json:"last_heartbeat_at,omitempty"`
	Inventory       HarnessInventorySnapshot `json:"inventory,omitempty"`
}

func (s *Store) EnsureNodeRegistry(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS phase4_nodes (
  node_ref TEXT PRIMARY KEY,
  online INTEGER NOT NULL DEFAULT 0,
  draining INTEGER NOT NULL DEFAULT 0,
  revoked INTEGER NOT NULL DEFAULT 0,
  enrolled_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL DEFAULT '',
  last_heartbeat_at TEXT NOT NULL DEFAULT '',
  inventory_json TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS phase4_nodes_online ON phase4_nodes(online, revoked, draining);
`)
	if err != nil {
		return fmt.Errorf("core: migrate Node registry: %w", err)
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
	if strings.TrimSpace(string(node)) == "" {
		return NodeRecord{}, errors.New("core: Node reference is required")
	}
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return NodeRecord{}, err
	}
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO phase4_nodes(node_ref, enrolled_at) VALUES(?, ?)`, node, timestamp(now)); err != nil {
		var existing NodeRecord
		if current, lookupErr := s.NodeRecord(ctx, node); lookupErr == nil {
			existing = current
			return existing, ErrNodeAlreadyEnrolled
		}
		return NodeRecord{}, err
	}
	return NodeRecord{Node: node, EnrolledAt: now}, nil
}

func (s *Store) NodeRecord(ctx context.Context, node NodeReference) (NodeRecord, error) {
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return NodeRecord{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT node_ref, online, draining, revoked, enrolled_at, last_seen_at, last_heartbeat_at, inventory_json FROM phase4_nodes WHERE node_ref = ?`, node)
	return scanNodeRecord(row)
}

func (s *Store) NodeRecords(ctx context.Context) ([]NodeRecord, error) {
	if err := s.EnsureNodeRegistry(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_ref, online, draining, revoked, enrolled_at, last_seen_at, last_heartbeat_at, inventory_json FROM phase4_nodes ORDER BY enrolled_at, node_ref`)
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

func (s *Store) UpdateNodeHeartbeat(ctx context.Context, node NodeReference, inventory HarnessInventorySnapshot) error {
	if err := inventory.Validate(); err != nil {
		return err
	}
	if inventory.Node != node {
		return errors.New("core: Node heartbeat identity mismatch")
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		return err
	}
	now := s.now()
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET online = 1, last_seen_at = ?, last_heartbeat_at = ?, inventory_json = ? WHERE node_ref = ? AND revoked = 0`, timestamp(now), timestamp(now), string(encoded), node)
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

func (s *Store) SetNodeDraining(ctx context.Context, node NodeReference, draining bool) (NodeRecord, error) {
	value := 0
	if draining {
		value = 1
	}
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET draining = ? WHERE node_ref = ? AND revoked = 0`, value, node)
	if err != nil {
		return NodeRecord{}, err
	}
	if err := s.requireActiveNode(ctx, node, result); err != nil {
		return NodeRecord{}, err
	}
	return s.NodeRecord(ctx, node)
}

func (s *Store) RevokeNode(ctx context.Context, node NodeReference) (NodeRecord, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE phase4_nodes SET revoked = 1, online = 0, draining = 1, last_seen_at = ? WHERE node_ref = ?`, timestamp(s.now()), node)
	if err != nil {
		return NodeRecord{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return NodeRecord{}, ErrNotFound
	}
	return s.NodeRecord(ctx, node)
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
	var enrolledAt, lastSeenAt, lastHeartbeatAt, inventoryJSON string
	if err := row.Scan(&record.Node, &online, &draining, &revoked, &enrolledAt, &lastSeenAt, &lastHeartbeatAt, &inventoryJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeRecord{}, ErrNotFound
		}
		return NodeRecord{}, err
	}
	record.Online, record.Draining, record.Revoked = online != 0, draining != 0, revoked != 0
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
