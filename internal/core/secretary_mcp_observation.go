package core

import (
	"context"
	"database/sql"
	"errors"
)

// SecretaryRuntimeLaunch is private process wiring, never a public DTO or prompt.
// The observation capability has no Worker/lifecycle authority.
type SecretaryRuntimeLaunch struct {
	ObserverCapability string `json:"-"`
	Generation         int64  `json:"-"`
}

type SecretaryMCPObservationPhase string

const (
	SecretaryMCPObservationStartup    SecretaryMCPObservationPhase = "startup"
	SecretaryMCPObservationInitialize SecretaryMCPObservationPhase = "initialize"
	SecretaryMCPObservationToolsList  SecretaryMCPObservationPhase = "tools_list"
)

type SecretaryMCPObservation struct {
	Phase          SecretaryMCPObservationPhase `json:"phase"`
	Success        bool                         `json:"success"`
	ToolCount      int                          `json:"tool_count,omitempty"`
	HasSpawnWorker bool                         `json:"has_spawn_worker,omitempty"`
	HasReplyToUser bool                         `json:"has_reply_to_user,omitempty"`
}

func (o SecretaryMCPObservation) validate() error {
	switch o.Phase {
	case SecretaryMCPObservationStartup, SecretaryMCPObservationInitialize, SecretaryMCPObservationToolsList:
	default:
		return ErrMCPObservationInvalid
	}
	if o.ToolCount < 0 || o.ToolCount > 100 {
		return ErrMCPObservationInvalid
	}
	hasCatalogueFields := o.ToolCount != 0 || o.HasSpawnWorker || o.HasReplyToUser
	if (o.Phase != SecretaryMCPObservationToolsList || !o.Success) && hasCatalogueFields {
		return ErrMCPObservationInvalid
	}
	if o.ToolCount == 0 && (o.HasSpawnWorker || o.HasReplyToUser) {
		return ErrMCPObservationInvalid
	}
	return nil
}

// SecretaryMCPDiscovery deliberately omits launch, turn, token and native identities.
// A listed catalogue proves only the broker's written response, not provider choice.
type SecretaryMCPDiscovery struct {
	Started           bool   `json:"started"`
	Initialized       bool   `json:"initialized"`
	ToolsListed       bool   `json:"tools_listed"`
	ToolCount         int    `json:"tool_count"`
	HasSpawnWorker    bool   `json:"has_spawn_worker"`
	HasReplyToUser    bool   `json:"has_reply_to_user"`
	Failed            bool   `json:"failed"`
	Revoked           bool   `json:"revoked"`
	GenerationMatches bool   `json:"generation_matches"`
	Code              string `json:"code"`
}

var ErrMCPObservationUnauthorized = errors.New("core: MCP observation unauthorized")
var ErrMCPObservationInvalid = errors.New("core: invalid MCP observation")

