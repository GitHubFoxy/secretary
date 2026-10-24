package node

import (
 "archive/tar"
 "archive/zip"
 "bytes"
 "compress/gzip"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
)

func TestManagedCodexInventoryAndDoctorRealExecutables(t *testing.T) {
 home := t.TempDir()
 t.Setenv("HOME",home)
 if runtime.GOOS == "windows" { t.Setenv("USERPROFILE",home) }
 dir := filepath.Join(home,"managed")
 if err := os.MkdirAll(dir,0700); err != nil { t.Fatal(err) }
 source := filepath.Join(home,"fixture.go")
 program := `package main
import ("bufio";"encoding/json";"fmt";"os")
func main() {
 args:=os.Args[1:]
 if len(args)>0 {
  switch args[0] {
  case "--version","--help": fmt.Println("codex-fixture 1.0"); return
  case "login": fmt.Println("Logged in using ChatGPT"); return
  }
 }
 scanner:=bufio.NewScanner(os.Stdin)
 for scanner.Scan() {
  var req struct { ID json.RawMessage; Method string }
  if json.Unmarshal(scanner.Bytes(),&req)!=nil || len(req.ID)==0 { continue }
  var result any
  switch req.Method {
  case "initialize":
   result=map[string]any{"protocolVersion":1,"agentInfo":map[string]any{"version":"fixture"},"agentCapabilities":map[string]any{"loadSession":true},"_meta":map[string]any{"steering":map[string]any{"supported":true}}}
  case "session/new":
   result=map[string]any{"sessionId":"fixture-session","models":map[string]any{"availableModels":[]any{map[string]any{"modelId":"fixture-model"}}}}
  case "session/load":
   result=map[string]any{"models":map[string]any{}}
  default:
   result=map[string]any{}
  }
  response:=map[string]any{"jsonrpc":"2.0","id":req.ID,"result":result}
  encoded,_:=json.Marshal(response)
  fmt.Println(string(encoded))
 }
}
`
 if err := os.WriteFile(source,[]byte(program),0600); err != nil { t.Fatal(err) }
 suffix:=""
 if runtime.GOOS=="windows" { suffix=".exe" }
 cli:=filepath.Join(dir,"codex"+suffix)
 compiler:=exec.Command("go","build","-o",cli,source)
 if output,err:=compiler.CombinedOutput();err!=nil { t.Fatalf("compile portable executable fixture: %v: %s",err,output) }
 acp:=filepath.Join(dir,"codex-acp"+suffix)
 binary,err:=os.ReadFile(cli)
 if err!=nil { t.Fatal(err) }
 if err:=os.WriteFile(acp,binary,0700);err!=nil { t.Fatal(err) }
 makeRecord:=func(path string) CodexBinaryRecord {
  hash,err:=codexFileSHA256(path)
  if err!=nil { t.Fatal(err) }
  resolved,err:=filepath.EvalSymlinks(path)
  if err!=nil { t.Fatal(err) }
  return CodexBinaryRecord{Path:path,ResolvedPath:resolved,Version:"codex-fixture 1.0",SHA256:hash,Provenance:"managed"}
 }
 pair:=CodexPairInventory{Schema:1,Scope:"server",Harness:"codex",Codex:makeRecord(cli),ACP:makeRecord(acp)}
 pair.Compatibility.Validated=true
 location,err:=CodexPairInventoryPath("server")
 if err!=nil { t.Fatal(err) }
 if err:=os.MkdirAll(filepath.Dir(location),0700);err!=nil { t.Fatal(err) }
 serialized,err:=json.Marshal(pair)
 if err!=nil { t.Fatal(err) }
 if err:=os.WriteFile(location,serialized,0600);err!=nil { t.Fatal(err) }
 hostName:="codex-code-mode-host"+suffix
 hostPath:=filepath.Join(dir,hostName)
 hostBytes:=[]byte{0x7f,'E','L','F',2,1,1,0,11,12}
 if runtime.GOOS=="windows" { hostBytes=[]byte{'M','Z',0x90,0,2,1,1,0,11,12} }
 if runtime.GOOS=="darwin" { hostBytes=[]byte{0xcf,0xfa,0xed,0xfe,2,1,1,0,11,12} }
 archiveURL:="file:///fixture.tar.gz"
 var archive bytes.Buffer
 if runtime.GOOS=="windows" {
  archiveURL="file:///fixture.zip"
  writer:=zip.NewWriter(&archive)
  member,err:=writer.Create("codex-code-mode-host-fixture.exe")
  if err!=nil { t.Fatal(err) }
  if _,err:=member.Write(hostBytes);err!=nil { t.Fatal(err) }
  if err:=writer.Close();err!=nil { t.Fatal(err) }
 } else {
  zipped:=gzip.NewWriter(&archive)
  writer:=tar.NewWriter(zipped)
  if err:=writer.WriteHeader(&tar.Header{Name:hostName+"-fixture",Mode:0700,Size:int64(len(hostBytes))});err!=nil { t.Fatal(err) }
  if _,err:=writer.Write(hostBytes);err!=nil { t.Fatal(err) }
  if err:=writer.Close();err!=nil { t.Fatal(err) }
  if err:=zipped.Close();err!=nil { t.Fatal(err) }
 }
 data:=archive.Bytes()
 hash:=sha256.Sum256(data)
 asset:=codexAsset{URL:archiveURL,SHA256:hex.EncodeToString(hash[:]),Name:hostName}
 original:=codexInventoryTrustedAssets
 codexInventoryTrustedAssets=func() ([]codexAsset,error) { return []codexAsset{asset},nil }
 defer func(){codexInventoryTrustedAssets=original}()
 if err:=os.WriteFile(filepath.Join(dir,hostName+codexArchiveExtension(asset)),data,0600);err!=nil { t.Fatal(err) }
 if err:=os.WriteFile(hostPath,hostBytes,0700);err!=nil { t.Fatal(err) }
 if err:=os.Remove(hostPath);err!=nil { t.Fatal(err) }
 if _,err:=ReadCodexPairInventory("server");err==nil || !strings.Contains(err.Error(),"code-mode host") { t.Fatalf("missing host accepted: %v",err) }
 if _,err:=ManageCodexPair(context.Background(),"doctor","server","","");err==nil || !strings.Contains(err.Error(),"code-mode host") { t.Fatalf("doctor accepted missing host: %v",err) }
 if err:=os.WriteFile(hostPath,hostBytes,0700);err!=nil { t.Fatal(err) }
 if _,err:=ReadCodexPairInventory("server");err!=nil { t.Fatalf("valid pinned host rejected: %v",err) }
 if _,err:=ManageCodexPair(context.Background(),"doctor","server","","");err!=nil { t.Fatalf("doctor rejected valid pinned host: %v",err) }
 modified:=append([]byte(nil),hostBytes...)
 modified[len(modified)-1]^=0xff
 if err:=os.WriteFile(hostPath,modified,0700);err!=nil { t.Fatal(err) }
 if _,err:=ReadCodexPairInventory("server");err==nil || !strings.Contains(err.Error(),"bytes differ from pinned upstream archive") { t.Fatalf("tampered host accepted by inventory: %v",err) }
 if _,err:=ManageCodexPair(context.Background(),"doctor","server","","");err==nil || !strings.Contains(err.Error(),"bytes differ from pinned upstream archive") { t.Fatalf("tampered host accepted by doctor: %v",err) }
}
