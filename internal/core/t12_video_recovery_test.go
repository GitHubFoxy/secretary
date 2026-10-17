package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestT12CrashWindowOriginalAndJobAtomicWithLegacyOrphanReplay(t *testing.T) {
	ctx:=context.Background()
	path:=filepath.Join(t.TempDir(),"video-recover.db")
	store,err:=Open(ctx,path)
	if err!=nil{t.Fatal(err)}
	person,conv,err:=store.EnsureOwner(ctx)
	if err!=nil{t.Fatal(err)}
	original:=[]byte("original still in database")
	a,dup,err:=store.SaveVideoArtifact(ctx,person.ID,conv.ID,"atomic","clip.mp4","video/mp4",original,1,12,12,45)
	if err!=nil||dup{t.Fatalf("atomic video save: %v duplicate=%v",err,dup)}
	if _,_,err=store.ArtifactForPerson(ctx,person.ID,a.ID);err!=nil{t.Fatal(err)}
	if err=store.Close();err!=nil{t.Fatal(err)}
	store,err=Open(ctx,path)
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	j,err:=store.VideoJob(ctx,person.ID,a.ID)
	if err!=nil||j.Status!="queued"{t.Fatalf("crash after commit must preserve runnable job: %+v %v",j,err)}
	again,dup,err:=store.SaveVideoArtifact(ctx,person.ID,conv.ID,"atomic","clip.mp4","video/mp4",original,1,12,12,45)
	if err!=nil||!dup||again.ID!=a.ID{t.Fatalf("repeated upload lost job or original: %+v %v",again,err)}
	claimed,ok,err:=store.ClaimVideoJob(ctx,person.ID)
	if err!=nil||!ok||claimed.ArtifactID!=a.ID||claimed.Status!="processing"{t.Fatalf("claim queued job: %+v %v",claimed,err)}
	if err=store.RecoverVideoJobs(ctx,person.ID);err!=nil{t.Fatal(err)}
	claimed,ok,err=store.ClaimVideoJob(ctx,person.ID)
	if err!=nil||!ok||claimed.ArtifactID!=a.ID{t.Fatalf("restart did not resume in-flight job: %+v %v",claimed,err)}
	if err=store.FailVideo(ctx,person.ID,a.ID,"failed fixture");err!=nil{t.Fatal(err)}
	retry,restarted,err:=store.RetryFailedVideo(ctx,person.ID,a.ID)
	if err!=nil||!restarted||retry.Status!="queued"{t.Fatalf("explicit retry failed %+v %v",retry,err)}
	_,restarted,err=store.RetryFailedVideo(ctx,person.ID,a.ID)
	if err!=nil||restarted{t.Fatalf("duplicate retry launched processing twice: %v",err)}
	// Reproduce the old crash window as a legacy orphan: original exists, but no video job.
	orphan,dup,err:=store.SaveArtifact(ctx,person.ID,conv.ID,"legacy-crash","old.mp4","video/mp4",original)
	if err!=nil||dup{t.Fatal(err)}
	if _,err=store.VideoJob(ctx,person.ID,orphan.ID);!errors.Is(err,ErrNotFound){t.Fatalf("expected legacy orphan: %v",err)}
	rescued,dup,err:=store.SaveVideoArtifact(ctx,person.ID,conv.ID,"legacy-crash","old.mp4","video/mp4",original,1,12,12,45)
	if err!=nil||!dup||rescued.ID!=orphan.ID{t.Fatalf("retry failed to rescue legacy crash: %+v %v",rescued,err)}
	j,err=store.VideoJob(ctx,person.ID,orphan.ID)
	if err!=nil||j.Status!="queued"{t.Fatalf("rescued job missing: %+v %v",j,err)}
}

func TestT12VideoConversionQueueIsBoundedAndClaimIsExclusive(t *testing.T) {
	ctx:=context.Background()
	store,err:=Open(ctx,filepath.Join(t.TempDir(),"queue.db"))
	if err!=nil{t.Fatal(err)}
	defer store.Close()
	person,conv,err:=store.EnsureOwner(ctx)
	if err!=nil{t.Fatal(err)}
	for i:=0;i<MaxPendingVideoJobs;i++ {
		key:=string(rune('a'+i))+"-video"
		_,_,err:=store.SaveVideoArtifact(ctx,person.ID,conv.ID,key,key+".mp4","video/mp4",[]byte("x"),1,12,12,45)
		if err!=nil{t.Fatalf("queued job %d: %v",i,err)}
	}
	_,_,err=store.SaveVideoArtifact(ctx,person.ID,conv.ID,"overflow","overflow.mp4","video/mp4",[]byte("x"),1,12,12,45)
	if !errors.Is(err,ErrVideoQueueFull){t.Fatalf("expected bounded queue rejection: %v",err)}
	for i:=0;i<MaxPendingVideoJobs;i++ {
		job,ok,err:=store.ClaimVideoJob(ctx,person.ID)
		if err!=nil||!ok{t.Fatalf("claim %d: %+v %v",i,job,err)}
		if err=store.FailVideo(ctx,person.ID,job.ArtifactID,"limit test");err!=nil{t.Fatal(err)}
	}
	if job,ok,err:=store.ClaimVideoJob(ctx,person.ID);err!=nil||ok{t.Fatalf("duplicate claim: %+v %v",job,err)}
	_,_,err=store.SaveVideoArtifact(ctx,person.ID,conv.ID,"overflow","overflow.mp4","video/mp4",[]byte("x"),1,12,12,45)
	if err!=nil{t.Fatalf("queue capacity not released: %v",err)}
}
