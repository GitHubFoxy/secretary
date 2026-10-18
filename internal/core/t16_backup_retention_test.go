package core

import (
 "context"
 "os"
 "path/filepath"
 "testing"
)

func TestT16BackupRestoreArtifactsAndOwner(t *testing.T) {
 ctx:=context.Background()
 source,err:=Open(ctx,filepath.Join(t.TempDir(),"source.db"));if err!=nil{t.Fatal(err)}
 owner,conv,err:=source.CreatePersonWithConversation(ctx);if err!=nil{t.Fatal(err)}
 original,_,err:=source.SaveArtifact(ctx,owner.ID,conv.ID,"backup-test","result.txt","text/plain",[]byte("worker result bytes"));if err!=nil{t.Fatal(err)}
 dir:=t.TempDir()
 extra:=filepath.Join(dir,"memory.md")
 if err=os.WriteFile(extra,[]byte("persistent memory"),0600);err!=nil{t.Fatal(err)}
 archive:=filepath.Join(dir,"server.zip")
 if err=source.CreateServerBackup(ctx,archive,map[string]string{"memory.md":extra});err!=nil{t.Fatal(err)}
 restoredDB:=filepath.Join(dir,"restored.db")
 restoredMemory:=filepath.Join(dir,"restored-memory.md")
 if err=RestoreServerBackup(ctx,archive,restoredDB,map[string]string{"memory.md":restoredMemory});err!=nil{t.Fatal(err)}
 restored,err:=Open(ctx,restoredDB);if err!=nil{t.Fatal(err)}
 a,body,err:=restored.ArtifactForPerson(ctx,owner.ID,original.ID)
 if err!=nil||string(body)!="worker result bytes"||a.SHA256!=original.SHA256{t.Fatalf("restored Artifact %+v %q: %v",a,body,err)}
 restored.Close()
 data,err:=os.ReadFile(restoredMemory);if err!=nil||string(data)!="persistent memory"{t.Fatalf("restored memory %q: %v",data,err)}
 if err=RestoreServerBackup(ctx,archive,restoredDB,map[string]string{"memory.md":restoredMemory});err==nil{t.Fatal("restore overwrote existing Server")}
 if err=os.WriteFile(archive,[]byte("corrupt"),0600);err!=nil{t.Fatal(err)}
 fresh:=filepath.Join(dir,"corrupt-restored.db")
 if err=RestoreServerBackup(ctx,archive,fresh,map[string]string{"memory.md":filepath.Join(dir,"corrupt-memory.md")});err==nil{t.Fatal("corrupt archive restored")}
 if _,err=os.Stat(fresh);!os.IsNotExist(err){t.Fatalf("corrupt restore installed Server: %v",err)}
 source.Close()
}

func TestT16DeletionIsOwnerScopedAndPreservesHistory(t *testing.T){
 ctx:=context.Background();s,err:=Open(ctx,filepath.Join(t.TempDir(),"server.db"));if err!=nil{t.Fatal(err)};defer s.Close()
 owner,conv,err:=s.CreatePersonWithConversation(ctx);if err!=nil{t.Fatal(err)}
 other,_,err:=s.CreatePersonWithConversation(ctx);if err!=nil{t.Fatal(err)}
 original,_,err:=s.SaveArtifact(ctx,owner.ID,conv.ID,"delete-t16","result.txt","text/plain",[]byte("data"));if err!=nil{t.Fatal(err)}
 if err=s.DeleteOwnerArtifact(ctx,other.ID,original.ID);err!=ErrNotFound{t.Fatalf("cross owner delete: %v",err)}
 if _,_,err=s.ArtifactForPerson(ctx,owner.ID,original.ID);err!=nil{t.Fatal(err)}
 if err=s.DeleteOwnerArtifact(ctx,owner.ID,original.ID);err!=nil{t.Fatal(err)}
 if _,_,err=s.ArtifactForPerson(ctx,owner.ID,original.ID);err!=ErrNotFound{t.Fatalf("deleted artifact still readable: %v",err)}
 var count int
 if err=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM conversation_entries WHERE conversation_id=? AND body LIKE '%artifact://%'",conv.ID).Scan(&count);err!=nil||count==0{t.Fatalf("result reference history lost: count %d error %v",count,err)}
}
