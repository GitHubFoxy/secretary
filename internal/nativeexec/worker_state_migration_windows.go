//go:build windows

package nativeexec

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MigrateWindowsNativeStore moves only selected native-provider state into
// the persistent separately provisioned Worker account profile. Never expose
// the private Node directory or the Server database to that account.
// Existing destination data are never overwritten; interrupted copies are
// staged and retried until an atomic rename succeeds.
func MigrateWindowsNativeStore(source, destination string) error {
	if strings.TrimSpace(source)=="" { return nil }
	src, err := filepath.Abs(source)
	if err!=nil {return err}
	dst, err := filepath.Abs(destination)
	if err!=nil {return err}
	if strings.EqualFold(filepath.Clean(src),filepath.Clean(dst)) {return nil}
	info,err:=os.Lstat(src)
	if errors.Is(err,fs.ErrNotExist) {return nil}
	if err!=nil {return err}
	if info.Mode()&os.ModeSymlink!=0 {return errors.New("native state migration: source symlink forbidden")}
	if dstInfo,dstErr:=os.Lstat(dst);dstErr==nil {
		// First-run provisioning may create empty provider directories.
		// Merge missing files (never overwrite newer Worker checkpoints)
		// instead of silently treating an empty destination as migrated.
		if !info.IsDir() || !dstInfo.IsDir() { return nil }
		return filepath.WalkDir(src,func(path string,d fs.DirEntry,walkErr error)error{
			if walkErr!=nil{return walkErr}
			if d.Type()&os.ModeSymlink!=0 {return fmt.Errorf("native state migration: symlink forbidden: %s",path)}
			rel,err:=filepath.Rel(src,path);if err!=nil{return err}
			if rel=="."{return nil}
			to:=filepath.Join(dst,rel)
			if d.IsDir(){return os.MkdirAll(to,0700)}
			if !d.Type().IsRegular(){return errors.New("native state migration: unsupported file type")}
			if _,statErr:=os.Lstat(to);statErr==nil{return nil}else if !errors.Is(statErr,fs.ErrNotExist){return statErr}
			temp:=to+".t09-copying"
			_ = os.Remove(temp)
			if copyErr:=copyWindowsNativeStateFile(path,temp);copyErr!=nil{return copyErr}
			if _,statErr:=os.Lstat(to);statErr==nil {_ = os.Remove(temp);return nil}
			if renameErr:=os.Rename(temp,to);renameErr!=nil {_ = os.Remove(temp);return renameErr}
			return nil
		})
	}else if !errors.Is(dstErr,fs.ErrNotExist){return dstErr}
	if err:=os.MkdirAll(filepath.Dir(dst),0700);err!=nil{return err}
	temp:=dst+".t09-migrating"
	// Only clean up this known, deliberately separate staging destination.
	if err:=os.RemoveAll(temp);err!=nil{return err}
	if info.IsDir() {
		if err:=os.Mkdir(temp,0700);err!=nil{return err}
		err=filepath.WalkDir(src,func(path string,d fs.DirEntry,walkErr error)error{
			if walkErr!=nil{return walkErr}
			if d.Type()&os.ModeSymlink!=0 {return fmt.Errorf("native state migration: symlink forbidden: %s",path)}
			rel,err:=filepath.Rel(src,path);if err!=nil{return err}
			if rel=="."{return nil}
			to:=filepath.Join(temp,rel)
			if d.IsDir(){return os.Mkdir(to,0700)}
			if !d.Type().IsRegular(){return fmt.Errorf("native state migration: nonregular file: %s",path)}
			return copyWindowsNativeStateFile(path,to)
		})
		if err!=nil{_ = os.RemoveAll(temp);return err}
	}else if info.Mode().IsRegular(){
		if err:=copyWindowsNativeStateFile(src,temp);err!=nil{return err}
	}else{return errors.New("native state migration: unsupported source type")}
	if err:=os.Rename(temp,dst);err!=nil{_ = os.RemoveAll(temp);return err}
	return nil
}

func copyWindowsNativeStateFile(src,dst string)error{
	in,err:=os.Open(src);if err!=nil{return err}
	defer in.Close()
	// Avoid copying server-scale arbitrary datasets by mistake.
	stat,err:=in.Stat();if err!=nil{return err}
	if stat.Size()>256<<20{return fmt.Errorf("native state migration: file exceeds 256 MiB: %s",src)}
	out,err:=os.OpenFile(dst,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600);if err!=nil{return err}
	_,copyErr:=io.Copy(out,io.LimitReader(in,256<<20+1))
	closeErr:=out.Close()
	if copyErr!=nil{return copyErr}
	return closeErr
}

func migrateWindowsNativeProviderHome(sourceHome,workerHome string)error{
	if sourceHome==""||workerHome==""{return errors.New("native state migration: provider home required")}
	for _,name:=range []string{".codex",".claude",".claude.json"}{
		if err:=MigrateWindowsNativeStore(filepath.Join(sourceHome,name),filepath.Join(workerHome,name));err!=nil{
			return fmt.Errorf("native state migration %s: %w",name,err)
		}
	}
	return nil
}
