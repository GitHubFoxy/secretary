package node

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
)

func TestManagedCodexInventoryHostPortable(t *testing.T) {
 home := t.TempDir()
 t.Setenv("HOME", home)
 if runtime.GOOS == "windows" { t.Setenv("USERPROFILE", home) }
 dir := filepath.Join(home,"managed")
 if err := os.MkdirAll(dir,0700); err != nil { t.Fatal(err) }
 ext := ""
 if runtime.GOOS == "windows" { ext = ".exe" }
 makeRecord := func(name string) CodexBinaryRecord {
  path := filepath.Join(dir,name+ext)
  data := []byte("fixture-"+name)
  if err := os.WriteFile(path,data,0700); err != nil { t.Fatal(err) }
  digest := sha256.Sum256(data)
  return CodexBinaryRecord{Path:path,ResolvedPath:path,Version:name+" fixture",SHA256:hex.EncodeToString(digest[:]),Provenance:"managed"}
 }
 pair := CodexPairInventory{Schema:1,Scope:"server",Harness:"codex",Codex:makeRecord("codex"),ACP:makeRecord("codex-acp")}
 pair.Compatibility.Validated = true
 location,err := CodexPairInventoryPath("server")
 if err != nil { t.Fatal(err) }
 if err := os.MkdirAll(filepath.Dir(location),0700); err != nil { t.Fatal(err) }
 content,err := json.Marshal(pair)
 if err != nil { t.Fatal(err) }
 if err := os.WriteFile(location,content,0600); err != nil { t.Fatal(err) }
 verify := func(record CodexBinaryRecord) error {
  data,err := os.ReadFile(record.ResolvedPath)
  if err != nil { return err }
  digest := sha256.Sum256(data)
  if hex.EncodeToString(digest[:]) != record.SHA256 { return fmt.Errorf("fixture binary changed") }
  return nil
 }
 read := func() error {
  _,err := readCodexPairInventoryWithVerifiers("server",verify,verify)
  return err
 }
 if err := read(); err == nil || !strings.Contains(err.Error(),"code-mode host") { t.Fatalf("missing host accepted: %v",err) }
 hostName := "codex-code-mode-host"+ext
 host := filepath.Join(dir,hostName)
 validHeader := []byte{0x7f,'E','L','F',2,1,1,0}
 if runtime.GOOS == "windows" { validHeader = []byte{'M','Z',0x90,0,2,1,1,0} }
 if runtime.GOOS == "darwin" { validHeader = []byte{0xcf,0xfa,0xed,0xfe,2,1,1,0} }
 if err := os.WriteFile(host,validHeader,0700); err != nil { t.Fatal(err) }
 if err := read(); err == nil || !strings.Contains(err.Error(),"trusted archive") { t.Fatalf("unattested host accepted: %v",err) }
 corrupt := append([]byte(nil),validHeader...)
 corrupt[len(corrupt)-1] ^= 0xff
 if err := os.WriteFile(host,corrupt,0700); err != nil { t.Fatal(err) }
 if err := read(); err == nil || !strings.Contains(err.Error(),"trusted archive") { t.Fatalf("signature-preserving corrupt host accepted: %v",err) }
 if err := os.WriteFile(host,[]byte("invalid"),0700); err != nil { t.Fatal(err) }
 if err := read(); err == nil || !strings.Contains(err.Error(),"binary format") { t.Fatalf("invalid-format host accepted: %v",err) }
 if _,err := manageCodexPairWithProbe(context.Background(),"doctor","server","","",func(_ context.Context, _ CodexPairInventory) error {
  return verifyManagedCodeModeHost(dir)
 }); err == nil || !strings.Contains(err.Error(),"binary format") { t.Fatalf("doctor accepted malformed host: %v",err) }
 if err := os.Remove(host); err != nil { t.Fatal(err) }
 if _,err := manageCodexPairWithProbe(context.Background(),"doctor","server","","",func(_ context.Context, _ CodexPairInventory) error {
  return verifyManagedCodeModeHost(dir)
 }); err == nil || !strings.Contains(err.Error(),"code-mode host") { t.Fatalf("doctor accepted missing host: %v",err) }
}
