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

type ClientStatus string

const (
	ClientPending ClientStatus = "pending"
	ClientActive  ClientStatus = "active"
	ClientRevoked ClientStatus = "revoked"
)

type ClientScope string

const (
	ScopeConversationRead  ClientScope = "conversation:read"
	ScopeConversationWrite ClientScope = "conversation:write"
	ScopeWorkerRead        ClientScope = "worker:read"
	ScopeWorkerWrite       ClientScope = "worker:write"
	ScopeApprovalRead      ClientScope = "approval:read"
	ScopeApprovalWrite     ClientScope = "approval:write"
	ScopeProjectRead       ClientScope = "project:read"
	ScopeProjectWrite      ClientScope = "project:write"
	ScopeNodeRead          ClientScope = "node:read"
	ScopeNodeWrite         ClientScope = "node:write"
	ScopeClientManage      ClientScope = "client:manage"
	ScopeUserRead          ClientScope = "user:read"
	ScopeUserWrite         ClientScope = "user:write"
)

var ErrClientRevoked = errors.New("core: Client is revoked")

// Client is the public, server-owned identity of a trusted user interface.
// CredentialHash is deliberately never serialized.
type Client struct {
	ID             string        `json:"id"`
	PersonID       string        `json:"person_id"`
	DeviceID       string        `json:"device_id"`
	DisplayName    string        `json:"display_name"`
	Platform       string        `json:"platform"`
	Scopes         []ClientScope `json:"scopes"`
	Status         ClientStatus  `json:"status"`
	CreatedAt      time.Time     `json:"created_at"`
	RevokedAt      *time.Time    `json:"revoked_at,omitempty"`
	CredentialHash string        `json:"-"`
}

func defaultClientScopes() []ClientScope {
	return []ClientScope{ScopeConversationRead, ScopeConversationWrite, ScopeWorkerRead, ScopeWorkerWrite, ScopeApprovalRead, ScopeApprovalWrite, ScopeProjectRead, ScopeProjectWrite, ScopeNodeRead, ScopeNodeWrite, ScopeClientManage, ScopeUserRead, ScopeUserWrite}
}

func validClientScope(scope ClientScope) bool {
	switch scope {
	case ScopeConversationRead, ScopeConversationWrite, ScopeWorkerRead, ScopeWorkerWrite, ScopeApprovalRead, ScopeApprovalWrite, ScopeProjectRead, ScopeProjectWrite, ScopeNodeRead, ScopeNodeWrite, ScopeClientManage, ScopeUserRead, ScopeUserWrite:
		return true
	default:
		return false
	}
}

func normalizeClientScopes(scopes []ClientScope) []ClientScope {
	if len(scopes) == 0 {
		return defaultClientScopes()
	}
	seen := make(map[ClientScope]bool, len(scopes))
	result := make([]ClientScope, 0, len(scopes))
	for _, scope := range scopes {
		scope = ClientScope(strings.TrimSpace(string(scope)))
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		result = append(result, scope)
	}
	return result
}

func randomClientCredential() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("core: generate Client credential: %w", err)
	}
	return "cli_" + base64.RawURLEncoding.EncodeToString(value), nil
}

