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

var (
	ErrClientRevoked      = errors.New("core: Client is revoked")
	ErrPairingNotReady    = errors.New("core: Client pairing is not approved")
	ErrPairingAlreadyUsed = errors.New("core: Client pairing handoff already used")
)

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

type ClientPairing struct {
	Client
	PendingToken string `json:"pending_token,omitempty"`
}

type ClientPairingStatus struct {
	ClientID        string       `json:"client_id"`
	Status          ClientStatus `json:"status"`
	CredentialReady bool         `json:"credential_ready"`
	Redeemed        bool         `json:"redeemed"`
}

type clientCredentialOutcome struct {
	Client     Client `json:"client"`
	Credential string `json:"credential,omitempty"`
}

type clientRedeemOutcome struct {
	Client           Client `json:"client"`
	CredentialSecret string `json:"credential_secret"`
	Generation       int64  `json:"generation"`
	PendingTokenHash string `json:"pending_token_hash"`
}

func (s *Store) PairClient(ctx context.Context, personID, deviceID, displayName, platform string, scopes []ClientScope, idempotencyKeys ...string) (Client, error) {
	pairing, err := s.PairClientWithToken(ctx, personID, deviceID, displayName, platform, scopes, idempotencyKeys...)
	return pairing.Client, err
}

func (s *Store) PairClientWithToken(ctx context.Context, personID, deviceID, displayName, platform string, scopes []ClientScope, idempotencyKeys ...string) (ClientPairing, error) {
	deviceID, displayName, platform = strings.TrimSpace(deviceID), strings.TrimSpace(displayName), strings.TrimSpace(platform)
	if len(idempotencyKeys) > 1 {
		return ClientPairing{}, errors.New("core: at most one Client idempotency key is allowed")
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
		return ClientPairing{}, errors.New("core: Client person, device, display name and platform are required")
	}
	for _, scope := range scopes {
		if !validClientScope(scope) {
			return ClientPairing{}, fmt.Errorf("core: invalid Client scope %q", scope)
		}
	}
	normalizedScopes := normalizeClientScopes(scopes)
	requestHash, err := idempotencyRequestHash(struct {
		PersonID    string
		DeviceID    string
		DisplayName string
		Platform    string
		Scopes      []ClientScope
	}{personID, deviceID, displayName, platform, normalizedScopes})
	if err != nil {
		return ClientPairing{}, err
	}
	if key != "" {
		var storedHash, encoded string
		err := s.db.QueryRowContext(ctx, `SELECT request_hash, outcome_json FROM idempotency_records WHERE operation = 'client.pair' AND idempotency_key = ?`, key).Scan(&storedHash, &encoded)
		if err == nil {
			if err := checkIdempotencyHash(storedHash, requestHash); err != nil {
				return ClientPairing{}, err
			}
			var stored Client
			if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
				return ClientPairing{}, err
			}
			var encrypted string
			var redeemed int
			if err := s.db.QueryRowContext(ctx, `SELECT pending_token_secret, pending_token_redeemed FROM client_pairings WHERE client_id = ?`, stored.ID).Scan(&encrypted, &redeemed); err == nil && redeemed == 0 && encrypted != "" {
				if token, decryptErr := s.decryptNodeCredential(ctx, encrypted); decryptErr == nil {
					return ClientPairing{Client: stored, PendingToken: string(token)}, nil
				}
			}
			return ClientPairing{Client: stored}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ClientPairing{}, err
		}
	}
	var existingID string
	var existingStatus ClientStatus
	if err := s.db.QueryRowContext(ctx, `SELECT id, status FROM clients WHERE device_id = ?`, deviceID).Scan(&existingID, &existingStatus); err == nil {
		if existingStatus == ClientActive {
			client, err := s.Client(ctx, existingID)
			if err != nil {
				return ClientPairing{}, err
			}
			return ClientPairing{Client: client}, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ClientPairing{}, err
	}
	pendingToken, err := randomClientCredential()
	if err != nil {
		return ClientPairing{}, err
	}
	pendingSecret, err := s.encryptNodeCredential(ctx, []byte(pendingToken))
	if err != nil {
		return ClientPairing{}, err
	}
	encodedScopes, err := json.Marshal(normalizedScopes)
	if err != nil {
		return ClientPairing{}, err
	}
	now := s.now()
	client := Client{ID: existingID, PersonID: personID, DeviceID: deviceID, DisplayName: displayName, Platform: platform, Scopes: normalizedScopes, Status: ClientPending, CreatedAt: now, CredentialHash: credentialHash([]byte(pendingToken))}
	if client.ID == "" {
		client.ID = newID("cli")
	}
	created, err := withTx(s, ctx, func(tx *sql.Tx) (Client, error) {
		if existingID == "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO clients(id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, credential_secret, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, '', ?)`, client.ID, client.PersonID, client.DeviceID, client.DisplayName, client.Platform, string(encodedScopes), client.Status, client.CredentialHash, timestamp(now)); err != nil {
				return Client{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO client_pairings(id, client_id, pending_token_hash, pending_token_secret, generation, created_at) VALUES(?, ?, ?, ?, 1, ?)`, newID("pair"), client.ID, credentialHash([]byte(pendingToken)), pendingSecret, timestamp(now)); err != nil {
				return Client{}, err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE clients SET person_id = ?, display_name = ?, platform = ?, scopes_json = ?, status = ?, credential_hash = ?, credential_secret = '', revoked_at = NULL WHERE id = ?`, client.PersonID, client.DisplayName, client.Platform, string(encodedScopes), client.Status, client.CredentialHash, client.ID); err != nil {
				return Client{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE client_pairings SET pending_token_hash = ?, pending_token_secret = ?, pending_token_redeemed = 0, generation = generation + 1, created_at = ? WHERE client_id = ?`, credentialHash([]byte(pendingToken)), pendingSecret, timestamp(now), client.ID); err != nil {
				return Client{}, err
			}
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "client.paired", AggregateType: "client", AggregateID: client.ID, Source: "server", Payload: map[string]any{"client_id": client.ID, "device_id": client.DeviceID, "platform": client.Platform}}, client); err != nil {
			return Client{}, err
		}
		if key != "" {
			encodedClient, err := json.Marshal(client)
			if err != nil {
				return Client{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES('client.pair', ?, ?, ?, ?)`, key, requestHash, string(encodedClient), timestamp(now)); err != nil {
				return Client{}, err
			}
		}
		return client, nil
	})
	if err != nil {
		return ClientPairing{}, err
	}
	return ClientPairing{Client: created, PendingToken: pendingToken}, nil
}

