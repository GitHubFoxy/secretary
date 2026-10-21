//go:build !windows

package node

import (
 "archive/tar"
 "bytes"
 "compress/gzip"
 "context"
 "encoding/json"
 "crypto/sha256"
 "encoding/hex"
 "fmt"
 "sync"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "runtime"
 "testing"
 "time"
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

// The upstream Linux MUSL executable is not guaranteed to fit within 128 MiB.
// An earlier extraction limit silently wrote only the first 128 MiB, leaving a
// checksum-verified archive but an invalid native binary that could SIGSEGV.

func TestFetchCodexExecutableRetriesInterruptedHTTPSBody(t *testing.T) {
 var buffer bytes.Buffer
 zipped := gzip.NewWriter(&buffer)
 tarWriter := tar.NewWriter(zipped)
 payload := []byte("#!/bin/sh\necho codex 0.163.0\n")
 if err := tarWriter.WriteHeader(&tar.Header{Name:"codex-linux",Typeflag:tar.TypeReg,Mode:0700,Size:int64(len(payload))}); err != nil { t.Fatal(err) }
 if _, err := tarWriter.Write(payload); err != nil { t.Fatal(err) }
 if err := tarWriter.Close(); err != nil { t.Fatal(err) }
 if err := zipped.Close(); err != nil { t.Fatal(err) }
 archive := buffer.Bytes()
 hash := sha256.Sum256(archive)
 attempts := 0
 server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  attempts++
  w.Header().Set("Content-Length",fmt.Sprint(len(archive)))
  if attempts == 1 {
   _, _ = w.Write(archive[:len(archive)/2])
   return
  }
  _, _ = w.Write(archive)
 }))
 defer server.Close()
 prior := http.DefaultTransport
 http.DefaultTransport = server.Client().Transport
 defer func() { http.DefaultTransport = prior }()
 asset := codexAsset{URL:server.URL+"/codex.tar.gz",SHA256:hex.EncodeToString(hash[:]),Name:"codex"}
 if err := fetchCodexExecutable(context.Background(),asset,t.TempDir());err != nil {t.Fatal(err)}
 if attempts != 2 {t.Fatalf("expected HTTPS body truncation to retry once, got %d requests",attempts)}
}

func TestDownloadCodexReleaseDoesNotRetryHTTP404(t *testing.T) {
 attempts := 0
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  attempts++
  http.NotFound(w,r)
 }))
 defer server.Close()
 if err:=downloadCodexReleaseWithClient(context.Background(),server.Client(),server.URL,filepath.Join(t.TempDir(),"archive.tar.gz"),0);err==nil || !strings.Contains(err.Error(),"HTTP 404") { t.Fatalf("expected HTTP 404 without retry, got %v",err) }
 if attempts!=1 {t.Fatalf("unexpected HTTP 404 retries: %d",attempts)}
}

func TestCodexReleaseResumesInterruptedRangeToDisk(t *testing.T) {
 var compressed bytes.Buffer
 writer := gzip.NewWriter(&compressed)
 archive := tar.NewWriter(writer)
 payload := []byte("#!/bin/sh\necho codex 0.163.0\n")
 if err := archive.WriteHeader(&tar.Header{Name:"codex-linux",Typeflag:tar.TypeReg,Mode:0700,Size:int64(len(payload))}); err != nil {t.Fatal(err)}
 if _,err:=archive.Write(payload);err!=nil {t.Fatal(err)}
 if err:=archive.Close();err!=nil {t.Fatal(err)}
 if err:=writer.Close();err!=nil {t.Fatal(err)}
 data := compressed.Bytes()
 digest := sha256.Sum256(data)
 half := len(data)/2
 requests := 0
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  requests++
  if requests == 1 {
   if r.Header.Get("Range") != "" { t.Errorf("first request had Range: %q",r.Header.Get("Range")) }
   hijack,ok := w.(http.Hijacker)
   if !ok {t.Error("hijacking unsupported");return}
   conn,_,err:=hijack.Hijack()
   if err!=nil {t.Error(err);return}
   _,_ = fmt.Fprintf(conn,"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",len(data))
   _,_ = conn.Write(data[:half])
   _ = conn.Close()
   return
  }
  if r.Header.Get("Range") != fmt.Sprintf("bytes=%d-",half) {t.Errorf("resume requested wrong byte offset: %q",r.Header.Get("Range"))}
  w.Header().Set("Content-Length",fmt.Sprint(len(data)-half))
  w.Header().Set("Content-Range",fmt.Sprintf("bytes %d-%d/%d",half,len(data)-1,len(data)))
  w.WriteHeader(http.StatusPartialContent)
  _,_ = w.Write(data[half:])
 }))
 defer server.Close()
 asset := codexAsset{URL:server.URL,SHA256:hex.EncodeToString(digest[:]),Name:"codex"}
 stage := t.TempDir()
 if err:=fetchCodexExecutable(context.Background(),asset,stage);err!=nil {t.Fatal(err)}
 if requests!=2 {t.Fatalf("expected one Range resume, got %d requests",requests)}
 received,err:=os.ReadFile(filepath.Join(stage,"codex.tar.gz"))
 if err!=nil || !bytes.Equal(received,data) {t.Fatalf("archive not recovered: %v",err)}
}

