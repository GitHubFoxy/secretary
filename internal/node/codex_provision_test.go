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
  version := codexPinnedVersion
  if name=="codex-acp" { version=codexACPPinnedVersion }
  payload := []byte("#!/bin/sh\necho " + name + " " + version + "\n")
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
 if err:=os.Remove(acp);err != nil { t.Fatal(err) }
 if _,_,err:=installManagedCodexAssets(context.Background(),assets,root,"fixture");err==nil { t.Fatal("partial managed installation accepted") }
 broken:=[]codexAsset{{URL:server.URL+"/codex",SHA256:strings.Repeat("0",64),Name:"codex"},assets[1]}
 if _,_,err:=installManagedCodexAssets(context.Background(),broken,root,"corrupt");err==nil { t.Fatal("bad archive accepted") }
 if _,err:=os.Stat(filepath.Join(root,"corrupt"));!os.IsNotExist(err) { t.Fatalf("damaged release activated: %v",err) }
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
  response={'sessionId':'fixture','models':{'availableModels':[{'modelId':'gpt-fixture'}]}}
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
 if !strings.Contains(string(wire),"initialize") || !strings.Contains(string(wire),"session/new") {t.Fatalf("missing required ACP protocol messages: %s",wire)}
 pair.ACP.Path=bad
 if err:=codexNativePairReady(context.Background(),pair);err==nil {t.Fatal("invalid ACP initialize contract accepted")}
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