func (s *Store) PairClient(ctx context.Context, personID, deviceID, displayName, platform string, scopes []ClientScope, idempotencyKeys ...string) (Client, error) {
	deviceID, displayName, platform = strings.TrimSpace(deviceID), strings.TrimSpace(displayName), strings.TrimSpace(platform)
	if len(idempotencyKeys) > 1 {
		return Client{}, errors.New("core: at most one Client idempotency key is allowed")
	}
	key := ""
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	if personID == "" || deviceID == "" || displayName == "" || platform == "" {
		return Client{}, errors.New("core: Client person, device, display name and platform are required")
	}
	for _, scope := range scopes {
		if !validClientScope(scope) {
			return Client{}, fmt.Errorf("core: invalid Client scope %q", scope)
		}
	}
	normalizedScopes := normalizeClientScopes(scopes)
	encodedScopes, err := json.Marshal(normalizedScopes)
	if err != nil {
		return Client{}, err
	}
	credential, err := randomClientCredential()
	if err != nil {
		return Client{}, err
	}
	now := s.now()
	client := Client{ID: newID("cli"), PersonID: personID, DeviceID: deviceID, DisplayName: displayName, Platform: platform, Scopes: normalizedScopes, Status: ClientPending, CreatedAt: now, CredentialHash: credentialHash([]byte(credential))}
	if key != "" {
		var encoded string
		if err := s.db.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = 'client.pair' AND idempotency_key = ?`, key).Scan(&encoded); err == nil {
			var stored Client
			if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
				return Client{}, err
			}
			return stored, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Client{}, err
		}
	}
	var existingID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM clients WHERE device_id = ?`, deviceID).Scan(&existingID); err == nil {
		return s.Client(ctx, existingID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Client{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO clients(id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, client.ID, client.PersonID, client.DeviceID, client.DisplayName, client.Platform, string(encodedScopes), client.Status, client.CredentialHash, timestamp(now))
	if err != nil {
		return Client{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO client_pairings(id, client_id, created_at) VALUES(?, ?, ?)`, newID("pair"), client.ID, timestamp(now)); err != nil {
		return Client{}, err
	}
	_, _ = s.RecordEventWithMetadata(ctx, EventInput{Kind: "client.paired", AggregateType: "client", AggregateID: client.ID, Source: "server", Payload: map[string]any{"client_id": client.ID, "device_id": client.DeviceID, "platform": client.Platform}})
	if key != "" {
		encodedClient, err := json.Marshal(client)
		if err != nil {
			return Client{}, err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES('client.pair', ?, ?, ?)`, key, string(encodedClient), timestamp(now)); err != nil {
			return Client{}, err
		}
	}
	return client, nil
}

func (s *Store) ApproveClient(ctx context.Context, id string) (Client, string, error) {
	credential, err := randomClientCredential()
	if err != nil {
		return Client{}, "", err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE clients SET status = ?, credential_hash = ? WHERE id = ? AND status = ?`, ClientActive, credentialHash([]byte(credential)), strings.TrimSpace(id), ClientPending)
	if err != nil {
		return Client{}, "", err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		client, lookupErr := s.Client(ctx, id)
		if lookupErr != nil {
			return Client{}, "", lookupErr
		}
		if client.Status == ClientActive {
			return client, "", nil
		}
		return Client{}, "", ErrInvalidTransition
	}
	client, err := s.Client(ctx, id)
	if err != nil {
		return Client{}, "", err
	}
	_, _ = s.RecordEventWithMetadata(ctx, EventInput{Kind: "client.connected", AggregateType: "client", AggregateID: client.ID, Source: "server", Payload: map[string]any{"client_id": client.ID}})
	return client, credential, nil
}

func (s *Store) Client(ctx context.Context, id string) (Client, error) {
	return scanClient(s.db.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE id = ?`, strings.TrimSpace(id)))
}

func (s *Store) Clients(ctx context.Context, personID string) ([]Client, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE person_id = ? ORDER BY created_at, id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clients := make([]Client, 0)
	for rows.Next() {
		client, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

func (s *Store) AuthenticateClient(ctx context.Context, credential string) (Client, error) {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return Client{}, ErrNotFound
	}
	client, err := scanClient(s.db.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE credential_hash = ?`, credentialHash([]byte(credential))))
	if err != nil {
		return Client{}, err
	}
	if client.Status == ClientRevoked {
		return Client{}, ErrClientRevoked
	}
	if client.Status != ClientActive {
		return Client{}, ErrNotFound
	}
	return client, nil
}

func (s *Store) RevokeClient(ctx context.Context, id string) (Client, error) {
	now := s.now()
	result, err := s.db.ExecContext(ctx, `UPDATE clients SET status = ?, revoked_at = ? WHERE id = ? AND status <> ?`, ClientRevoked, timestamp(now), strings.TrimSpace(id), ClientRevoked)
	if err != nil {
		return Client{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		client, lookupErr := s.Client(ctx, id)
		if lookupErr != nil {
			return Client{}, lookupErr
		}
		if client.Status != ClientRevoked {
			return Client{}, ErrInvalidTransition
		}
		return client, nil
	}
	client, err := s.Client(ctx, id)
	if err != nil {
		return Client{}, err
	}
	_, _ = s.RecordEventWithMetadata(ctx, EventInput{Kind: "client.revoked", AggregateType: "client", AggregateID: client.ID, Source: "server", Payload: map[string]any{"client_id": client.ID}})
	return client, nil
}

func (c Client) HasScope(scope ClientScope) bool {
	for _, candidate := range c.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func scanClient(scanner interface{ Scan(...any) error }) (Client, error) {
	var client Client
	var scopesJSON, createdAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(&client.ID, &client.PersonID, &client.DeviceID, &client.DisplayName, &client.Platform, &scopesJSON, &client.Status, &client.CredentialHash, &createdAt, &revokedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Client{}, ErrNotFound
		}
		return Client{}, err
	}
	if err := json.Unmarshal([]byte(scopesJSON), &client.Scopes); err != nil {
		return Client{}, err
	}
	var err error
	if client.CreatedAt, err = parseTimestamp(createdAt); err != nil {
		return Client{}, err
	}
	if revokedAt.Valid && revokedAt.String != "" {
		value, parseErr := parseTimestamp(revokedAt.String)
		if parseErr != nil {
			return Client{}, parseErr
		}
		client.RevokedAt = &value
	}
	return client, nil
}