func TestCodexReleaseSlowBodyAndRangeIgnored(t *testing.T) {
 payload := []byte("complete-response-with-delayed-body")
 serverRequests := 0
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  serverRequests++
  if r.Header.Get("Range")!="" && r.Header.Get("Range")!="bytes=8-" {t.Errorf("unexpected Range: %s",r.Header.Get("Range"))}
  w.WriteHeader(http.StatusOK)
  _,_ = w.Write(payload[:8])
  w.(http.Flusher).Flush()
  time.Sleep(80*time.Millisecond)
  _,_ = w.Write(payload[8:])
 }))
 defer server.Close()
 target := filepath.Join(t.TempDir(),"archive.tar.gz")
 if err:=os.WriteFile(target,[]byte("badpart!"),0600);err!=nil {t.Fatal(err)}
 if err:=downloadCodexReleaseWithClient(context.Background(),server.Client(),server.URL,target,0);err!=nil {t.Fatal(err)}
 actual,err:=os.ReadFile(target)
 if err!=nil || !bytes.Equal(actual,payload) {t.Fatalf("server ignored Range but archive was not restarted: %v",err)}
 if serverRequests!=1 {t.Fatalf("unexpected retry count: %d",serverRequests)}
 if codexDownloadTimeout<10*time.Minute {t.Fatal("slow Codex download timeout regressed below ten minutes")}
}

func TestCodexReleaseRejectsMismatchedContentRange(t *testing.T) {
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
  w.Header().Set("Content-Range","bytes 0-2/20")
  w.WriteHeader(http.StatusPartialContent)
  _,_ = w.Write([]byte("bad"))
 }))
 defer server.Close()
 target:=filepath.Join(t.TempDir(),"archive.tar.gz")
 if err:=os.WriteFile(target,[]byte("partial"),0600);err!=nil {t.Fatal(err)}
 if err:=downloadCodexReleaseWithClient(context.Background(),server.Client(),server.URL,target,0);err==nil || !strings.Contains(err.Error(),"Content-Range") {t.Fatalf("incorrect resume accepted: %v",err)}
}

func TestUnpackCodexArchivePreservesFullExecutableOver128MiB(t *testing.T) {
 const executableSize = int64(128<<20) + 4096
 var compressed bytes.Buffer
 zipped := gzip.NewWriter(&compressed)
 archive := tar.NewWriter(zipped)
 if err := archive.WriteHeader(&tar.Header{
  Name: "codex-x86_64-unknown-linux-musl", Typeflag: tar.TypeReg,
  Mode: 0700, Size: executableSize,
 }); err != nil { t.Fatal(err) }
 chunk := make([]byte, 1<<20)
 for written := int64(0); written < executableSize; {
  count := int64(len(chunk))
  if remaining := executableSize - written; remaining < count { count = remaining }
  n, err := archive.Write(chunk[:int(count)])
  if err != nil { t.Fatal(err) }
  written += int64(n)
 }
 if err := archive.Close(); err != nil { t.Fatal(err) }
 if err := zipped.Close(); err != nil { t.Fatal(err) }

 stage := t.TempDir()
 asset := codexAsset{Name: "codex", URL: "https://example.invalid/codex.tar.gz"}
 if err := unpackCodexArchive(compressed.Bytes(), asset, stage); err != nil { t.Fatal(err) }
 info, err := os.Stat(filepath.Join(stage, "codex"))
 if err != nil { t.Fatal(err) }
 if info.Size() != executableSize {
  t.Fatalf("native executable truncated: got %d bytes, want %d", info.Size(), executableSize)
 }
}

