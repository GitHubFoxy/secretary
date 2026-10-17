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
	ArtifactID string  `json:"artifact_id"`
	TimestampMS int64  `json:"timestamp_ms"`
}

type VideoFrameData struct {
	TimestampMS int64
	Data []byte
}

type VideoJob struct {
	ArtifactID string
	PersonID string
	Status string
	Error string
	FPS float64
	MaxFrames int
	MaxDuration int
	TimeoutSeconds int
}

func (s *Store) migrateVideoArtifacts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS video_artifact_jobs (
		source_artifact_id TEXT PRIMARY KEY REFERENCES artifacts(id),
		person_id TEXT NOT NULL REFERENCES persons(id),
		status TEXT NOT NULL CHECK(status IN ('processing','ready','failed')),
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
		ordinal INTEGER NOT NULL,
		UNIQUE(source_artifact_id,ordinal),
		UNIQUE(source_artifact_id,timestamp_ms)
	);
	CREATE INDEX IF NOT EXISTS video_jobs_person ON video_artifact_jobs(person_id,status);`)
	return err
}

func (s *Store) RegisterVideo(ctx context.Context, personID, artifactID string, fps float64, maxFrames, maxDuration, timeout int) (VideoJob, error) {
	if personID == "" || artifactID == "" || fps < .25 || fps > 4 || maxFrames < 1 || maxFrames > 16 || maxDuration < 1 || maxDuration > 60 || timeout < 1 || timeout > 120 {
		return VideoJob{}, errors.New("video: invalid processing limits")
	}
	err := withTxErr(s, ctx, func(tx *sql.Tx) error {
		var mediaType string
		if err := tx.QueryRowContext(ctx, `SELECT media_type FROM artifacts WHERE person_id = ? AND id = ?`, personID, artifactID).Scan(&mediaType); err != nil { return err }
		if mediaType != "video/mp4" && mediaType != "video/webm" && mediaType != "video/quicktime" {
			return errors.New("video: unsupported original media type")
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO video_artifact_jobs
			(source_artifact_id,person_id,status,fps,max_frames,max_duration,timeout_seconds,created_at,updated_at)
			VALUES(?,?,'processing',?,?,?,?,?,?)`, artifactID, personID, fps, maxFrames, maxDuration, timeout, timestamp(s.now()), timestamp(s.now()))
		return err
	})
	if err != nil { return VideoJob{}, err }
	return s.VideoJob(ctx, personID, artifactID)
}

func (s *Store) VideoJob(ctx context.Context, personID, artifactID string) (VideoJob, error) {
	var j VideoJob
	err := s.db.QueryRowContext(ctx, `SELECT source_artifact_id,person_id,status,error,fps,max_frames,max_duration,timeout_seconds
		FROM video_artifact_jobs WHERE person_id=? AND source_artifact_id=?`, personID, artifactID).Scan(&j.ArtifactID,&j.PersonID,&j.Status,&j.Error,&j.FPS,&j.MaxFrames,&j.MaxDuration,&j.TimeoutSeconds)
	if errors.Is(err,sql.ErrNoRows) { return j,ErrNotFound }
	return j,err
}

func (s *Store) PendingVideoJobs(ctx context.Context, personID string) ([]VideoJob,error) {
	rows,err:=s.db.QueryContext(ctx, `SELECT source_artifact_id FROM video_artifact_jobs WHERE person_id=? AND status='processing'`, personID)
	if err!=nil { return nil,err }
	var ids []string
	for rows.Next() { var id string; if err=rows.Scan(&id);err!=nil { rows.Close();return nil,err };ids=append(ids,id) }
	err=rows.Err();rows.Close()
	if err!=nil { return nil,err }
	var jobs []VideoJob
	for _,id:=range ids { j,e:=s.VideoJob(ctx,personID,id);if e!=nil {return nil,e};jobs=append(jobs,j) }
	return jobs,nil
}

