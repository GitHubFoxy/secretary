package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const MaxArtifactBytes = 8 << 20

type Artifact struct {
	ID        string    `json:"id"`
	PersonID  string    `json:"-"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type artifactSaved struct {
	Artifact  Artifact
	Entry     ConversationEntry
	Duplicate bool
}

func (s *Store) migrateArtifacts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS artifacts (
		id TEXT PRIMARY KEY,
		person_id TEXT NOT NULL REFERENCES persons(id),
		conversation_id TEXT NOT NULL REFERENCES conversations(id),
		idempotency_key TEXT NOT NULL,
		name TEXT NOT NULL,
		media_type TEXT NOT NULL,
		size INTEGER NOT NULL CHECK(size > 0 AND size <= 8388608),
		sha256 TEXT NOT NULL,
		body BLOB NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(person_id, idempotency_key)
	);
	CREATE INDEX IF NOT EXISTS artifacts_by_person ON artifacts(person_id, created_at);`)
	return err
}

func (s *Store) SaveArtifact(ctx context.Context, personID, conversationID, key, name, mediaType string, body []byte) (Artifact, bool, error) {
	if personID == "" || conversationID == "" || key == "" || len(key) > 160 || len(body) == 0 || len(body) > MaxArtifactBytes || len(name) == 0 || len(name) > 180 || len(mediaType) == 0 || len(mediaType) > 120 {
		return Artifact{}, false, errors.New("core: invalid or over-limit artifact")
	}
	digest := sha256.Sum256(body)
	checksum := hex.EncodeToString(digest[:])
	outcome, err := withTx(s, ctx, func(tx *sql.Tx) (artifactSaved, error) {
		existing, found, err := readArtifact(ctx, tx, personID, key)
		if err != nil {
			return artifactSaved{}, err
		}
		if found {
			if existing.Name != name || existing.MediaType != mediaType || existing.SHA256 != checksum || existing.Size != int64(len(body)) {
				return artifactSaved{}, ErrIdempotencyConflict
			}
			return artifactSaved{Artifact: existing, Duplicate: true}, nil
		}
		var owner string
		if err := tx.QueryRowContext(ctx, `SELECT person_id FROM conversations WHERE id = ?`, conversationID).Scan(&owner); err != nil {
			return artifactSaved{}, err
		}
		if owner != personID {
			return artifactSaved{}, ErrNotFound
		}
		now := s.now()
		a := Artifact{ID: newID("art"), PersonID: personID, Name: name, MediaType: mediaType, Size: int64(len(body)), SHA256: checksum, Status: "ready", CreatedAt: now}
		_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id, person_id, conversation_id, idempotency_key, name, media_type, size, sha256, body, created_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, a.PersonID, conversationID, key, a.Name, a.MediaType, a.Size, a.SHA256, body, timestamp(now))
		if err != nil {
			return artifactSaved{}, err
		}
		entry, err := appendEntry(ctx, tx, now, conversationID, EntrySystem, fmt.Sprintf("File uploaded: %q (%d bytes). Ready: artifact://%s", name, len(body), a.ID))
		if err != nil {
			return artifactSaved{}, err
		}
		return artifactSaved{Artifact: a, Entry: entry}, nil
	})
	if err != nil {
		return Artifact{}, false, err
	}
	if !outcome.Duplicate {
		s.notifyEntry(outcome.Entry)
	}
	return outcome.Artifact, outcome.Duplicate, nil
}

func readArtifact(ctx context.Context, q *sql.Tx, personID, key string) (Artifact, bool, error) {
	var a Artifact
	err := q.QueryRowContext(ctx, `SELECT id, person_id, name, media_type, size, sha256, created_at
		FROM artifacts WHERE person_id = ? AND idempotency_key = ?`, personID, key).
		Scan(&a.ID, &a.PersonID, &a.Name, &a.MediaType, &a.Size, &a.SHA256, newTimestampScanner(&a.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, err
	}
	a.Status = "ready"
	return a, true, nil
}

func (s *Store) ArtifactForPerson(ctx context.Context, personID, artifactID string) (Artifact, []byte, error) {
	var a Artifact
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, person_id, name, media_type, size, sha256, body, created_at
		FROM artifacts WHERE person_id = ? AND id = ?`, personID, artifactID).
		Scan(&a.ID, &a.PersonID, &a.Name, &a.MediaType, &a.Size, &a.SHA256, &body, newTimestampScanner(&a.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, nil, ErrNotFound
	}
	if err != nil {
		return Artifact{}, nil, err
	}
	a.Status = "ready"
	if len(body) != int(a.Size) {
		return Artifact{}, nil, fmt.Errorf("artifact %s: body size mismatch", a.ID)
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != a.SHA256 {
		return Artifact{}, nil, fmt.Errorf("artifact %s: checksum mismatch", a.ID)
	}
	return a, body, nil
}