func TestVersionlessCodexACPDoesNotBlockNativePairInstall(t *testing.T) {
 t.Setenv("HOME", t.TempDir())
 dir := t.TempDir()
 cli := codexFixtureExecutable(t, dir, "codex")
 acp := filepath.Join(dir, "codex-acp")
 // The actual zed-industries/codex-acp v0.16.0 CLI does not provide
 // --version: clap exits 2 for the unexpected option, while --help works.
 err := os.WriteFile(acp, []byte("#!/bin/sh\ncase \"$1\" in\n  --version) echo \"error: unexpected argument '--version' found\" >&2; exit 2 ;;\n  --help) echo 'Usage: codex-acp -c key=value'; exit 0 ;;\n  *) exit 1 ;;\nesac\n"), 0700)
 if err != nil { t.Fatal(err) }

 if _, err := codexBinaryRecord(acp, "external"); err == nil {
  t.Fatal("Codex CLI must not treat --version errors as acceptable")
 }
 external, err := codexACPBinaryRecord(acp, "external")
 if err != nil { t.Fatal(err) }
 if external.Version != codexACPExternalVersionless { t.Fatalf("invented external ACP version: %q", external.Version) }
 if err := verifyCodexACPBinary(external); err != nil { t.Fatalf("external versionless ACP verification: %v", err) }
 managed, err := codexACPBinaryRecord(acp, "managed")
 if err != nil { t.Fatal(err) }
 if managed.Version != codexACPManagedVersionless { t.Fatalf("wrong managed version label: %q", managed.Version) }
 if err := verifyCodexACPBinary(managed); err != nil { t.Fatalf("managed versionless ACP verification: %v", err) }

 if _, err := ManageCodexPair(context.Background(), "install", "node", cli, acp); err != nil { t.Fatalf("native pair install rejected versionless ACP: %v", err) }
 if _, err := ManageCodexPair(context.Background(), "install", "node", "", ""); err != nil { t.Fatalf("repeat install failed: %v", err) }

 if err := os.WriteFile(acp, []byte("#!/bin/sh\necho replaced\n"), 0700); err != nil { t.Fatal(err) }
 if err := verifyCodexACPBinary(external); err == nil || !strings.Contains(err.Error(), "bytes changed") {
  t.Fatalf("expected versionless ACP tampering rejection, got %v", err)
 }
}

