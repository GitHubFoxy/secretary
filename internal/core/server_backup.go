package core

import (
 "archive/zip"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "strings"
 "database/sql"
)

type serverBackupManifest struct {
 Version int `json:"version"`
 Files map[string]string `json:"files"`
}

// ErrBackupPublishedDurabilityUnconfirmed means destination exists and is complete,
// but parent-directory fsync failed. Do not retry using the same name blindly.
var ErrBackupPublishedDurabilityUnconfirmed = errors.New("backup published but durability is unconfirmed")

// Test-only injection at the post-link backup directory-sync boundary.
var backupPublicationSyncHook func(string) error

// CreateServerBackup writes a consistent SQLite snapshot (including Artifact BLOBs,
// ownership, results and memory/config records) into a checked archive. The caller
// must separately supply any external memory/config files as named regular files.
// The archive is published only after all checks and fsync have succeeded.
func (s *Store) CreateServerBackup(ctx context.Context, destination string, extras map[string]string) (err error) {
 if destination=="" {return errors.New("backup destination is required")}
 if _,e:=os.Lstat(destination);e==nil{return errors.New("backup destination already exists")}else if !errors.Is(e,os.ErrNotExist){return e}
 dir:=filepath.Dir(destination)
 snapshot,e:=os.CreateTemp(dir,".secretary-snapshot-*.db")
 if e!=nil{return e}
 snapshotPath:=snapshot.Name()
 snapshot.Close()
 os.Remove(snapshotPath)
 defer os.Remove(snapshotPath)
 if _,e=s.db.ExecContext(ctx,"VACUUM INTO "+sqliteStringLiteral(snapshotPath));e!=nil{return fmt.Errorf("snapshot sqlite: %w",e)}
 if e=verifySnapshot(ctx,snapshotPath);e!=nil{return e}
 output,e:=os.CreateTemp(dir,".secretary-backup-*.zip")
 if e!=nil{return e}
 tmp:=output.Name()
 defer os.Remove(tmp)
 zw:=zip.NewWriter(output)
 manifest:=serverBackupManifest{Version:1,Files:map[string]string{}}
 files:=map[string]string{"server.sqlite":snapshotPath}
 for name,path:=range extras {
  if name==""||name=="server.sqlite"||name=="manifest.json"||filepath.Base(name)!=name||strings.ContainsAny(name,"/\\")||path=="" {output.Close();return fmt.Errorf("invalid backup file name %q",name)}
  files["extras/"+name]=path
 }
 for name,path:=range files {
  info,e:=os.Lstat(path)
  if e!=nil||!info.Mode().IsRegular(){output.Close();return fmt.Errorf("backup input %s must be a regular file: %v",name,e)}
  f,e:=os.Open(path);if e!=nil{output.Close();return e}
  entry,e:=zw.Create(name);if e!=nil{f.Close();output.Close();return e}
  h:=sha256.New()
  _,e=io.Copy(io.MultiWriter(entry,h),f);f.Close()
  if e!=nil{output.Close();return e}
  manifest.Files[name]=hex.EncodeToString(h.Sum(nil))
 }
 encoded,e:=json.Marshal(manifest);if e!=nil{output.Close();return e}
 entry,e:=zw.Create("manifest.json");if e!=nil{output.Close();return e}
 if _,e=entry.Write(encoded);e!=nil{output.Close();return e}
 if e=zw.Close();e!=nil{output.Close();return e}
 if e=output.Sync();e!=nil{output.Close();return e}
 if e=output.Close();e!=nil{return e}
 if e=os.Link(tmp,destination);e!=nil{return fmt.Errorf("publish backup without overwrite: %w",e)}
 if backupPublicationSyncHook!=nil { e=backupPublicationSyncHook(dir) } else { e=syncBackupDirectory(dir) }
 if e!=nil{return fmt.Errorf("%w: %s: %v",ErrBackupPublishedDurabilityUnconfirmed,destination,e)}
 return nil
}

