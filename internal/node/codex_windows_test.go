//go:build windows

package node

import (
 "archive/zip"
 "bytes"
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
