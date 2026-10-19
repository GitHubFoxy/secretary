//go:build !windows

package node

import (
 "archive/tar"
 "bytes"
 "compress/gzip"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func codexFixtureExecutable(t *testing.T, dir, name string) string {
 t.Helper()
 path:=filepath.Join(dir,name)
 if err:=os.WriteFile(path,[]byte("#!/bin/sh\nif [ \"$1\" = \"login\" ]; then exit 1; fi\necho "+name+" 1.2.3\n"),0700);err!=nil {t.Fatal(err)}
 return path
}

func TestManageCodexPairAdoptRepeatAndFailClosedUpgrade(t *testing.T) {
 t.Setenv("HOME",t.TempDir())
 dir:=t.TempDir()
 cli:=codexFixtureExecutable(t,dir,"codex")
 acp:=codexFixtureExecutable(t,dir,"codex-acp")
 ctx:=context.Background()
 if _,err:=ManageCodexPair(ctx,"install","node",cli,acp);err!=nil {t.Fatal(err)}
 previous,err:=readCodexPairUnchecked("node")
 if err!=nil {t.Fatal(err)}
 if _,err:=ManageCodexPair(ctx,"install","node","","");err!=nil {t.Fatal(err)}
 replacement:=codexFixtureExecutable(t,dir,"other-codex")
 if _,err:=ManageCodexPair(ctx,"install","node",replacement,acp);err==nil {t.Fatal("unapproved replacement accepted")}
 if _,err:=ManageCodexPair(ctx,"upgrade","node",replacement,acp);err==nil {t.Fatal("unauthorized candidate upgraded")}
 current,err:=readCodexPairUnchecked("node")
 if err!=nil {t.Fatal(err)}
 if current.Codex.Path!=previous.Codex.Path {t.Fatalf("original pair changed: %s",current.Codex.Path)}
 if _,err:=ManageCodexPair(ctx,"install","server",cli,acp);err!=nil {t.Fatal(err)}
 if err:=os.Rename(cli,cli+".moved");err!=nil {t.Fatal(err)}
 if _,err:=ManageCodexPair(ctx,"env","node","","");err==nil {t.Fatal("moved binary accepted")}
}

func TestFetchCodexExecutableChecksumAndInterruptedArchive(t *testing.T) {
 var buffer bytes.Buffer
 writer:=gzip.NewWriter(&buffer)
 archive:=tar.NewWriter(writer)
 payload:=[]byte("#!/bin/sh\necho codex 0.163.0\n")
 if err:=archive.WriteHeader(&tar.Header{Name:"codex-x86_64",Typeflag:tar.TypeReg,Mode:0700,Size:int64(len(payload))});err!=nil {t.Fatal(err)}
 if _,err:=archive.Write(payload);err!=nil {t.Fatal(err)}
 if err:=archive.Close();err!=nil {t.Fatal(err)}
 if err:=writer.Close();err!=nil {t.Fatal(err)}
 tarball:=buffer.Bytes()
 digest:=sha256.Sum256(tarball)
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){_,_=io.Copy(w,bytes.NewReader(tarball))}))
 defer server.Close()
 stage:=t.TempDir()
 asset:=codexAsset{URL:server.URL,SHA256:hex.EncodeToString(digest[:]),Name:"codex"}
 if err:=fetchCodexExecutable(context.Background(),asset,stage);err!=nil {t.Fatal(err)}
 if got,err:=os.ReadFile(filepath.Join(stage,"codex"));err!=nil || !bytes.Equal(got,payload) {t.Fatalf("staged binary mismatch: %v",err)}
 changed:=asset
 changed.SHA256=strings.Repeat("0",64)
 if err:=fetchCodexExecutable(context.Background(),changed,t.TempDir());err==nil {t.Fatal("altered artifact accepted")}
 truncated:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){_,_=w.Write(tarball[:len(tarball)/2])}))
 defer truncated.Close()
 failed:=asset
 failed.URL=truncated.URL
 if err:=fetchCodexExecutable(context.Background(),failed,t.TempDir());err==nil {t.Fatal("interrupted download accepted")}
}

func TestManagedCodexPairPinsSupportedPlatforms(t *testing.T) {
 assets,err:=codexNativeAssets()
 if err!=nil {
  t.Skipf("host not supported: %v",err)
 }
 if len(assets)!=2 {t.Fatalf("expected CLI and ACP pair, got %d",len(assets))}
 for _,asset:=range assets {
  if asset.URL=="" || len(asset.SHA256)!=64 || asset.Name=="" {t.Fatalf("incomplete pin: %+v",asset)}
 }
}


func TestInstallManagedCodexAssetsFreshRetryPartialAndCorrupt(t *testing.T) {
 createArchive := func(name string) []byte {
  var buf bytes.Buffer
  z := gzip.NewWriter(&buf)
  archive := tar.NewWriter(z)
  payload := []byte("#!/bin/sh\\necho " + name + " 1.2.3\\n")
  if err := archive.WriteHeader(&tar.Header{Name:name+"-native",Typeflag:tar.TypeReg,Mode:0700,Size:int64(len(payload))}); err != nil { t.Fatal(err) }
  if _,err := archive.Write(payload); err != nil { t.Fatal(err) }
  if err := archive.Close(); err != nil { t.Fatal(err) }
  if err := z.Close(); err != nil { t.Fatal(err) }
  return buf.Bytes()
 }
 cliArchive := createArchive("codex")
 acpArchive := createArchive("codex-acp")
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/codex" { _,_=w.Write(cliArchive) } else { _,_=w.Write(acpArchive) }
 }))
 defer server.Close()
 digest := func(data []byte) string { sum:=sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
 assets := []codexAsset{{URL:server.URL+"/codex",SHA256:digest(cliArchive),Name:"codex"},{URL:server.URL+"/acp",SHA256:digest(acpArchive),Name:"codex-acp"}}
 root := t.TempDir()
 cli,acp,err := installManagedCodexAssets(context.Background(),assets,root,"fixture")
 if err != nil { t.Fatal(err) }
 if _,err := os.Stat(cli);err != nil { t.Fatal(err) }
 if _,err := os.Stat(acp);err != nil { t.Fatal(err) }
 if _,_,err := installManagedCodexAssets(context.Background(),assets,root,"fixture");err != nil { t.Fatal(err) }
 if err:=os.Remove(acp);err != nil { t.Fatal(err) }
 if _,_,err:=installManagedCodexAssets(context.Background(),assets,root,"fixture");err==nil { t.Fatal("partial managed installation accepted") }
 broken:=[]codexAsset{{URL:server.URL+"/codex",SHA256:strings.Repeat("0",64),Name:"codex"},assets[1]}
 if _,_,err:=installManagedCodexAssets(context.Background(),broken,root,"corrupt");err==nil { t.Fatal("bad archive accepted") }
 if _,err:=os.Stat(filepath.Join(root,"corrupt"));!os.IsNotExist(err) { t.Fatalf("damaged release activated: %v",err) }
}
