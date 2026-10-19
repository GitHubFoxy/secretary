package node

import (
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
  return CodexBinaryRecord{Path:path, ResolvedPath:path, Version:name+" 1.2.3", SHA256:hex.EncodeToString(hash[:]), Provenance:"external"}
 }
 pair := CodexPairInventory{Schema:1,Scope:"node",Harness:"codex",Codex:makeBinary("codex"),ACP:makeBinary("codex-acp")}
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