func TestCodexACPVersionlessFallbackFailsClosed(t *testing.T) {
 dir := t.TempDir()
 acp := filepath.Join(dir, "codex-acp")
 cases := []struct { name, script string }{
  {"unrelated-exit-two", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'runtime initialization failed' >&2; exit 2; fi\necho help\n"},
  {"broken-help", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo \"error: unexpected argument '--version' found\" >&2; exit 2; fi\nexit 7\n"},
  {"empty-help", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo \"error: unexpected argument '--version' found\" >&2; exit 2; fi\nexit 0\n"},
 }
 for _, tc := range cases {
  t.Run(tc.name, func(t *testing.T) {
   if err := os.WriteFile(acp, []byte(tc.script), 0700); err != nil { t.Fatal(err) }
   if _, err := codexACPBinaryRecord(acp, "external"); err == nil { t.Fatal("invalid ACP probe accepted") }
  })
 }
}

func TestManagedCodexPairPinsSupportedPlatforms(t *testing.T) {
 assets,err:=codexNativeAssets()
 if err!=nil {
  t.Skipf("host not supported: %v",err)
 }
 expected := 1
 if runtime.GOOS=="windows" { expected = 2 }
 if len(assets)!=expected {t.Fatalf("expected %d upstream assets (the modern Unix ACP is bundled), got %d",expected,len(assets))}
 for _,asset:=range assets {
  if asset.URL=="" || len(asset.SHA256)!=64 || asset.Name=="" {t.Fatalf("incomplete pin: %+v",asset)}
 }
}


func TestInstallManagedCodexAssetsFreshRetryPartialAndCorrupt(t *testing.T) {
 createArchive := func(name string) []byte {
  var buf bytes.Buffer
  z := gzip.NewWriter(&buf)
  archive := tar.NewWriter(z)
  version := codexPinnedVersion
  if name=="codex-acp" { version=codexACPPinnedVersion }
  payload := []byte("#!/bin/sh\necho " + name + " " + version + "\n")
  if name=="codex-acp" {
   payload = []byte("#!/bin/sh\ncase \"$1\" in\n  --version) echo \"error: unexpected argument '--version' found\" >&2; exit 2 ;;\n  --help) echo 'Usage: codex-acp -c key=value'; exit 0 ;;\n  *) exit 1 ;;\nesac\n")
  }
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
 if err:=os.WriteFile(cli, []byte("#!/bin/sh\necho codex "+codexPinnedVersion+"\nexit 0\n"),0700);err!=nil {t.Fatal(err)}
 if _,_,err:=installManagedCodexAssets(context.Background(),assets,root,"fixture");err==nil {t.Fatal("same-version executable tampering accepted") }
 secondCLI,secondACP,err:=installManagedCodexAssets(context.Background(),assets,root,"acp-tamper")
 if err!=nil {t.Fatal(err)}
 _=secondCLI
 if err:=os.WriteFile(secondACP,[]byte("#!/bin/sh\necho codex-acp "+codexACPPinnedVersion+"\nexit 0\n"),0700);err!=nil {t.Fatal(err)}
 if _,_,err:=installManagedCodexAssets(context.Background(),assets,root,"acp-tamper");err==nil {t.Fatal("same-version ACP tampering accepted")}
 if err:=os.Remove(acp);err != nil { t.Fatal(err) }
 if _,_,err:=installManagedCodexAssets(context.Background(),assets,root,"fixture");err==nil { t.Fatal("partial managed installation accepted") }
 broken:=[]codexAsset{{URL:server.URL+"/codex",SHA256:strings.Repeat("0",64),Name:"codex"},assets[1]}
 if _,_,err:=installManagedCodexAssets(context.Background(),broken,root,"corrupt");err==nil { t.Fatal("bad archive accepted") }
 if _,err:=os.Stat(filepath.Join(root,"corrupt"));!os.IsNotExist(err) { t.Fatalf("damaged release activated: %v",err) }
}



// An earlier Secretary release can have the same Codex/ACP version numbers
// while carrying a different trusted Bun-compiled ACP archive. Installing B
// must preserve A's files and activate a verified separate B path. It must
// never interpret this ordinary cross-release change as executable tampering.
func TestManagedCodexReleaseRefreshWithDifferentBundledACP(t *testing.T) {
 t.Setenv("HOME",t.TempDir())
 ctx:=context.Background()
 makeArchive:=func(name,variant string) []byte {
  t.Helper()
  var buf bytes.Buffer
  gz:=gzip.NewWriter(&buf)
  tarball:=tar.NewWriter(gz)
  payload:=[]byte("#!/bin/sh\necho codex "+codexPinnedVersion+"\n")
  if name=="codex-acp" {
   payload=[]byte("#!/bin/sh\n# compiled bundle: "+variant+"\ncase \"$1\" in\n --version) echo \"error: unexpected argument '--version' found\" >&2; exit 2 ;;\n --help) echo 'Usage: codex-acp -c key=value'; exit 0 ;;\n *) exit 1 ;;\nesac\n")
  }
  err:=tarball.WriteHeader(&tar.Header{Name:name+"-native",Typeflag:tar.TypeReg,Mode:0700,Size:int64(len(payload))})
  if err!=nil { t.Fatal(err) }
  if _,err=tarball.Write(payload);err!=nil {t.Fatal(err)}
  if err=tarball.Close();err!=nil {t.Fatal(err)}
  if err=gz.Close();err!=nil {t.Fatal(err)}
  return buf.Bytes()
 }
 oldACP:=makeArchive("codex-acp","release-A")
 newACP:=makeArchive("codex-acp","release-B")
 cliArchive:=makeArchive("codex","shared")
 archives:=map[string][]byte{"/codex":cliArchive,"/old-acp":oldACP,"/new-acp":newACP}
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  data,ok:=archives[r.URL.Path]
  if !ok {http.NotFound(w,r);return}
  _,_=w.Write(data)
 }))
 defer server.Close()
 asset:=func(url,name string,data []byte) codexAsset {
  digest:=sha256.Sum256(data)
  return codexAsset{URL:server.URL+url,Name:name,SHA256:hex.EncodeToString(digest[:])}
 }
 codex:=asset("/codex","codex",cliArchive)
 oldAssets:=[]codexAsset{codex,asset("/old-acp","codex-acp",oldACP)}
 newAssets:=[]codexAsset{codex,asset("/new-acp","codex-acp",newACP)}
 root:=filepath.Join(os.Getenv("HOME"),".local","share","secretary","native","releases",codexPinnedVersion+"-"+codexACPPinnedVersion)
 oldCLI,oldPath,err:=installManagedCodexAssets(ctx,oldAssets,root,runtime.GOOS+"-"+runtime.GOARCH)
 if err!=nil {t.Fatal(err)}
 candidateTarget:=managedCodexAssetTarget(runtime.GOOS+"-"+runtime.GOARCH,newAssets)
 if candidateTarget==managedCodexAssetTarget(runtime.GOOS+"-"+runtime.GOARCH,oldAssets) {t.Fatal("different trusted archives aliased to one managed directory")}
 newCLI,newPath,err:=installManagedCodexAssets(ctx,newAssets,root,candidateTarget)
 if err!=nil {t.Fatal(err)}
 if newPath==oldPath || newCLI==oldCLI {t.Fatal("managed rebuild reused old executable paths")}
 if _,_,err:=installManagedCodexAssets(ctx,newAssets,root,candidateTarget);err!=nil {t.Fatalf("idempotent cached B: %v",err)}
 oldRecord,err:=codexBinaryRecord(oldCLI,"managed")
 if err!=nil {t.Fatal(err)}
 oldACPRecord,err:=codexACPBinaryRecord(oldPath,"managed")
 if err!=nil {t.Fatal(err)}
 for _,scope:=range []string{"server","node"} {
  prev:=CodexPairInventory{Schema:1,Scope:scope,Harness:"codex",Codex:oldRecord,ACP:oldACPRecord}
  location,err:=CodexPairInventoryPath(scope)
  if err!=nil {t.Fatal(err)}
  if err:=saveCodexPair(location,prev);err!=nil {t.Fatal(err)}
  if _,err:=refreshManagedCodexInventory(scope,prev,newCLI,newPath);err!=nil {t.Fatal(err)}
  current,err:=readCodexPairUnchecked(scope)
  if err!=nil {t.Fatal(err)}
  if current.Codex.Path!=newCLI || current.ACP.Path!=newPath {t.Fatalf("%s did not switch to B",scope)}
  if current.Compatibility.Validated {t.Fatal("unprobed ACP pair incorrectly marked protocol-valid")}
  backup,err:=os.ReadFile(filepath.Join(filepath.Dir(location),"codex-pair.previous.json"))
  if err!=nil {t.Fatal(err)}
  var previous CodexPairInventory
  if err=json.Unmarshal(backup,&previous);err!=nil {t.Fatal(err)}
  if previous.ACP.Path!=oldPath || previous.Codex.Path!=oldCLI {t.Fatal("old managed pair not available for rollback")}
 }
 if _,err:=os.Stat(oldCLI);err!=nil {t.Fatalf("old CLI deleted: %v",err)}
 if _,err:=os.Stat(oldPath);err!=nil {t.Fatalf("old ACP deleted: %v",err)}
 if err:=verifyManagedCodexArchives(oldAssets,filepath.Join(root,runtime.GOOS+"-"+runtime.GOARCH));err!=nil {t.Fatalf("old release bytes changed: %v",err)}
 if _,err:=refreshManagedCodexInventory("server",CodexPairInventory{Codex:CodexBinaryRecord{Provenance:"external"},ACP:oldACPRecord},newCLI,newPath);err==nil {
  t.Fatal("implicit refresh accepted an external provenance")
 }
 broken:=[]codexAsset{codex,{URL:server.URL+"/new-acp",Name:"codex-acp",SHA256:strings.Repeat("0",64)}}
 if _,_,err:=installManagedCodexAssets(ctx,broken,root,managedCodexAssetTarget(runtime.GOOS+"-"+runtime.GOARCH,broken));err==nil {t.Fatal("tampered archive accepted")}
 active,err:=readCodexPairUnchecked("server")
 if err!=nil || active.ACP.Path!=newPath {t.Fatalf("failed checksum changed active inventory: %v",err)}
}

