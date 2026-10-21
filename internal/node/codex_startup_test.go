//go:build !windows

package node

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func startupCodexPairFixture(t *testing.T, scope string, authenticated bool) CodexPairInventory {
 t.Helper()
 t.Setenv("HOME", t.TempDir())
 root := t.TempDir()
 binary := func(name, script string) CodexBinaryRecord {
  t.Helper()
  path := filepath.Join(root, name)
  bytes := []byte("#!/bin/sh\n" + script)
  if err := os.WriteFile(path, bytes, 0700); err != nil { t.Fatal(err) }
  digest := sha256.Sum256(bytes)
  return CodexBinaryRecord{Path: path, ResolvedPath: path, Version: name+" 1.2.3", SHA256: hex.EncodeToString(digest[:]), Provenance: "external"}
 }
 cli := "if [ \"$1\" = \"--version\" ]; then echo 'codex 1.2.3'; exit 0; fi\n"
 if authenticated {
  cli += "if [ \"$1\" = \"login\" ]; then echo 'Logged in using ChatGPT'; exit 0; fi\n"
 } else {
  cli += "if [ \"$1\" = \"login\" ]; then echo 'Not logged in'; exit 1; fi\n"
 }
 cli += "exit 2\n"
 pair := CodexPairInventory{Schema: 1, Scope: scope, Harness: "codex", Codex: binary("codex", cli), ACP: binary("codex-acp", "echo 'codex-acp 1.2.3'\n")}
 pair.Compatibility.Validated = true
 location, err := CodexPairInventoryPath(scope)
 if err != nil { t.Fatal(err) }
 if err := os.MkdirAll(filepath.Dir(location), 0700); err != nil { t.Fatal(err) }
 encoded, err := json.Marshal(pair)
 if err != nil { t.Fatal(err) }
 if err := os.WriteFile(location, encoded, 0600); err != nil { t.Fatal(err) }
 return pair
}

func TestCodexStartupMissingInventoryDoesNotReachRuntime(t *testing.T) {
 t.Setenv("HOME", t.TempDir())
 invoked := false
 err := checkCodexStartupWithProbe(context.Background(), "server", func(context.Context, CodexPairInventory) error {
  invoked = true
  return nil
 })
 if err == nil || !strings.Contains(err.Error(), "inventory") || !strings.Contains(err.Error(), "install") || !strings.Contains(err.Error(), "secretary doctor") || invoked {
  t.Fatalf("missing inventory diagnostics=%v, probe invoked=%v", err, invoked)
 }
 if strings.Contains(err.Error(), "__secretary_codex") { t.Fatalf("sentinel leaked: %v", err) }
}

func TestCodexStartupExistingInventoryRequiresSelectedCLIAuthentication(t *testing.T) {
 pair := startupCodexPairFixture(t, "server", false)
 err := CheckCodexStartup(context.Background(), "server")
 if err == nil || !strings.Contains(err.Error(), pair.Codex.Path+" login") || !strings.Contains(err.Error(), "secretary doctor") {
  t.Fatalf("wrong selected Codex login guidance: %v", err)
 }
 if strings.Contains(err.Error(), "ACP unavailable") || strings.Contains(err.Error(), "__secretary_codex") {
  t.Fatalf("auth failure was misclassified: %v", err)
 }
}

func TestCodexStartupRejectsMissingOrModifiedACP(t *testing.T) {
 pair := startupCodexPairFixture(t, "server", true)
 if err := os.Remove(pair.ACP.Path); err != nil { t.Fatal(err) }
 err := CheckCodexStartup(context.Background(), "server")
 if err == nil || !strings.Contains(err.Error(), "ACP") || strings.Contains(err.Error(), " login") {
  t.Fatalf("missing ACP misclassified: %v", err)
 }
}

func TestCodexStartupSeparatesProtocolFailureFromLogin(t *testing.T) {
 startupCodexPairFixture(t, "server", true)
 err := checkCodexStartupWithProbe(context.Background(), "server", func(context.Context, CodexPairInventory) error {
  return errors.New("Codex native ACP initialize/session-new failure: protocol version mismatch")
 })
 if err == nil || !strings.Contains(err.Error(), "ACP") || !strings.Contains(err.Error(), "protocol") || strings.Contains(err.Error(), " login") {
  t.Fatalf("ACP protocol misclassified: %v", err)
 }
}

func TestCodexStartupValidatedWorkingPair(t *testing.T) {
 pair := startupCodexPairFixture(t, "server", true)
 called := false
 err := checkCodexStartupWithProbe(context.Background(), "server", func(_ context.Context, selected CodexPairInventory) error {
  called = true
  if selected.Codex.Path != pair.Codex.Path || selected.ACP.Path != pair.ACP.Path { t.Fatalf("wrong selected pair: %+v", selected) }
  return nil
 })
 if err != nil || !called { t.Fatalf("validated inventory rejected: %v (called=%v)", err, called) }
}