func verifySnapshot(ctx context.Context,path string) error {
 db,e:=sql.Open("sqlite","file:"+filepath.ToSlash(path)+"?mode=ro")
 if e!=nil{return e}
 defer db.Close()
 var result string
 if e=db.QueryRowContext(ctx,"PRAGMA integrity_check").Scan(&result);e!=nil{return e}
 if result!="ok"{return fmt.Errorf("sqlite integrity check: %s",result)}
 var invalid int
 if e=db.QueryRowContext(ctx,"SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&invalid);e!=nil{return e}
 if invalid!=0{return fmt.Errorf("sqlite foreign-key integrity check found %d broken references",invalid)}
 rows,e:=db.QueryContext(ctx,"SELECT id,size,sha256,body FROM artifacts")
 if e!=nil{return e}
 defer rows.Close()
 for rows.Next(){
  var id,hash string;var size int64;var body []byte
  if e=rows.Scan(&id,&size,&hash,&body);e!=nil{return e}
  actual:=sha256.Sum256(body)
  if int64(len(body))!=size||hex.EncodeToString(actual[:])!=hash{return fmt.Errorf("corrupt Artifact %s",id)}
 }
 return rows.Err()
}

// restoreInstallHook is a test-only crash/failure injection seam. Real publication
// remains a single directory rename, never a series of per-file installs.
var restoreInstallHook func(string) error

func syncBackupDirectory(path string) error {
 f,err:=os.Open(path);if err!=nil{return err};defer f.Close()
 return f.Sync()
}

// RestoreServerBackup verifies the complete archive in a private sibling directory,
// syncs every file and that directory, then publishes the entire Server state
// through one atomic same-filesystem directory rename. Call only while offline.
// Destination directory must NOT exist; a crash before publication leaves only
// an ignored staging directory, which the next restore removes and reconstructs.
func RestoreServerBackup(ctx context.Context,archive,dbDestination string,extraDestinations map[string]string) error {
 if archive==""||dbDestination==""{return errors.New("restore paths are required")}
 destinationDir:=filepath.Clean(filepath.Dir(dbDestination))
 if destinationDir=="."||destinationDir==string(filepath.Separator){return errors.New("restore requires a dedicated new Server directory")}
 base:=filepath.Base(dbDestination)
 if base=="."||base==".." {return errors.New("invalid database destination")}
 if _,err:=os.Lstat(destinationDir);err==nil{return errors.New("restore requires a nonexistent destination directory")}else if !errors.Is(err,os.ErrNotExist){return err}
 parent:=filepath.Dir(destinationDir)
 if info,err:=os.Lstat(parent);err!=nil||!info.IsDir(){return fmt.Errorf("restore parent unavailable: %v",err)}
 targets:=map[string]string{"server.sqlite":base}
 for name,path:=range extraDestinations{
  if filepath.Base(name)!=name||name==""||path==""||filepath.Clean(filepath.Dir(path))!=destinationDir{
   return fmt.Errorf("restore extras must be within the new Server directory: %q",name)
  }
  targets["extras/"+name]=filepath.Base(path)
 }
 used:=map[string]bool{}
 for _,name:=range targets{
  if used[name]{return errors.New("restore filenames collide")}
  used[name]=true
 }
 zr,err:=zip.OpenReader(archive);if err!=nil{return err};defer zr.Close()
 entries:=map[string]*zip.File{}
 for _,f:=range zr.File{
  if _,ok:=entries[f.Name];ok{return fmt.Errorf("duplicate archive member %s",f.Name)}
  if f.Name!="manifest.json"&&f.Name!="server.sqlite"&&!(strings.HasPrefix(f.Name,"extras/")&&filepath.Base(f.Name)==strings.TrimPrefix(f.Name,"extras/")){
   return fmt.Errorf("unexpected archive member %s",f.Name)
  }
  entries[f.Name]=f
 }
 mf:=entries["manifest.json"];if mf==nil{return errors.New("missing backup manifest")}
 reader,err:=mf.Open();if err!=nil{return err}
 data,err:=io.ReadAll(io.LimitReader(reader,1<<20));reader.Close();if err!=nil{return err}
 var manifest serverBackupManifest
 if err=json.Unmarshal(data,&manifest);err!=nil{return err}
 if manifest.Version!=1||len(manifest.Files)!=len(targets)||len(entries)!=len(targets)+1||manifest.Files["server.sqlite"]=="" {
  return errors.New("unsupported or incomplete backup manifest")
 }
 // A unique, securely created directory avoids collisions with user data.
 // Interrupted, unpublished staging is never assumed to be ours and never
 // recursively deleted by a subsequent restore. An administrator may inspect
 // and remove abandoned private directories explicitly.
 staging,err:=os.MkdirTemp(parent,".secretary-restore-*")
 if err!=nil{return err}
 defer os.RemoveAll(staging)
 if err:=syncBackupDirectory(parent);err!=nil{return err}
 for name,destName:=range targets{
  f:=entries[name];hash:=manifest.Files[name]
  if f==nil||len(hash)!=64{return fmt.Errorf("missing backup member or checksum: %s",name)}
  if f.UncompressedSize64>1<<34{return errors.New("backup member exceeds size limit")}
  dest:=filepath.Join(staging,destName)
  out,err:=os.OpenFile(dest,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil{return err}
  reader,err:=f.Open();if err!=nil{out.Close();return err}
  h:=sha256.New()
  n,copyErr:=io.Copy(io.MultiWriter(out,h),io.LimitReader(reader,(1<<34)+1))
  closeErr:=reader.Close()
  if copyErr!=nil||closeErr!=nil||uint64(n)!=f.UncompressedSize64||hex.EncodeToString(h.Sum(nil))!=hash{
   out.Close();return fmt.Errorf("backup member failed integrity check: %s",name)
  }
  if err=out.Sync();err!=nil{out.Close();return err}
  if err=out.Close();err!=nil{return err}
  if restoreInstallHook!=nil{if err=restoreInstallHook("staged:"+name);err!=nil{return err}}
 }
 if err:=verifySnapshot(ctx,filepath.Join(staging,base));err!=nil{return err}
 if err:=syncBackupDirectory(staging);err!=nil{return err}
 if restoreInstallHook!=nil{if err:=restoreInstallHook("before-publication");err!=nil{return err}}
 if _,err:=os.Lstat(destinationDir);err==nil{return errors.New("restore destination appeared during staging")}else if !errors.Is(err,os.ErrNotExist){return err}
 if err:=os.Rename(staging,destinationDir);err!=nil{return fmt.Errorf("publish restored Server: %w",err)}
 // Rename is the atomic visibility boundary. Even if fsync fails, never
 // destructively roll back the already published complete directory.
 if err:=syncBackupDirectory(parent);err!=nil{return fmt.Errorf("restored Server is published but parent directory sync failed: %w",err)}
 return nil
}

// RestoreServerBackupAuto discovers archive extras and installs them only under
// a fresh Server data directory. This deliberately excludes Node-native data.
func RestoreServerBackupAuto(ctx context.Context, archive, dataDir string) error {
 zr,err:=zip.OpenReader(archive);if err!=nil{return err}
 extras:=map[string]string{}
 for _,f:=range zr.File {
  if !strings.HasPrefix(f.Name,"extras/"){continue}
  name:=strings.TrimPrefix(f.Name,"extras/")
  switch name {
  case "config.toml","user.md","memory.md","preferences.md":
   extras[name]=filepath.Join(dataDir,name)
  default:
   zr.Close()
   return fmt.Errorf("unsupported restore extra: %s",name)
  }
 }
 zr.Close()
 return RestoreServerBackup(ctx,archive,filepath.Join(dataDir,"secretary.db"),extras)
}