func TestManageCodexPairValidatedUpgradeAndRollback(t *testing.T) {
 t.Setenv("HOME",t.TempDir())
 dir:=t.TempDir()
 first:=codexFixtureExecutable(t,dir,"codex-first")
 second:=codexFixtureExecutable(t,dir,"codex-second")
 acp:=codexFixtureExecutable(t,dir,"codex-acp")
 ctx:=context.Background()
 calls:=0
 fakeProbe:=func(_ context.Context,pair CodexPairInventory) error {
  calls++
  if pair.Codex.Path=="" || pair.ACP.Path=="" { return fmt.Errorf("missing chosen executable") }
  return nil
 }
 if _,err:=manageCodexPairWithProbe(ctx,"install","node",first,acp,fakeProbe);err!=nil {t.Fatal(err)}
 if _,err:=manageCodexPairWithProbe(ctx,"readiness","node","","",fakeProbe);err!=nil {t.Fatal(err)}
 if _,err:=manageCodexPairWithProbe(ctx,"upgrade","node",second,acp,fakeProbe);err!=nil {t.Fatal(err)}
 selected,err:=ReadCodexPairInventory("node")
 if err!=nil || selected.Codex.Path!=second {t.Fatalf("upgraded inventory: %+v, %v",selected,err)}
 if _,err:=manageCodexPairWithProbe(ctx,"rollback","node","","",fakeProbe);err!=nil {t.Fatal(err)}
 selected,err=ReadCodexPairInventory("node")
 if err!=nil || selected.Codex.Path!=first {t.Fatalf("rolled back inventory: %+v, %v",selected,err)}
 if calls!=3 {t.Fatalf("expected readiness, upgrade, rollback probes, got %d",calls)}
}