func (s *Store) migrateSecretaryMCPObservation(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS secretary_runtime_launches (
 id TEXT PRIMARY KEY,
 identity_id TEXT NOT NULL REFERENCES secretary_identities(id),
 generation INTEGER NOT NULL,
 capability_hash TEXT NOT NULL UNIQUE,
 parent_capability_hash TEXT NOT NULL,
 revoked INTEGER NOT NULL DEFAULT 0,
 started INTEGER NOT NULL DEFAULT 0,
 initialized INTEGER NOT NULL DEFAULT 0,
 tools_listed INTEGER NOT NULL DEFAULT 0,
 tool_count INTEGER NOT NULL DEFAULT 0,
 has_spawn_worker INTEGER NOT NULL DEFAULT 0,
 has_reply_to_user INTEGER NOT NULL DEFAULT 0,
 failed INTEGER NOT NULL DEFAULT 0,
 UNIQUE(identity_id,generation)
 );
 CREATE TABLE IF NOT EXISTS secretary_turn_launches (
 turn_id TEXT PRIMARY KEY REFERENCES secretary_turns(id),
 launch_id TEXT NOT NULL REFERENCES secretary_runtime_launches(id)
 );`)
	return err
}

// BeginSecretaryRuntime registers an actual impending session launch. Every launch
// advances generation, even with unchanged pins. Failed starts are explicitly revoked.
func (s *Store) BeginSecretaryRuntime(ctx context.Context, identityID, capability, harness, model, reasoning string) (SecretaryRuntimeLaunch, error) {
	return withTx(s, ctx, func(tx *sql.Tx) (SecretaryRuntimeLaunch, error) {
		var generation int64
		err := tx.QueryRowContext(ctx, `SELECT i.runtime_generation FROM secretary_identities i JOIN secretary_capabilities c ON c.person_id=i.person_id WHERE i.id=? AND c.token_hash=? AND c.revoked_at IS NULL`, identityID, hashToken(capability)).Scan(&generation)
		if errors.Is(err, sql.ErrNoRows) {
			return SecretaryRuntimeLaunch{}, ErrMCPObservationUnauthorized
		}
		if err != nil {
			return SecretaryRuntimeLaunch{}, err
		}
		generation++
		if _, err = tx.ExecContext(ctx, `UPDATE secretary_runtime_launches SET revoked=1 WHERE identity_id=?`, identityID); err != nil {
			return SecretaryRuntimeLaunch{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE secretary_identities SET runtime_generation=?,runtime_harness=?,runtime_model=?,runtime_reasoning=?,updated_at=? WHERE id=?`, generation, harness, model, reasoning, timestamp(s.now()), identityID); err != nil {
			return SecretaryRuntimeLaunch{}, err
		}
		launch := SecretaryRuntimeLaunch{ObserverCapability: newID("mcpobs"), Generation: generation}
		_, err = tx.ExecContext(ctx, `INSERT INTO secretary_runtime_launches(id,identity_id,generation,capability_hash,parent_capability_hash) VALUES(?,?,?,?,?)`, newID("launch"), identityID, generation, hashToken(launch.ObserverCapability), hashToken(capability))
		return launch, err
	})
}

func (s *Store) EndSecretaryRuntime(ctx context.Context, launch SecretaryRuntimeLaunch) error {
	_, err := s.db.ExecContext(ctx, `UPDATE secretary_runtime_launches SET revoked=1 WHERE capability_hash=?`, hashToken(launch.ObserverCapability))
	return err
}

