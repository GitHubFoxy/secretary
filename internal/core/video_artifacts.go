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

type VideoFrame struct {
	ArtifactID    string `json:"artifact_id"`
	TimestampMS   int64  `json:"timestamp_ms"`
	TimestampKind string `json:"timestamp_kind"`
}

type VideoFrameData struct {
	TimestampMS   int64
	TimestampKind string
	Data          []byte
}

type VideoJob struct {
	ArtifactID     string
	PersonID       string
	Status         string
	Error          string
	FPS            float64
	MaxFrames      int
	MaxDuration    int
	TimeoutSeconds int
}

func (s *Store) migrateVideoArtifacts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS video_artifact_jobs (
		source_artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id),
		person_id TEXT NOT NULL REFERENCES persons(id),
		status TEXT NOT NULL CHECK(status IN ('queued','processing','ready','failed')),
		error TEXT NOT NULL DEFAULT '',
		fps REAL NOT NULL,
		max_frames INTEGER NOT NULL,
		max_duration INTEGER NOT NULL,
		timeout_seconds INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS video_artifact_frames (
		source_artifact_id TEXT NOT NULL REFERENCES video_artifact_jobs(source_artifact_id),
		frame_artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id),
		timestamp_ms INTEGER NOT NULL CHECK(timestamp_ms >= 0),
		timestamp_kind TEXT NOT NULL DEFAULT 'nominal-grid',
		ordinal INTEGER NOT NULL,
		UNIQUE(source_artifact_id,ordinal),
		UNIQUE(source_artifact_id,timestamp_ms)
	);
	CREATE INDEX IF NOT EXISTS video_jobs_person ON video_artifact_jobs(person_id,status);`)
	if err != nil {
		return err
	}
	// Older T12 databases persist i/FPS timestamps and have no kind column.
	// Retain their nominal-grid marker; do not relabel them as source PTS.
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(video_artifact_frames)`)
	if err != nil {
		return err
	}
	hasKind := false
	for rows.Next() {
		var number, required, primary int
		var name, kind string
		var defaultValue sql.NullString
		if err = rows.Scan(&number, &name, &kind, &required, &defaultValue, &primary); err != nil {
			break
		}
		if name == "timestamp_kind" {
			hasKind = true
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if hasKind {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE video_artifact_frames ADD COLUMN timestamp_kind TEXT NOT NULL DEFAULT 'nominal-grid'`)
	return err
}

const MaxPendingVideoJobs = 32

var ErrVideoQueueFull = errors.New("video: processing queue full")

func validateVideoJob(j VideoJob) error {
	if j.FPS < .25 || j.FPS > 4 || j.MaxFrames < 1 || j.MaxFrames > 16 || j.MaxDuration < 1 || j.MaxDuration > 60 || j.TimeoutSeconds < 1 || j.TimeoutSeconds > 120 {
		return errors.New("video: invalid processing limits")
	}
	return nil
}

func insertVideoJobTx(ctx context.Context, tx *sql.Tx, artifactID, personID string, job VideoJob, now time.Time) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_artifact_jobs WHERE source_artifact_id=? AND person_id=?`, artifactID, personID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_artifact_jobs WHERE status IN ('queued','processing')`).Scan(&pending); err != nil {
		return err
	}
	if pending >= MaxPendingVideoJobs {
		return ErrVideoQueueFull
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO video_artifact_jobs
		(source_artifact_id,person_id,status,fps,max_frames,max_duration,timeout_seconds,created_at,updated_at)
		VALUES(?,?,'queued',?,?,?,?,?,?)`,
		artifactID, personID, job.FPS, job.MaxFrames, job.MaxDuration, job.TimeoutSeconds, timestamp(now), timestamp(now))
	return err
}

// Requeue in-flight work after process restart. Originals are never removed.
func (s *Store) RecoverVideoJobs(ctx context.Context, personID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='queued',updated_at=? WHERE person_id=? AND status='processing'`, timestamp(s.now()), personID)
	return err
}

// Claim one queued conversion transactionally so two conversion workers never transcode the same job.
func (s *Store) ClaimVideoJob(ctx context.Context, personID string) (VideoJob, bool, error) {
	var id string
	err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT source_artifact_id FROM video_artifact_jobs WHERE person_id=? AND status='queued' ORDER BY created_at,source_artifact_id LIMIT 1`, personID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			id = ""
			return nil
		}
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='processing',updated_at=? WHERE person_id=? AND source_artifact_id=? AND status='queued'`, timestamp(s.now()), personID, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			id = ""
			return nil
		}
		return nil
	})
	if err != nil {
		return VideoJob{}, false, err
	}
	if id == "" {
		return VideoJob{}, false, nil
	}
	j, err := s.VideoJob(ctx, personID, id)
	return j, err == nil, err
}

func (s *Store) RetryFailedVideo(ctx context.Context, personID, artifactID string) (VideoJob, bool, error) {
	var restarted bool
	err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM video_artifact_jobs WHERE person_id=? AND source_artifact_id=?`, personID, artifactID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != "failed" {
			return nil
		}
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_artifact_jobs WHERE status IN ('queued','processing')`).Scan(&pending); err != nil {
			return err
		}
		if pending >= MaxPendingVideoJobs {
			return ErrVideoQueueFull
		}
		res, err := tx.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='queued',error='',updated_at=? WHERE person_id=? AND source_artifact_id=? AND status='failed'`, timestamp(s.now()), personID, artifactID)
		if err != nil {
			return err
		}
		n, e := res.RowsAffected()
		restarted = n == 1
		return e
	})
	if err != nil {
		return VideoJob{}, false, err
	}
	j, err := s.VideoJob(ctx, personID, artifactID)
	return j, restarted, err
}

