package core

import (
 "context"
 "path/filepath"
 "testing"
)

func TestT16OwnerArtifactIntegrityInventory(t *testing.T){
 ctx:=context.Background()
 store,err:=Open(ctx,filepath.Join(t.TempDir(),"artifacts.db"))
 if err!=nil{t.Fatal(err)}
 defer store.Close()
 owner,conversation,err:=store.CreatePersonWithConversation(ctx)
 if err!=nil{t.Fatal(err)}
 body:=[]byte("preserved central Worker output")
 a,duplicate,err:=store.SaveArtifact(ctx,owner.ID,conversation.ID,"t16-report","result.txt","text/plain",body)
 if err!=nil||duplicate{t.Fatalf("save original: %v duplicate=%v",err,duplicate)}
 report,err:=store.VerifyOwnerArtifacts(ctx,owner.ID)
 if err!=nil||report.Count!=1||report.Bytes!=int64(len(body)){t.Fatalf("inventory %+v %v",report,err)}
 if _,_,err:=store.ArtifactForPerson(ctx,owner.ID,a.ID);err!=nil{t.Fatal(err)}
 // A valid SQLite row with tampered bytes must never be reported as backed up.
 if _,err:=store.db.ExecContext(ctx,`UPDATE artifacts SET body=? WHERE id=?`,[]byte("tampered"),a.ID);err!=nil{t.Fatal(err)}
 if _,err:=store.VerifyOwnerArtifacts(ctx,owner.ID);err==nil{t.Fatal("corrupt original passed restore verification")}
}
