//go:build windows

package node

import (
 "archive/zip"
 "bytes"
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestInstall07PinnedWindowsAssets(t *testing.T) {
 assets, err := codexNativeAssets()
 if err != nil { t.Fatal(err) }
 if len(assets) != 2 { t.Fatalf("assets = %d", len(assets)) }
 for _, asset := range assets {
  if !strings.HasSuffix(asset.URL, ".zip") || len(asset.SHA256) != 64 || !strings.HasSuffix(asset.Name, ".exe") {
   t.Fatalf("unverified Windows release asset: %+v", asset)
  }
 }
 if assets[0].Name != "codex.exe" || assets[1].Name != "codex-acp.exe" { t.Fatal(assets) }
}

func TestInstall07WindowsArchiveSafeExtraction(t *testing.T) {
 makeZip := func(names []string) []byte {
  var output bytes.Buffer
  writer := zip.NewWriter(&output)
  for _, name := range names {
   member, err := writer.Create(name)
   if err != nil { t.Fatal(err) }
   if _, err := member.Write([]byte("fixture executable")); err != nil { t.Fatal(err) }
  }
  if err := writer.Close(); err != nil { t.Fatal(err) }
  return output.Bytes()
 }
 asset := codexAsset{URL:"https://example.invalid/codex.zip", Name:"codex.exe"}
 stage := t.TempDir()
 if err := unpackCodexArchive(makeZip([]string{"codex-x86_64-pc-windows-msvc.exe"}), asset, stage); err != nil { t.Fatal(err) }
 contents, err := os.ReadFile(filepath.Join(stage, "codex.exe"))
 if err != nil || string(contents) != "fixture executable" { t.Fatalf("extracted unexpected bytes: %v", err) }
 for _, names := range [][]string{
  {"other.exe"},
  {"codex-x86_64-pc-windows-msvc.exe", "codex-aarch64-pc-windows-msvc.exe"},
 } {
  if err := unpackCodexArchive(makeZip(names), asset, t.TempDir()); err == nil {
   t.Fatalf("archive with entries %v unexpectedly accepted", names)
  }
 }
}

func TestInstall07WindowsInventoryAdoptTamperUpgradeAndRollback(t *testing.T) {
 source := os.Getenv("SECRETARY_INSTALL07_TEST_EXE")
 if source == "" { t.Skip("CI fixture executable is not configured") }
 t.Setenv("USERPROFILE", t.TempDir())
 root := t.TempDir()
 copyBinary := func(name string) string {
  t.Helper()
  sourceBytes, err := os.ReadFile(source)
  if err != nil { t.Fatal(err) }
  location := filepath.Join(root,name)
  if err := os.WriteFile(location,sourceBytes,0700); err != nil { t.Fatal(err) }
  return location
 }
 cli := copyBinary("codex.exe")
 acp := copyBinary("codex-acp.exe")
 newer := copyBinary("codex-v2.exe")
 ctx := context.Background()
 fakeProbe := func(_ context.Context, pair CodexPairInventory) error {
  if pair.Codex.Path == "" || pair.ACP.Path == "" { return fmt.Errorf("incomplete pair") }
  return nil
 }
 if _,err := manageCodexPairWithProbe(ctx,"install","node",cli,acp,fakeProbe); err != nil { t.Fatal(err) }
 if _,err := manageCodexPairWithProbe(ctx,"readiness","node","","",fakeProbe); err != nil { t.Fatal(err) }
 first,err := ReadCodexPairInventory("node")
 if err != nil || first.Codex.Path != cli || first.Codex.Provenance != "external" { t.Fatalf("adopted record: %+v %v",first,err) }
 if _,err := manageCodexPairWithProbe(ctx,"install","node",cli,acp,fakeProbe); err != nil { t.Fatal(err) }
 if _,err := manageCodexPairWithProbe(ctx,"install","node",newer,acp,fakeProbe); err == nil { t.Fatal("implicit binary switching accepted") }
 if _,err := manageCodexPairWithProbe(ctx,"upgrade","node",newer,acp,func(context.Context,CodexPairInventory) error { return errors.New("provider unavailable") }); err == nil { t.Fatal("failed readiness upgraded the selected pair") }
 selected,err := ReadCodexPairInventory("node")
 if err != nil || selected.Codex.Path != cli { t.Fatalf("failed upgrade mutated selection: %+v %v",selected,err) }
 if _,err := manageCodexPairWithProbe(ctx,"upgrade","node",newer,acp,fakeProbe); err != nil { t.Fatal(err) }
 if _,err := manageCodexPairWithProbe(ctx,"rollback","node","","",fakeProbe); err != nil { t.Fatal(err) }
 selected,err = ReadCodexPairInventory("node")
 if err != nil || selected.Codex.Path != cli { t.Fatalf("rollback did not restore pair: %+v %v",selected,err) }
 if err := os.WriteFile(cli,[]byte("tampered"),0700); err != nil { t.Fatal(err) }
 if _,err := ReadCodexPairInventory("node"); err == nil { t.Fatal("tampered selected executable accepted") }
 if _,err := manageCodexPairWithProbe(ctx,"install","server",newer,acp,fakeProbe); err != nil { t.Fatal(err) }
 if _,err := readCodexPairUnchecked("server"); err != nil { t.Fatalf("server scope unexpectedly shared with Node: %v",err) }
 for _,ext := range []string{".cmd",".ps1"} {
  file:=filepath.Join(root,"shim"+ext)
  if err:=os.WriteFile(file,[]byte("echo unsupported"),0600); err != nil { t.Fatal(err) }
  if _,err:=codexBinaryRecord(file,"external"); err==nil { t.Fatalf("%s launcher accepted",ext) }
 }
}
