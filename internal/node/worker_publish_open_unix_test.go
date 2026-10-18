//go:build linux || darwin || freebsd || netbsd || openbsd

package node

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestT09OpenNoFollowRefusesSymlinkInAnyComponent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "real")
	if err := os.Mkdir(dir, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dir,"safe.zip"),[]byte("ok"),0600); err != nil { t.Fatal(err) }
	if err := os.Symlink(dir,filepath.Join(root,"linked")); err != nil { t.Skip(err) }
	if err := os.Symlink(filepath.Join(dir,"safe.zip"),filepath.Join(root,"final.zip")); err != nil { t.Skip(err) }
	for _, path := range []string{"linked/safe.zip", "final.zip"} {
		if f,err := openPublishedFileNoFollow(root,path); err == nil {
			f.Close()
			t.Fatalf("symlink accepted in %q",path)
		}
	}
}

// Repeated directory swaps cannot redirect reads outside the pinned workspace
// fd. A concurrent attacker sees errors or the regular in-root file, never
// outside contents.
func TestT09OpenNoFollowResistsDirectorySwap(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside,"file.zip"),[]byte("PRIVATE"),0600);err!=nil { t.Fatal(err) }
	inside := filepath.Join(root,"inside")
	if err := os.Mkdir(inside,0700);err!=nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(inside,"file.zip"),[]byte("PUBLIC"),0600);err!=nil { t.Fatal(err) }
	link := filepath.Join(root,"entry")
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func(){
		defer wg.Done()
		for {
			select { case <-stop:return;default: }
			_ = os.Remove(link)
			_ = os.Symlink(outside,link)
			_ = os.Remove(link)
			_ = os.Symlink(inside,link)
		}
	}()
	defer func(){close(stop);wg.Wait()}()
	for i:=0;i<250;i++{
		f,err := openPublishedFileNoFollow(root,"entry/file.zip")
		if err!=nil {continue}
		var b [10]byte
		n,readErr := f.Read(b[:])
		_ = f.Close()
		if readErr != nil && !errors.Is(readErr,os.ErrClosed) {
			// EOF is allowed after bytes have been read.
			if n==0 {continue}
		}
		if string(b[:n])!="PUBLIC" {t.Fatalf("out-of-workspace symlink race returned %q",b[:n])}
	}
}

func TestT09OpenNoFollowRejectsSymlinkWorkspaceAncestor(t *testing.T) {
	base:=t.TempDir()
	outside:=t.TempDir()
	nested:=filepath.Join(base,"trusted","workspace")
	if err:=os.MkdirAll(nested,0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(nested,"output.zip"),[]byte("PROJECT"),0600);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(outside,"output.zip"),[]byte("SERVER-SECRET"),0600);err!=nil{t.Fatal(err)}
	original:=filepath.Join(base,"trusted")
	bak:=filepath.Join(base,"trusted-hold")
	if err:=os.Rename(original,bak);err!=nil{t.Fatal(err)}
	if err:=os.Symlink(outside,original);err!=nil{t.Fatal(err)}
	defer func(){_ = os.Remove(original);_ = os.Rename(bak,original)}()
	f,err:=openPublishedFileNoFollow(nested,"output.zip")
	if err==nil {f.Close();t.Fatal("symlink in Workspace ancestor was accepted")}
}