func (s *Store) RegisterVideo(ctx context.Context, personID, artifactID string, fps float64, maxFrames, maxDuration, timeout int) (VideoJob, error) {
	if personID == "" || artifactID == "" {
		return VideoJob{}, errors.New("video: missing identity")
	}
	if err := validateVideoJob(VideoJob{FPS: fps, MaxFrames: maxFrames, MaxDuration: maxDuration, TimeoutSeconds: timeout}); err != nil {
		return VideoJob{}, err
	}
	err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		var mediaType string
		if err := tx.QueryRowContext(ctx, `SELECT media_type FROM artifacts WHERE person_id = ? AND id = ?`, personID, artifactID).Scan(&mediaType); err != nil {
			return err
		}
		if mediaType != "video/mp4" && mediaType != "video/webm" && mediaType != "video/quicktime" {
			return errors.New("video: unsupported original media type")
		}
		return insertVideoJobTx(ctx, tx, artifactID, personID, VideoJob{FPS: fps, MaxFrames: maxFrames, MaxDuration: maxDuration, TimeoutSeconds: timeout}, s.now())
	})
	if err != nil {
		return VideoJob{}, err
	}
	return s.VideoJob(ctx, personID, artifactID)
}

func (s *Store) VideoJob(ctx context.Context, personID, artifactID string) (VideoJob, error) {
	var j VideoJob
	err := s.db.QueryRowContext(ctx, `SELECT source_artifact_id,person_id,status,error,fps,max_frames,max_duration,timeout_seconds
		FROM video_artifact_jobs WHERE person_id=? AND source_artifact_id=?`, personID, artifactID).Scan(&j.ArtifactID, &j.PersonID, &j.Status, &j.Error, &j.FPS, &j.MaxFrames, &j.MaxDuration, &j.TimeoutSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

func (s *Store) PendingVideoJobs(ctx context.Context, personID string) ([]VideoJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_artifact_id FROM video_artifact_jobs WHERE person_id=? AND status IN ('queued','processing')`, personID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var jobs []VideoJob
	for _, id := range ids {
		j, e := s.VideoJob(ctx, personID, id)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Store) FailVideo(ctx context.Context, personID, artifactID, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='failed',error=?,updated_at=?
		WHERE person_id=? AND source_artifact_id=? AND status='processing'`, reason, timestamp(s.now()), personID, artifactID)
	return err
}

func (s *Store) CompleteVideo(ctx context.Context, personID, artifactID string, frames []VideoFrameData) error {
	if len(frames) == 0 || len(frames) > 16 {
		return errors.New("video: invalid frame count")
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM video_artifact_jobs WHERE person_id=? AND source_artifact_id=?`, personID, artifactID).Scan(&status)
		if err != nil {
			return err
		}
		if status == "ready" {
			return nil
		}
		if status != "processing" {
			return ErrInvalidTransition
		}
		var conversationID string
		if err = tx.QueryRowContext(ctx, `SELECT conversation_id FROM artifacts WHERE id=? AND person_id=?`, artifactID, personID).Scan(&conversationID); err != nil {
			return err
		}
		last := int64(-1)
		for i, frame := range frames {
			if frame.TimestampMS <= last || len(frame.Data) == 0 || len(frame.Data) > MaxArtifactBytes {
				return fmt.Errorf("video: invalid frame %d", i)
			}
			last = frame.TimestampMS
			kind := frame.TimestampKind
			if kind == "" {
				kind = "nominal-grid"
			} // legacy/manual processor inputs
			if kind != "nominal-grid" && kind != "source-pts-relative" {
				return fmt.Errorf("video: invalid timestamp kind %q", kind)
			}
			id := newID("art")
			digest := sha256.Sum256(frame.Data)
			name := fmt.Sprintf("video_%s_%03d.webp", artifactID, i)
			_, err = tx.ExecContext(ctx, `INSERT INTO artifacts(id,person_id,conversation_id,idempotency_key,name,media_type,size,sha256,body,created_at)
				VALUES(?,?,?,?,?,?,?,?,?,?)`, id, personID, conversationID, fmt.Sprintf("video-frame:%s:%d", artifactID, i), name, "image/webp", len(frame.Data), hex.EncodeToString(digest[:]), frame.Data, timestamp(s.now()))
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO video_artifact_frames(source_artifact_id,frame_artifact_id,timestamp_ms,timestamp_kind,ordinal) VALUES(?,?,?,?,?)`, artifactID, id, frame.TimestampMS, kind, i)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='ready',error='',updated_at=? WHERE source_artifact_id=? AND status='processing'`, timestamp(s.now()), artifactID)
		return err
	})
}

func (s *Store) populateVideoRelation(ctx context.Context, personID string, a *Artifact) error {
	var source, status, reason string
	err := s.db.QueryRowContext(ctx, `SELECT j.source_artifact_id,j.status,j.error FROM video_artifact_jobs j
		WHERE j.person_id=? AND j.source_artifact_id=?`, personID, a.ID).Scan(&source, &status, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRowContext(ctx, `SELECT f.source_artifact_id FROM video_artifact_frames f JOIN video_artifact_jobs j
			ON j.source_artifact_id=f.source_artifact_id WHERE j.person_id=? AND f.frame_artifact_id=?`, personID, a.ID).Scan(&source)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		a.SourceArtifactID = source
		a.ProcessingStatus = "ready"
		return nil
	}
	if err != nil {
		return err
	}
	a.ProcessingStatus = status
	a.ProcessingError = reason
	if status != "ready" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT frame_artifact_id,timestamp_ms,timestamp_kind FROM video_artifact_frames
		WHERE source_artifact_id=? ORDER BY ordinal`, a.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	a.VideoFrames = []VideoFrame{}
	for rows.Next() {
		var frame VideoFrame
		if err := rows.Scan(&frame.ArtifactID, &frame.TimestampMS, &frame.TimestampKind); err != nil {
			return err
		}
		a.VideoFrames = append(a.VideoFrames, frame)
	}
	return rows.Err()
}
