package core

import (
 "context"
 "database/sql"
 "errors"
)

// DeleteOwnerArtifact removes exactly the selected original plus derivatives that
// are no longer referenced by another source. Conversation and Worker Result rows
// remain durable, and deleted artifact:// references subsequently return NotFound.
func (s *Store) DeleteOwnerArtifact(ctx context.Context,owner,id string) error {
 if owner==""||id==""{return ErrNotFound}
 return withTxErr(s,ctx,func(tx *sql.Tx)error{
  var actual string
  if err:=tx.QueryRowContext(ctx,"SELECT person_id FROM artifacts WHERE id=?",id).Scan(&actual);err!=nil{
   if errors.Is(err,sql.ErrNoRows){return ErrNotFound};return err
  }
  if actual!=owner{return ErrNotFound}
  // Find derivatives first; a derivative linked to any other original stays.
  rows,err:=tx.QueryContext(ctx,`SELECT frame_artifact_id FROM video_artifact_frames WHERE source_artifact_id=?`,id)
  if err!=nil{return err}
  var derivatives []string
  for rows.Next(){var v string;if err=rows.Scan(&v);err!=nil{break};derivatives=append(derivatives,v)}
  if e:=rows.Err();err==nil{err=e};rows.Close();if err!=nil{return err}
  var transcript sql.NullString
  err=tx.QueryRowContext(ctx,`SELECT transcript_artifact_id FROM voice_artifact_links WHERE source_artifact_id=? AND person_id=?`,id,owner).Scan(&transcript)
  if err!=nil&&!errors.Is(err,sql.ErrNoRows){return err}
  if transcript.Valid{derivatives=append(derivatives,transcript.String)}
  // First detach links. Explicit selection can also itself be a derivative.
  for _,query:=range []string{
   "DELETE FROM video_artifact_frames WHERE source_artifact_id=? OR frame_artifact_id=?",
   "DELETE FROM video_artifact_jobs WHERE source_artifact_id=?",
   "DELETE FROM voice_artifact_links WHERE source_artifact_id=? OR transcript_artifact_id=?",
  }{
   if _,err=tx.ExecContext(ctx,query,id,id);err!=nil{return err}
  }
  if _,err=tx.ExecContext(ctx,"DELETE FROM artifacts WHERE id=? AND person_id=?",id,owner);err!=nil{return err}
  for _,derived:=range derivatives{
   var references int
   err=tx.QueryRowContext(ctx,`SELECT (SELECT COUNT(*) FROM video_artifact_frames WHERE frame_artifact_id=?)
    +(SELECT COUNT(*) FROM voice_artifact_links WHERE transcript_artifact_id=?)
    +(SELECT COUNT(*) FROM voice_artifact_links WHERE source_artifact_id=?)
    +(SELECT COUNT(*) FROM video_artifact_jobs WHERE source_artifact_id=?)`,derived,derived,derived,derived).Scan(&references)
   if err!=nil{return err}
   if references==0{
    if _,err=tx.ExecContext(ctx,"DELETE FROM artifacts WHERE id=? AND person_id=?",derived,owner);err!=nil{return err}
   }
  }
  return nil
 })
}
