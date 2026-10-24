//go:build !windows

package node

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

func TestCodexPairInventoryRejectsMovedAndChangedExecutables(t *testing.T) {
 home := t.TempDir()
 t.Setenv("HOME", home)
 dir := filepath.Join(home, "bin")
 if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
 makeBinary := func(name string) CodexBinaryRecord {
  path := filepath.Join(dir, name)
  data := []byte("#!/bin/sh\necho " + name + " 1.2.3\n")
  if err := os.WriteFile(path, data, 0700); err != nil { t.Fatal(err) }
  hash := sha256.Sum256(data)
  resolved, err := filepath.EvalSymlinks(path)
  if err != nil { t.Fatal(err) }
  return CodexBinaryRecord{Path:path, ResolvedPath:resolved, Version:name+" 1.2.3", SHA256:hex.EncodeToString(hash[:]), Provenance:"external"}
 }
 pair := CodexPairInventory{Schema:1,Scope:"node",Harness:"codex",Codex:makeBinary("codex"),ACP:makeBinary("codex-acp")}
 pair.Compatibility.Validated = true
 location, err := CodexPairInventoryPath("node")
 if err != nil { t.Fatal(err) }
 if err := os.MkdirAll(filepath.Dir(location), 0700); err != nil { t.Fatal(err) }
 save := func() {
  bytes, err := json.Marshal(pair)
  if err != nil { t.Fatal(err) }
  if err := os.WriteFile(location, bytes, 0600); err != nil { t.Fatal(err) }
 }
 save()
 if _, err := ReadCodexPairInventory("node"); err != nil { t.Fatal(err) }
 if err := os.Rename(pair.Codex.Path, pair.Codex.Path+".moved"); err != nil { t.Fatal(err) }
 if _, err := ReadCodexPairInventory("node"); err == nil { t.Fatal("moved binary accepted") }
 if err := os.Rename(pair.Codex.Path+".moved", pair.Codex.Path); err != nil { t.Fatal(err) }
 if err := os.WriteFile(pair.ACP.Path, []byte("#!/bin/sh\necho changed\n"), 0700); err != nil { t.Fatal(err) }
 if _, err := ReadCodexPairInventory("node"); err == nil || !strings.Contains(err.Error(), "bytes changed") { t.Fatalf("expected checksum failure, got %v", err) }
}

func TestCodexPairScopeIsolation(t *testing.T) {
 t.Setenv("HOME", t.TempDir())
 if _, err := ReadCodexPairInventory("server"); !os.IsNotExist(err) { t.Fatalf("expected absent Server inventory: %v", err) }
 if _, err := CodexPairInventoryPath("../escape"); err == nil { t.Fatal("invalid scope accepted") }
}

func TestManagedCodexInventoryRequiresCodeModeHost(t *testing.T) {
 home := t.TempDir()
 t.Setenv("HOME", home)
 dir := filepath.Join(home, "managed")
 if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
 makeBinary := func(name string) CodexBinaryRecord {
  path := filepath.Join(dir, name)
  data := []byte("#!/bin/sh\necho " + name + " 1.2.3\n")
  if err := os.WriteFile(path, data, 0700); err != nil { t.Fatal(err) }
  hash := sha256.Sum256(data)
  return CodexBinaryRecord{Path:path, ResolvedPath:path, Version:name+" 1.2.3", SHA256:hex.EncodeToString(hash[:]), Provenance:"managed"}
 }
 pair := CodexPairInventory{Schema:1, Scope:"server", Harness:"codex", Codex:makeBinary("codex"), ACP:makeBinary("codex-acp")}
 pair.Compatibility.Validated = true
 location, err := CodexPairInventoryPath("server")
 if err != nil { t.Fatal(err) }
 if err := os.MkdirAll(filepath.Dir(location), 0700); err != nil { t.Fatal(err) }
 data, err := json.Marshal(pair)
 if err != nil { t.Fatal(err) }
 if err := os.WriteFile(location, data, 0600); err != nil { t.Fatal(err) }
 if _, err := ReadCodexPairInventory("server"); err == nil || !strings.Contains(err.Error(), "code-mode host") {
  t.Fatalf("missing managed code-mode host accepted: %v", err)
 }
 host := filepath.Join(dir, "codex-code-mode-host")
 if err := os.WriteFile(host, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}, 0700); err != nil { t.Fatal(err) }
 if _, err := ReadCodexPairInventory("server"); err != nil { t.Fatalf("complete managed pair refused: %v", err) }
 summary, err := manageCodexPairWithProbe(context.Background(), "doctor", "server", "", "", func(context.Context, CodexPairInventory) error { return nil })
 if err != nil || !strings.Contains(summary, "code-mode host "+host+": ready") { t.Fatalf("doctor did not report managed code-mode host: %q %v", summary, err) }
 if err := os.WriteFile(host, []byte("bad host"), 0700); err != nil { t.Fatal(err) }
 if _, err := ReadCodexPairInventory("server"); err == nil || !strings.Contains(err.Error(), "binary format") {
  t.Fatalf("invalid managed code-mode host accepted: %v", err)
 }
}
