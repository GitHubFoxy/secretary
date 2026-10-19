package webapi

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestVideoToolPairValidation(t *testing.T) {
 dir:=t.TempDir()
 oldA,hadA:=os.LookupEnv("SECRETARY_FFMPEG")
 oldB,hadB:=os.LookupEnv("SECRETARY_FFPROBE")
 t.Cleanup(func(){
  if hadA {os.Setenv("SECRETARY_FFMPEG",oldA)}else{os.Unsetenv("SECRETARY_FFMPEG")}
  if hadB {os.Setenv("SECRETARY_FFPROBE",oldB)}else{os.Unsetenv("SECRETARY_FFPROBE")}
 })
 first,second:=filepath.Join(dir,"ffmpeg"),filepath.Join(dir,"ffprobe")
 write:=func(path,tool,version string) {
  t.Helper()
  data:=[]byte("#!/bin/sh\necho '"+tool+" version "+version+"'\n")
  if err:=os.WriteFile(path,data,0700);err!=nil{t.Fatal(err)}
 }
 write(first,"ffmpeg","6.1");write(second,"ffprobe","6.1")
 os.Setenv("SECRETARY_FFMPEG",first);os.Setenv("SECRETARY_FFPROBE",second)
 if a,b,err:=resolveVideoTools(context.Background());err!=nil||a!=first||b!=second{t.Fatalf("compatible pair: %s %s %v",a,b,err)}
 write(second,"ffprobe","7.0")
 if _,_,err:=resolveVideoTools(context.Background());err==nil||!strings.Contains(err.Error(),"incompatible"){t.Fatalf("mismatch accepted: %v",err)}
 os.Remove(second)
 if _,_,err:=resolveVideoTools(context.Background());err==nil{t.Fatal("missing ffprobe accepted")}
 write(second,"ffprobe","6.1")
 os.WriteFile(second,[]byte("not executable"),0600)
 if _,_,err:=resolveVideoTools(context.Background());err==nil{t.Fatal("broken executable accepted")}
 os.Unsetenv("SECRETARY_FFPROBE")
 if _,_,err:=resolveVideoTools(context.Background());err==nil{t.Fatal("partial pair accepted")}
}

func TestVideoManagedManifestIntegrity(t *testing.T) {
 root:=t.TempDir()
 folder:=filepath.Join(root,"bin","video-tools")
 if err:=os.MkdirAll(folder,0700);err!=nil{t.Fatal(err)}
 m:=videoToolManifest{Origin:"test-distribution"}
 for _,tool:=range []string{"ffmpeg","ffprobe"}{
  path:=filepath.Join(folder,tool)
  data:=[]byte("#!/bin/sh\necho '"+tool+" version 6.1'\n")
  if err:=os.WriteFile(path,data,0700);err!=nil{t.Fatal(err)}
  sum:=sha256.Sum256(data)
  entry:=videoToolEntry{Version:"6.1",SHA256:hex.EncodeToString(sum[:])}
  if tool=="ffmpeg"{m.FFmpeg=entry}else{m.FFprobe=entry}
 }
 store:=func() {
  t.Helper()
  b,err:=json.Marshal(m);if err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(filepath.Join(root,"VIDEO-TOOLS.json"),b,0600);err!=nil{t.Fatal(err)}
 }
 store()
 if _,_,err:=verifyManagedVideoTools(context.Background(),root);err!=nil{t.Fatalf("valid managed pair: %v",err)}
 if err:=os.WriteFile(filepath.Join(folder,"ffmpeg"),[]byte("tampered"),0700);err!=nil{t.Fatal(err)}
 if _,_,err:=verifyManagedVideoTools(context.Background(),root);err==nil||!strings.Contains(err.Error(),"integrity"){t.Fatalf("tamper accepted: %v",err)}
 data:=[]byte("#!/bin/sh\necho 'ffmpeg version 6.1'\n")
 os.WriteFile(filepath.Join(folder,"ffmpeg"),data,0700)
 m.FFprobe.Version="7.0";store()
 if _,_,err:=verifyManagedVideoTools(context.Background(),root);err==nil||!strings.Contains(err.Error(),"manifest"){t.Fatalf("wrong manifest accepted: %v",err)}
 m.FFprobe.Version="6.1";store()
 os.Remove(filepath.Join(folder,"ffprobe"))
 if _,_,err:=verifyManagedVideoTools(context.Background(),root);err==nil{t.Fatal("missing managed tool accepted")}
}