func TestCodexNativeACPProtocolReadinessAndFailedCandidate(t *testing.T) {
 dir:=t.TempDir()
 cli:=filepath.Join(dir,"codex")
 if err:=os.WriteFile(cli,[]byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo codex-cli 0.163.0; elif [ \"$1\" = \"login\" ]; then echo Logged in; else exit 4; fi\n"),0700);err!=nil {t.Fatal(err)}
 makeACP:=func(name string, valid bool) string {
  t.Helper()
  path:=filepath.Join(dir,name)
  metadata:="True"
  if !valid {metadata="False"}
  script:=fmt.Sprintf(`#!/usr/bin/env python3
import json,sys
if '--version' in sys.argv:
 print('codex-acp 0.16.0')
 sys.exit(0)
for line in sys.stdin:
 req=json.loads(line)
 with open(%q,'a') as log:
  log.write(req.get('method','')+'\n')
 if req.get('method')=='initialize':
  response={'protocolVersion':1,'agentInfo':{'version':'0.16.0'},'agentCapabilities':{'loadSession':%s},'_meta':{'steering':{'supported':True}}}
 elif req.get('method')=='session/new':
  response={'sessionId':'fixture','models':{'availableModels':[{'modelId':'gpt-fixture[max]'},{'modelId':'gpt-fixture[ultra]'}]}}
 elif req.get('method')=='session/load':
  response={'models':{'availableModels':[{'modelId':'gpt-fixture[max]'}]}}
 else:
  response={}
 print(json.dumps({'jsonrpc':'2.0','id':req.get('id'),'result':response}),flush=True)
`,filepath.Join(dir,name+".log"),metadata)
  if err:=os.WriteFile(path,[]byte(script),0700);err!=nil {t.Fatal(err)}
  return path
 }
 good:=makeACP("good-acp",true)
 bad:=makeACP("bad-acp",false)
 t.Setenv("SECRETARY_ACP_COMMAND",bad)
 pair:=CodexPairInventory{Codex:CodexBinaryRecord{Path:cli},ACP:CodexBinaryRecord{Path:good}}
 if err:=codexNativePairReady(context.Background(),pair);err!=nil {t.Fatalf("selected valid ACP failed: %v",err)}
 wire,err:=os.ReadFile(good+".log")
 if err!=nil {t.Fatal(err)}
 if !strings.Contains(string(wire),"initialize") || !strings.Contains(string(wire),"session/new") || !strings.Contains(string(wire),"session/load") {t.Fatalf("missing required ACP protocol messages: %s",wire)}
  models, efforts, err := (ExecCommandRunner{}).ObserveCodexACPWithCommand(context.Background(), cli, good)
  if err != nil {t.Fatal(err)}
  if len(models)!=1 || string(models[0])!="gpt-fixture" {t.Fatalf("unexpected model catalog: %v",models)}
  if len(efforts)!=2 || string(efforts[0])!="max" || string(efforts[1])!="ultra" {t.Fatalf("reasoning max/ultra dropped: %v",efforts)}
 pair.ACP.Path=bad
 if err:=codexNativePairReady(context.Background(),pair);err==nil {t.Fatal("invalid ACP initialize contract accepted")}
 t.Setenv("HOME",t.TempDir())
 pair.ACP.Path=good
 if _,err:=manageCodexPairWithProbe(context.Background(),"install","server",cli,good,codexNativePairReady);err!=nil {t.Fatal(err)}
 if _,err:=manageCodexPairWithProbe(context.Background(),"readiness","server","","",codexNativePairReady);err!=nil {t.Fatal(err)}
 if err:=CheckCodexStartup(context.Background(),"server");err!=nil {t.Fatalf("validated native CLI and ACP refused startup: %v",err)}
 if _,err:=manageCodexPairWithProbe(context.Background(),"upgrade","server",cli,bad,codexNativePairReady);err==nil {t.Fatal("invalid candidate activated")}
 active,err:=readCodexPairUnchecked("server")
 if err!=nil || active.ACP.Path!=good {t.Fatalf("failed ACP candidate changed active inventory: %+v %v",active,err)}
 if got:=os.Getenv("SECRETARY_ACP_COMMAND");got!=bad {t.Fatal("native ACP readiness mutated process environment")}
}