func (s *Store) BindSecretaryRuntimeTurn(ctx context.Context, launch SecretaryRuntimeLaunch, turnID, inputID string) error {
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		var launchID string
		err := tx.QueryRowContext(ctx, `SELECT l.id FROM secretary_runtime_launches l JOIN secretary_identities i ON i.id=l.identity_id JOIN secretary_turns t ON t.identity_id=i.id JOIN secretary_capabilities c ON c.person_id=i.person_id AND c.token_hash=l.parent_capability_hash WHERE t.id=? AND t.input_id=? AND t.state=? AND l.capability_hash=? AND l.generation=? AND i.runtime_generation=l.generation AND l.revoked=0 AND c.revoked_at IS NULL`, turnID, inputID, SecretaryTurnActive, hashToken(launch.ObserverCapability), launch.Generation).Scan(&launchID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidSecretaryOrigin
		}
		if err != nil {
			return err
		}
		var existing string
		err = tx.QueryRowContext(ctx, `SELECT launch_id FROM secretary_turn_launches WHERE turn_id=?`, turnID).Scan(&existing)
		if err == nil {
			if existing != launchID {
				return ErrInvalidSecretaryOrigin
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO secretary_turn_launches(turn_id,launch_id) VALUES(?,?)`, turnID, launchID)
		return err
	})
}

func (s *Store) RecordSecretaryMCPObservation(ctx context.Context, personID, token string, observation SecretaryMCPObservation) error {
	if token == "" {
		return ErrMCPObservationUnauthorized
	}
	if err := observation.validate(); err != nil {
		return err
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		var id string
		var started, initialized, listed bool
		var count int
		var spawn, reply bool
		err := tx.QueryRowContext(ctx, `SELECT l.id,l.started,l.initialized,l.tools_listed,l.tool_count,l.has_spawn_worker,l.has_reply_to_user FROM secretary_runtime_launches l JOIN secretary_identities i ON i.id=l.identity_id JOIN secretary_capabilities c ON c.person_id=i.person_id AND c.token_hash=l.parent_capability_hash WHERE i.person_id=? AND l.capability_hash=? AND l.revoked=0 AND i.runtime_generation=l.generation AND c.revoked_at IS NULL`, personID, hashToken(token)).Scan(&id, &started, &initialized, &listed, &count, &spawn, &reply)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMCPObservationUnauthorized
		}
		if err != nil {
			return err
		}
		if !observation.Success {
			_, err = tx.ExecContext(ctx, `UPDATE secretary_runtime_launches SET failed=1 WHERE id=?`, id)
			return err
		}
		switch observation.Phase {
		case SecretaryMCPObservationStartup:
			_, err = tx.ExecContext(ctx, `UPDATE secretary_runtime_launches SET started=1 WHERE id=?`, id)
		case SecretaryMCPObservationInitialize:
			if !started {
				return ErrMCPObservationInvalid
			}
			_, err = tx.ExecContext(ctx, `UPDATE secretary_runtime_launches SET initialized=1 WHERE id=?`, id)
		case SecretaryMCPObservationToolsList:
			if !initialized {
				return ErrMCPObservationInvalid
			}
			if listed && (count != observation.ToolCount || spawn != observation.HasSpawnWorker || reply != observation.HasReplyToUser) {
				return ErrMCPObservationInvalid
			}
			_, err = tx.ExecContext(ctx, `UPDATE secretary_runtime_launches SET tools_listed=1,tool_count=?,has_spawn_worker=?,has_reply_to_user=? WHERE id=?`, observation.ToolCount, observation.HasSpawnWorker, observation.HasReplyToUser, id)
		}
		return err
	})
}

func (s *Store) SecretaryMCPDiscovery(ctx context.Context, turnID string) (SecretaryMCPDiscovery, error) {
	return secretaryMCPDiscoveryQuery(ctx, s.db, turnID)
}

func secretaryMCPDiscoveryQuery(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, turnID string) (SecretaryMCPDiscovery, error) {
	evidence := SecretaryMCPDiscovery{Code: "missing"}
	err := q.QueryRowContext(ctx, `SELECT l.started,l.initialized,l.tools_listed,l.tool_count,l.has_spawn_worker,l.has_reply_to_user,l.failed,(l.revoked<>0 OR c.revoked_at IS NOT NULL),i.runtime_generation=l.generation FROM secretary_turn_launches t JOIN secretary_runtime_launches l ON l.id=t.launch_id JOIN secretary_identities i ON i.id=l.identity_id JOIN secretary_capabilities c ON c.person_id=i.person_id AND c.token_hash=l.parent_capability_hash WHERE t.turn_id=?`, turnID).Scan(&evidence.Started, &evidence.Initialized, &evidence.ToolsListed, &evidence.ToolCount, &evidence.HasSpawnWorker, &evidence.HasReplyToUser, &evidence.Failed, &evidence.Revoked, &evidence.GenerationMatches)
	if errors.Is(err, sql.ErrNoRows) {
		return evidence, nil
	}
	if err != nil {
		return evidence, err
	}
	switch {
	case evidence.Revoked || !evidence.GenerationMatches:
		evidence.Code = "revoked"
	case evidence.Failed:
		evidence.Code = "failed"
	case evidence.ToolsListed:
		evidence.Code = "discovered"
	case evidence.Started:
		evidence.Code = "pending"
	}
	return evidence, nil
}