func (s *Store) ApproveClient(ctx context.Context, id string, idempotencyKeys ...string) (Client, string, error) {
	id = strings.TrimSpace(id)
	key := ""
	if len(idempotencyKeys) > 1 {
		return Client{}, "", errors.New("core: at most one Client idempotency key is allowed")
	}
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	requestHash, err := idempotencyRequestHash(struct{ ClientID string }{id})
	if err != nil {
		return Client{}, "", err
	}
	if key != "" {
		var storedHash, encoded, encrypted string
		lookupErr := s.db.QueryRowContext(ctx, `SELECT request_hash, outcome_json FROM idempotency_records WHERE operation = 'client.approve' AND idempotency_key = ?`, key).Scan(&storedHash, &encoded)
		if lookupErr == nil {
			if err := checkIdempotencyHash(storedHash, requestHash); err != nil {
				return Client{}, "", err
			}
			var stored clientCredentialOutcome
			if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
				return Client{}, "", err
			}
			if err := s.db.QueryRowContext(ctx, `SELECT credential_secret FROM clients WHERE id = ?`, id).Scan(&encrypted); err != nil {
				return Client{}, "", err
			}
			secret, err := s.decryptNodeCredential(ctx, encrypted)
			if err != nil {
				return Client{}, "", err
			}
			return stored.Client, string(secret), nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return Client{}, "", lookupErr
		}
	}
	credential, err := randomClientCredential()
	if err != nil {
		return Client{}, "", err
	}
	credentialSecret, err := s.encryptNodeCredential(ctx, []byte(credential))
	if err != nil {
		return Client{}, "", err
	}
	result, err := withTx(s, ctx, func(tx *sql.Tx) (clientCredentialOutcome, error) {
		if key != "" {
			var stored clientCredentialOutcome
			found, err := lookupIdempotencyTx(ctx, tx, "client.approve", key, requestHash, &stored)
			if err != nil || found {
				return stored, err
			}
		}
		result, err := tx.ExecContext(ctx, `UPDATE clients SET status = ?, credential_hash = ?, credential_secret = ? WHERE id = ? AND status = ?`, ClientActive, credentialHash([]byte(credential)), credentialSecret, id, ClientPending)
		if err != nil {
			return clientCredentialOutcome{}, err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			client, lookupErr := scanClient(tx.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE id = ?`, id))
			if lookupErr != nil {
				return clientCredentialOutcome{}, lookupErr
			}
			if client.Status == ClientActive {
				return clientCredentialOutcome{Client: client}, nil
			}
			return clientCredentialOutcome{}, ErrInvalidTransition
		}
		client, err := scanClient(tx.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE id = ?`, id))
		if err != nil {
			return clientCredentialOutcome{}, err
		}
		if _, err := appendEventTx(ctx, tx, s.now(), EventInput{Kind: "client.connected", AggregateType: "client", AggregateID: client.ID, Source: "server", Payload: map[string]any{"client_id": client.ID}}, client); err != nil {
			return clientCredentialOutcome{}, err
		}
		outcome := clientCredentialOutcome{Client: client, Credential: credential}
		if key != "" {
			encoded, err := json.Marshal(struct {
				Client Client `json:"client"`
			}{Client: client})
			if err != nil {
				return clientCredentialOutcome{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, "client.approve", key, requestHash, string(encoded), timestamp(s.now())); err != nil {
				return clientCredentialOutcome{}, err
			}
		}
		return outcome, nil
	})
	return result.Client, result.Credential, err
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

func (s *Store) RevokeClient(ctx context.Context, id string, idempotencyKeys ...string) (Client, error) {
	id = strings.TrimSpace(id)
	key := ""
	if len(idempotencyKeys) > 1 {
		return Client{}, errors.New("core: at most one Client idempotency key is allowed")
	}
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	requestHash, err := idempotencyRequestHash(struct{ ClientID string }{id})
	if err != nil {
		return Client{}, err
	}
	returnValue, err := withTx(s, ctx, func(tx *sql.Tx) (Client, error) {
		if key != "" {
			var stored Client
			found, err := lookupIdempotencyTx(ctx, tx, "client.revoke", key, requestHash, &stored)
			if err != nil || found {
				return stored, err
			}
		}
		now := s.now()
		result, err := tx.ExecContext(ctx, `UPDATE clients SET status = ?, revoked_at = ? WHERE id = ? AND status <> ?`, ClientRevoked, timestamp(now), id, ClientRevoked)
		if err != nil {
			return Client{}, err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			client, lookupErr := scanClient(tx.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE id = ?`, id))
			if lookupErr != nil {
				return Client{}, lookupErr
			}
			if client.Status != ClientRevoked {
				return Client{}, ErrInvalidTransition
			}
			if key != "" {
				encoded, _ := json.Marshal(client)
				_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, "client.revoke", key, requestHash, string(encoded), timestamp(now))
			}
			return client, err
		}
		client, err := scanClient(tx.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE id = ?`, id))
		if err != nil {
			return Client{}, err
		}
		if _, err := appendEventTx(ctx, tx, now, EventInput{Kind: "client.revoked", AggregateType: "client", AggregateID: client.ID, Source: "server", Payload: map[string]any{"client_id": client.ID}}, client); err != nil {
			return Client{}, err
		}
		if key != "" {
			encoded, err := json.Marshal(client)
			if err != nil {
				return Client{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, "client.revoke", key, requestHash, string(encoded), timestamp(now)); err != nil {
				return Client{}, err
			}
		}
		return client, nil
	})
	return returnValue, err
}

func (s *Store) ClientPairingStatus(ctx context.Context, id, pendingToken string) (ClientPairingStatus, error) {
	var status ClientPairingStatus
	var hash string
	var redeemed int
	err := s.db.QueryRowContext(ctx, `SELECT c.id, c.status, p.pending_token_hash, p.pending_token_redeemed FROM clients c JOIN client_pairings p ON p.client_id = c.id WHERE c.id = ?`, strings.TrimSpace(id)).Scan(&status.ClientID, &status.Status, &hash, &redeemed)
	if errors.Is(err, sql.ErrNoRows) {
		return ClientPairingStatus{}, ErrNotFound
	}
	if err != nil {
		return ClientPairingStatus{}, err
	}
	if credentialHash([]byte(strings.TrimSpace(pendingToken))) != hash {
		return ClientPairingStatus{}, ErrNotFound
	}
	status.Redeemed = redeemed != 0
	status.CredentialReady = status.Status == ClientActive && !status.Redeemed
	return status, nil
}

func (s *Store) RedeemClient(ctx context.Context, id, pendingToken string, idempotencyKeys ...string) (Client, string, error) {
	id = strings.TrimSpace(id)
	pendingToken = strings.TrimSpace(pendingToken)
	if id == "" || pendingToken == "" {
		return Client{}, "", ErrNotFound
	}
	key := ""
	if len(idempotencyKeys) > 1 {
		return Client{}, "", errors.New("core: at most one Client idempotency key is allowed")
	}
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	pendingTokenHash := credentialHash([]byte(pendingToken))
	requestHash, err := idempotencyRequestHash(struct {
		ClientID         string
		PendingTokenHash string
	}{id, pendingTokenHash})
	if err != nil {
		return Client{}, "", err
	}
	if key != "" {
		var storedHash, encoded string
		lookupErr := s.db.QueryRowContext(ctx, `SELECT request_hash, outcome_json FROM idempotency_records WHERE operation = 'client.redeem' AND idempotency_key = ?`, key).Scan(&storedHash, &encoded)
		if lookupErr == nil {
			if err := checkIdempotencyHash(storedHash, requestHash); err != nil {
				return Client{}, "", err
			}
			var stored clientRedeemOutcome
			if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
				return Client{}, "", err
			}
			if stored.Generation <= 0 || stored.PendingTokenHash != pendingTokenHash || stored.CredentialSecret == "" {
				return Client{}, "", ErrNotFound
			}
			secret, err := s.decryptNodeCredential(ctx, stored.CredentialSecret)
			if err != nil {
				return Client{}, "", ErrPairingNotReady
			}
			return stored.Client, string(secret), nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return Client{}, "", lookupErr
		}
	}
	var encrypted string
	var generation int64
	if err := s.db.QueryRowContext(ctx, `SELECT c.credential_secret, p.generation FROM clients c JOIN client_pairings p ON p.client_id = c.id WHERE c.id = ? AND p.pending_token_hash = ?`, id, pendingTokenHash).Scan(&encrypted, &generation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Client{}, "", ErrNotFound
		}
		return Client{}, "", err
	}
	credentialBytes, err := s.decryptNodeCredential(ctx, encrypted)
	if err != nil {
		return Client{}, "", ErrPairingNotReady
	}
	credentialSecret := ""
	if key != "" {
		credentialSecret, err = s.encryptNodeCredential(ctx, credentialBytes)
		if err != nil {
			return Client{}, "", err
		}
	}
	result, err := withTx(s, ctx, func(tx *sql.Tx) (clientCredentialOutcome, error) {
		if key != "" {
			var stored clientRedeemOutcome
			found, err := lookupIdempotencyTx(ctx, tx, "client.redeem", key, requestHash, &stored)
			if err != nil {
				return clientCredentialOutcome{}, err
			}
			if found {
				if stored.Generation <= 0 || stored.PendingTokenHash != pendingTokenHash || stored.CredentialSecret == "" {
					return clientCredentialOutcome{}, ErrNotFound
				}
				secret, err := s.decryptNodeCredential(ctx, stored.CredentialSecret)
				if err != nil {
					return clientCredentialOutcome{}, ErrPairingNotReady
				}
				return clientCredentialOutcome{Client: stored.Client, Credential: string(secret)}, nil
			}
		}
		var status ClientStatus
		var redeemed int
		if err := tx.QueryRowContext(ctx, `SELECT c.status, p.pending_token_redeemed FROM clients c JOIN client_pairings p ON p.client_id = c.id WHERE c.id = ? AND p.pending_token_hash = ? AND p.generation = ?`, id, pendingTokenHash, generation).Scan(&status, &redeemed); errors.Is(err, sql.ErrNoRows) {
			return clientCredentialOutcome{}, ErrNotFound
		} else if err != nil {
			return clientCredentialOutcome{}, err
		}
		if status != ClientActive {
			return clientCredentialOutcome{}, ErrPairingNotReady
		}
		if redeemed != 0 {
			return clientCredentialOutcome{}, ErrPairingAlreadyUsed
		}
		result, err := tx.ExecContext(ctx, `UPDATE client_pairings SET pending_token_redeemed = 1 WHERE client_id = ? AND pending_token_hash = ? AND generation = ? AND pending_token_redeemed = 0`, id, pendingTokenHash, generation)
		if err != nil {
			return clientCredentialOutcome{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return clientCredentialOutcome{}, ErrPairingAlreadyUsed
		}
		client, err := scanClient(tx.QueryRowContext(ctx, `SELECT id, person_id, device_id, display_name, platform, scopes_json, status, credential_hash, created_at, revoked_at FROM clients WHERE id = ?`, id))
		if err != nil {
			return clientCredentialOutcome{}, err
		}
		outcome := clientCredentialOutcome{Client: client, Credential: string(credentialBytes)}
		if key != "" {
			encoded, err := json.Marshal(clientRedeemOutcome{Client: client, CredentialSecret: credentialSecret, Generation: generation, PendingTokenHash: pendingTokenHash})
			if err != nil {
				return clientCredentialOutcome{}, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, request_hash, outcome_json, created_at) VALUES(?, ?, ?, ?, ?)`, "client.redeem", key, requestHash, string(encoded), timestamp(s.now())); err != nil {
				return clientCredentialOutcome{}, err
			}
		}
		return outcome, nil
	})
	return result.Client, result.Credential, err
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