func TestCodexPendingUpgradeRecoversWithoutLosingLastGood(t *testing.T) {
 t.Setenv("HOME",t.TempDir())
 dir:=t.TempDir()
 first:=codexFixtureExecutable(t,dir,"first")
 second:=codexFixtureExecutable(t,dir,"second")
 third:=codexFixtureExecutable(t,dir,"third")
 acp:=codexFixtureExecutable(t,dir,"codex-acp")
 probe:=func(_ context.Context,_ CodexPairInventory) error{return nil}
 ctx:=context.Background()
 if _,err:=manageCodexPairWithProbe(ctx,"install","node",first,acp,probe);err!=nil {t.Fatal(err)}
 if _,err:=manageCodexPairWithProbe(ctx,"readiness","node","","",probe);err!=nil {t.Fatal(err)}
 if _,err:=manageCodexPairWithProbe(ctx,"upgrade","node",second,acp,probe);err!=nil {t.Fatal(err)}
 pair2,err:=readCodexPairUnchecked("node")
 if err!=nil {t.Fatal(err)}
 location,_:=CodexPairInventoryPath("node")
 rollback:=filepath.Join(filepath.Dir(location),"codex-pair.previous.json")
 original,err:=os.ReadFile(rollback)
 if err!=nil {t.Fatal(err)}
 thirdCLI,err:=codexBinaryRecord(third,"external")
 if err!=nil {t.Fatal(err)}
 candidate:=pair2
 candidate.Codex=thirdCLI
 journal,_:=json.Marshal(codexPendingUpgrade{Previous:pair2,Candidate:candidate})
 pending:=filepath.Join(filepath.Dir(location),"codex-pair.pending.json")
 if err:=os.WriteFile(pending,journal,0600);err!=nil {t.Fatal(err)}
 if err:=recoverPendingCodexUpgrade("node");err!=nil {t.Fatal(err)}
 stable,err:=os.ReadFile(rollback)
 if err!=nil {t.Fatal(err)}
 if !bytes.Equal(stable,original) {t.Fatal("rollback overwritten before activation")}
 if err:=os.WriteFile(pending,journal,0600);err!=nil {t.Fatal(err)}
 if err:=saveCodexPair(location,candidate);err!=nil {t.Fatal(err)}
 if err:=recoverPendingCodexUpgrade("node");err!=nil {t.Fatal(err)}
 latest,err:=os.ReadFile(rollback)
 if err!=nil {t.Fatal(err)}
 var saved CodexPairInventory
 if err:=json.Unmarshal(latest,&saved);err!=nil {t.Fatal(err)}
 if saved.Codex.Path!=second {t.Fatal("last-good upgraded pair not retained across interruption")}
 if _,err:=manageCodexPairWithProbe(ctx,"rollback","node","","",probe);err!=nil {t.Fatal(err)}
 restored,err:=readCodexPairUnchecked("node")
 if err!=nil || restored.Codex.Path!=second {t.Fatalf("rollback recovery failed: %+v %v",restored,err)}
}

