package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) RegisterVoiceSource(ctx context.Context, personID, sourceID, identity string) error {
	if personID == "" || identity == "" || len(identity) > 160 {
		return errors.New("core: invalid voice source identity")
	}
	return s.withVoiceSource(ctx, personID, sourceID, identity, false)
}

func (s *Store) FailVoiceSource(ctx context.Context, personID, sourceID, identity string) error {
	return s.withVoiceSource(ctx, personID, sourceID, identity, true)
}

func (s *Store) withVoiceSource(ctx context.Context, personID, sourceID, identity string, failed bool) error {
	if personID == "" || sourceID == "" || identity == "" || len(identity) > 160 {
		return errors.New("core: invalid voice source identity")
	}
	_, err := withTx(s, ctx, func(tx *sql.Tx) (bool, error) {
		var mediaType string
		if err := tx.QueryRowContext(ctx, `SELECT media_type FROM artifacts WHERE person_id = ? AND id = ?`, personID, sourceID).Scan(&mediaType); err != nil {
			return false, err
		}
		if len(mediaType) < len("audio/") || mediaType[:len("audio/")] != "audio/" {
			return false, errors.New("core: voice original must be audio")
		}
		var existingID, status string
		err := tx.QueryRowContext(ctx, `SELECT source_artifact_id, status FROM voice_artifact_links WHERE person_id = ? AND source_identity = ?`, personID, identity).Scan(&existingID, &status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		if err == nil {
			if existingID != sourceID {
				return false, ErrIdempotencyConflict
			}
			if failed && status == "processing" {
				_, err = tx.ExecContext(ctx, `UPDATE voice_artifact_links SET status = 'failed' WHERE source_artifact_id = ? AND person_id = ?`, sourceID, personID)
			}
			return true, err
		}
		nextStatus := "processing"
		if failed {
			nextStatus = "failed"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO voice_artifact_links(source_artifact_id, person_id, source_identity, status) VALUES (?, ?, ?, ?)`, sourceID, personID, identity, nextStatus)
		return true, err
	})
	return err
}

func (s *Store) LinkVoiceTranscript(ctx context.Context, personID, sourceID, transcriptID, identity string) error {
	if personID == "" || sourceID == "" || transcriptID == "" || sourceID == transcriptID || identity == "" {
		return errors.New("core: invalid voice transcript linkage")
	}
	_, err := withTx(s, ctx, func(tx *sql.Tx) (bool, error) {
		var previous sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT transcript_artifact_id FROM voice_artifact_links WHERE source_artifact_id = ? AND person_id = ? AND source_identity = ?`, sourceID, personID, identity).Scan(&previous)
		if err != nil {
			return false, err
		}
		if previous.Valid {
			if previous.String != transcriptID {
				return false, ErrIdempotencyConflict
			}
			return true, nil
		}
		var mediaType string
		if err := tx.QueryRowContext(ctx, `SELECT media_type FROM artifacts WHERE person_id = ? AND id = ?`, personID, transcriptID).Scan(&mediaType); err != nil {
			return false, err
		}
		if mediaType != "text/markdown" {
			return false, errors.New("core: transcript must be Markdown")
		}
		res, err := tx.ExecContext(ctx, `UPDATE voice_artifact_links SET transcript_artifact_id = ?, status = 'ready' WHERE source_artifact_id = ? AND person_id = ? AND transcript_artifact_id IS NULL`, transcriptID, sourceID, personID)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, fmt.Errorf("core: voice linkage changed concurrently")
		}
		return true, nil
	})
	return err
}

func (s *Store) populateVoiceRelation(ctx context.Context, personID string, a *Artifact) error {
	var sourceID, identity, status string
	var transcriptID sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT source_artifact_id, transcript_artifact_id, source_identity, status
		FROM voice_artifact_links WHERE person_id = ? AND (source_artifact_id = ? OR transcript_artifact_id = ?)`, personID, a.ID, a.ID).
		Scan(&sourceID, &transcriptID, &identity, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	a.SourceIdentity = identity
	a.ProcessingStatus = status
	if sourceID != a.ID {
		a.SourceArtifactID = sourceID
	}
	if transcriptID.Valid && transcriptID.String != a.ID {
		a.TranscriptArtifactID = transcriptID.String
	}
	return nil
}
