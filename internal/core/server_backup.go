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

// RestoreServerBackup restores to NEW paths only while Server is stopped.
// All archive members are verified before installation. No existing Server state
// is overwritten. On failure any newly installed paths are rolled back.
func RestoreServerBackup(ctx context.Context,archive,dbDestination string,extraDestinations map[string]string) (err error){
 if archive==""||dbDestination==""{return errors.New("restore paths are required")}
 zr,e:=zip.OpenReader(archive);if e!=nil{return e}
 defer zr.Close()
 entries:=map[string]*zip.File{}
 var manifest serverBackupManifest
 for _,f:=range zr.File{
  if _,ok:=entries[f.Name];ok{return fmt.Errorf("duplicate archive member %s",f.Name)}
  if f.Name=="manifest.json"||f.Name=="server.sqlite"||strings.HasPrefix(f.Name,"extras/")&&filepath.Base(f.Name)==strings.TrimPrefix(f.Name,"extras/") {
   entries[f.Name]=f
  }else{return fmt.Errorf("unexpected archive member %s",f.Name)}
 }
 mf:=entries["manifest.json"];if mf==nil{return errors.New("missing backup manifest")}
 reader,e:=mf.Open();if e!=nil{return e}
 data,e:=io.ReadAll(io.LimitReader(reader,1<<20));reader.Close();if e!=nil{return e}
 if e=json.Unmarshal(data,&manifest);e!=nil{return e}
 if manifest.Version!=1||len(manifest.Files)==0||manifest.Files["server.sqlite"]==""||len(entries)!=len(manifest.Files)+1{return errors.New("unsupported or incomplete backup manifest")}
 destinations:=map[string]string{"server.sqlite":dbDestination}
 for name,path:=range extraDestinations{
  if filepath.Base(name)!=name||name==""||path==""{return errors.New("invalid restore extra")}
  destinations["extras/"+name]=path
 }
 if len(destinations)!=len(manifest.Files){return errors.New("restore destinations do not match manifest")}
 staged:=map[string]string{}
 defer func(){for _,p:=range staged{os.Remove(p)}}()
 for name,hash:=range manifest.Files{
  f:=entries[name];dst:=destinations[name]
  if f==nil||dst==""||len(hash)!=64{return fmt.Errorf("missing or unexpected backup member %s",name)}
  if _,e=os.Lstat(dst);e==nil{return fmt.Errorf("restore destination already exists: %s",dst)}else if !errors.Is(e,os.ErrNotExist){return e}
  if f.UncompressedSize64>1<<34{return errors.New("backup member exceeds size limit")}
  tmp,e:=os.CreateTemp(filepath.Dir(dst),".secretary-restore-*")
  if e!=nil{return e}
  staged[dst]=tmp.Name()
  reader,e:=f.Open();if e!=nil{tmp.Close();return e}
  h:=sha256.New()
  n,e:=io.Copy(io.MultiWriter(tmp,h),io.LimitReader(reader,1<<34+1))
  reader.Close()
  if e!=nil||uint64(n)!=f.UncompressedSize64||hex.EncodeToString(h.Sum(nil))!=hash{tmp.Close();return fmt.Errorf("backup member failed integrity check: %s",name)}
  if e=tmp.Sync();e!=nil{tmp.Close();return e}
  if e=tmp.Close();e!=nil{return e}
 }
 if e=verifySnapshot(ctx,staged[dbDestination]);e!=nil{return e}
 installed:=[]string{}
 for dst,src:=range staged{
  if e=os.Link(src,dst);e!=nil{
   for _,p:=range installed{os.Remove(p)}
   return fmt.Errorf("install restored file: %w",e)
  }
  installed=append(installed,dst)
 }
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