func TestConcurrentCodexScopeUpgradeRejectsWithoutOverwritingLastGood(t *testing.T) {
 t.Setenv("HOME", t.TempDir())
 dir:=t.TempDir()
 first:=codexFixtureExecutable(t,dir,"first")
 second:=codexFixtureExecutable(t,dir,"second")
 third:=codexFixtureExecutable(t,dir,"third")
 acp:=codexFixtureExecutable(t,dir,"codex-acp")
 ctx:=context.Background()
 allow:=func(_ context.Context,_ CodexPairInventory) error{return nil}
 if _,err:=manageCodexPairWithProbe(ctx,"install","node",first,acp,allow);err!=nil {t.Fatal(err)}
 if _,err:=manageCodexPairWithProbe(ctx,"readiness","node","","",allow);err!=nil {t.Fatal(err)}
 entered:=make(chan struct{})
 release:=make(chan struct{})
 var once sync.Once
 delayed:=func(_ context.Context,_ CodexPairInventory) error {
  once.Do(func(){close(entered)})
  <-release
  return nil
 }
 finished:=make(chan error,1)
 go func(){_,err:=manageCodexPairWithProbe(ctx,"upgrade","node",second,acp,delayed);finished<-err}()
 <-entered
 if _,err:=manageCodexPairWithProbe(ctx,"upgrade","node",third,acp,allow);err==nil {t.Fatal("simultaneous upgrade unexpectedly accepted")}
 prior,err:=ReadCodexPairInventory("node")
 if err!=nil || prior.Codex.Path!=first {t.Fatalf("active inventory changed while first upgrade probed: %+v %v",prior,err)}
 close(release)
 if err:=<-finished;err!=nil {t.Fatal(err)}
 current,err:=ReadCodexPairInventory("node")
 if err!=nil || current.Codex.Path!=second {t.Fatalf("first upgrade did not activate: %+v %v",current,err)}
 loc,_:=CodexPairInventoryPath("node")
 data,err:=os.ReadFile(filepath.Join(filepath.Dir(loc),"codex-pair.previous.json"))
 if err!=nil {t.Fatal(err)}
 var rollback CodexPairInventory
 if err:=json.Unmarshal(data,&rollback);err!=nil {t.Fatal(err)}
 if rollback.Codex.Path!=first {t.Fatalf("rollback target overwritten by rejected concurrent upgrade: %s",rollback.Codex.Path)}
}

func TestCodexNativePairReadyAcceptsSilentAuthorizedLoginStatus(t *testing.T) {
 dir:=t.TempDir()
 cli:=filepath.Join(dir,"codex")
 if err:=os.WriteFile(cli,[]byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo codex-cli 0.163.0; exit 0; fi\nif [ \"$1\" = \"login\" ]; then exit 0; fi\nexit 2\n"),0700);err!=nil {t.Fatal(err)}
 acp:=filepath.Join(dir,"codex-acp")
 script:=`#!/usr/bin/env python3
import json,sys
if '--version' in sys.argv:
 print('codex-acp 0.16.0')
 sys.exit(0)
for line in sys.stdin:
 req=json.loads(line)
 if req.get('method')=='initialize':
  response={'protocolVersion':1,'agentInfo':{'version':'0.16.0'},'agentCapabilities':{'loadSession':True},'_meta':{'steering':{'supported':True}}}
 elif req.get('method')=='session/new':
  response={'sessionId':'fixture','models':{'availableModels':[{'modelId':'gpt-fixture'}]}}
 else:
  response={}
 print(json.dumps({'jsonrpc':'2.0','id':req.get('id'),'result':response}),flush=True)
`
 if err:=os.WriteFile(acp,[]byte(script),0700);err!=nil {t.Fatal(err)}
 pair:=CodexPairInventory{Codex:CodexBinaryRecord{Path:cli},ACP:CodexBinaryRecord{Path:acp}}
 if err:=codexNativePairReady(context.Background(),pair);err!=nil {t.Fatalf("authorized Codex with a silent login status was refused: %v",err)}
 negative:=filepath.Join(dir,"codex-negative")
 if err:=os.WriteFile(negative,[]byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo codex-cli 0.163.0; exit 0; fi\nif [ \"$1\" = \"login\" ]; then echo 'Not logged in'; exit 0; fi\nexit 2\n"),0700);err!=nil {t.Fatal(err)}
 pair.Codex.Path=negative
 if err:=codexNativePairReady(context.Background(),pair);err==nil {t.Fatal("negative login status with a zero exit code was accepted")}
}