func (s *Store) FailVideo(ctx context.Context, personID, artifactID, reason string) error {
	if len(reason)>500 { reason=reason[:500] }
	_,err:=s.db.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='failed',error=?,updated_at=?
		WHERE person_id=? AND source_artifact_id=? AND status='processing'`, reason,timestamp(s.now()),personID,artifactID)
	return err
}

func (s *Store) CompleteVideo(ctx context.Context, personID, artifactID string, frames []VideoFrameData) error {
	if len(frames)==0 || len(frames)>16 { return errors.New("video: invalid frame count") }
	return withTxErr(s,ctx,func(tx *sql.Tx) error {
		var status string
		err:=tx.QueryRowContext(ctx, `SELECT status FROM video_artifact_jobs WHERE person_id=? AND source_artifact_id=?`,personID,artifactID).Scan(&status)
		if err!=nil{return err}
		if status=="ready" {return nil}
		if status!="processing" {return ErrInvalidTransition}
		var conversationID string
		if err=tx.QueryRowContext(ctx, `SELECT conversation_id FROM artifacts WHERE id=? AND person_id=?`,artifactID,personID).Scan(&conversationID);err!=nil{return err}
		last:=int64(-1)
		for i,frame:=range frames {
			if frame.TimestampMS<=last || len(frame.Data)==0 || len(frame.Data)>MaxArtifactBytes {return fmt.Errorf("video: invalid frame %d",i)}
			last=frame.TimestampMS
			id:=newID("art")
			digest:=sha256.Sum256(frame.Data)
			name:=fmt.Sprintf("video_%s_%03d.webp",artifactID,i)
			_,err=tx.ExecContext(ctx, `INSERT INTO artifacts(id,person_id,conversation_id,idempotency_key,name,media_type,size,sha256,body,created_at)
				VALUES(?,?,?,?,?,?,?,?,?,?)`,id,personID,conversationID,fmt.Sprintf("video-frame:%s:%d",artifactID,i),name,"image/webp",len(frame.Data),hex.EncodeToString(digest[:]),frame.Data,timestamp(s.now()))
			if err!=nil{return err}
			_,err=tx.ExecContext(ctx, `INSERT INTO video_artifact_frames(source_artifact_id,frame_artifact_id,timestamp_ms,ordinal) VALUES(?,?,?,?)`,artifactID,id,frame.TimestampMS,i)
			if err!=nil{return err}
		}
		_,err=tx.ExecContext(ctx, `UPDATE video_artifact_jobs SET status='ready',error='',updated_at=? WHERE source_artifact_id=? AND status='processing'`,timestamp(s.now()),artifactID)
		return err
	})
}

func (s *Store) populateVideoRelation(ctx context.Context, personID string, a *Artifact) error {
	var source,status,reason string
	err:=s.db.QueryRowContext(ctx, `SELECT j.source_artifact_id,j.status,j.error FROM video_artifact_jobs j
		WHERE j.person_id=? AND j.source_artifact_id=?`,personID,a.ID).Scan(&source,&status,&reason)
	if errors.Is(err,sql.ErrNoRows) {
		err=s.db.QueryRowContext(ctx, `SELECT f.source_artifact_id FROM video_artifact_frames f JOIN video_artifact_jobs j
			ON j.source_artifact_id=f.source_artifact_id WHERE j.person_id=? AND f.frame_artifact_id=?`,personID,a.ID).Scan(&source)
		if errors.Is(err,sql.ErrNoRows) {return nil}
		if err!=nil {return err}
		a.SourceArtifactID=source
		a.ProcessingStatus="ready"
		return nil
	}
	if err!=nil{return err}
	a.ProcessingStatus=status
	a.ProcessingError=reason
	if status!="ready" {return nil}
	rows,err:=s.db.QueryContext(ctx, `SELECT frame_artifact_id,timestamp_ms FROM video_artifact_frames
		WHERE source_artifact_id=? ORDER BY ordinal`,a.ID)
	if err!=nil{return err}
	defer rows.Close()
	a.VideoFrames=[]VideoFrame{}
	for rows.Next() {
		var frame VideoFrame
		if err:=rows.Scan(&frame.ArtifactID,&frame.TimestampMS);err!=nil{return err}
		a.VideoFrames=append(a.VideoFrames,frame)
	}
	return rows.Err()
}

var _ = time.Second
