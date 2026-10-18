package core

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "fmt"
)

// ArtifactIntegrityReport is an owner-scoped inventory for T16 restore
// verification. It does not claim to be a backup or a native session snapshot.
type ArtifactIntegrityReport struct {
 Count int `json:"count"`
 Bytes int64 `json:"bytes"`
}

// VerifyOwnerArtifacts checks the central SQLite-backed originals with the
// same checksums used for owner downloads. Missing/corrupt payloads fail closed.
func (s *Store) VerifyOwnerArtifacts(ctx context.Context, personID string)(ArtifactIntegrityReport,error){
 if personID==""{return ArtifactIntegrityReport{},errors.New("missing owner identity")}
 rows,err:=s.db.QueryContext(ctx,`SELECT id, size, sha256, body FROM artifacts WHERE person_id = ? ORDER BY id`,personID)
 if err!=nil{return ArtifactIntegrityReport{},err}
 defer rows.Close()
 var report ArtifactIntegrityReport
 for rows.Next(){
  var id, expected string
  var size int64
  var body []byte
  if err:=rows.Scan(&id,&size,&expected,&body);err!=nil{return ArtifactIntegrityReport{},err}
  sum:=sha256.Sum256(body)
  if int64(len(body))!=size||hex.EncodeToString(sum[:])!=expected{
   return ArtifactIntegrityReport{},fmt.Errorf("artifact integrity verification failed for %s",id)
  }
  report.Count++
  report.Bytes+=size
 }
 if err:=rows.Err();err!=nil{return ArtifactIntegrityReport{},err}
 return report,nil
}
