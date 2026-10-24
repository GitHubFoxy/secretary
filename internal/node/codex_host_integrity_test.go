package node

import (
 "archive/zip"
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestManagedCodeModeHostArchiveRejectsValidHeaderTampering(t *testing.T) {
 dir := t.TempDir()
 hostName := "codex-code-mode-host.exe"
 trusted := []byte{'M','Z',0x90,0,1,2,3,4,5,6,7,8}
 var buffer bytes.Buffer
 writer := zip.NewWriter(&buffer)
 member, err := writer.Create("codex-code-mode-host-test.exe")
 if err != nil { t.Fatal(err) }
 if _, err := member.Write(trusted); err != nil { t.Fatal(err) }
 if err := writer.Close(); err != nil { t.Fatal(err) }
 archive := buffer.Bytes()
 hash := sha256.Sum256(archive)
 asset := codexAsset{URL:"file:///trusted.zip",SHA256:hex.EncodeToString(hash[:]),Name:hostName}
 if err := os.WriteFile(filepath.Join(dir,hostName+".zip"),archive,0600); err != nil { t.Fatal(err) }
 if err := os.WriteFile(filepath.Join(dir,hostName),trusted,0700); err != nil { t.Fatal(err) }
 if err := verifyManagedCodexArchives([]codexAsset{asset},dir); err != nil { t.Fatalf("trusted host rejected: %v",err) }
 changed := append([]byte(nil),trusted...)
 changed[len(changed)-1] ^= 0xff
 if err := os.WriteFile(filepath.Join(dir,hostName),changed,0700); err != nil { t.Fatal(err) }
 if !bytes.Equal(changed[:4],trusted[:4]) { t.Fatal("fixture changed binary signature") }
 if err := verifyManagedCodexArchives([]codexAsset{asset},dir); err == nil || !strings.Contains(err.Error(),"bytes differ from pinned upstream archive") {
  t.Fatalf("tampered host with intact signature accepted: %v",err)
 }
 if err := os.WriteFile(filepath.Join(dir,hostName),trusted,0700); err != nil { t.Fatal(err) }
 archive[8] ^= 0xff
 if err := os.WriteFile(filepath.Join(dir,hostName+".zip"),archive,0600); err != nil { t.Fatal(err) }
 if err := verifyManagedCodexArchives([]codexAsset{asset},dir); err == nil || !strings.Contains(err.Error(),"trusted archive digest mismatch") {
  t.Fatalf("tampered archive accepted: %v",err)
 }
}
