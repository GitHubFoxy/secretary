package node

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "time"
)

type CodexBinaryRecord struct {
 Path string `json:"path"`
 ResolvedPath string `json:"resolved_path"`
 Version string `json:"version"`
 SHA256 string `json:"sha256"`
 Provenance string `json:"provenance"`
}

type CodexPairInventory struct {
 Compatibility struct { Validated bool `json:"validated"` } `json:"compatibility"`
 Schema int `json:"schema"`
 Scope string `json:"scope"`
 Harness string `json:"harness"`
 Codex CodexBinaryRecord `json:"codex"`
 ACP CodexBinaryRecord `json:"acp"`
}

func CodexPairInventoryPath(scope string) (string, error) {
 if scope != "server" && scope != "node" { return "", fmt.Errorf("invalid native pair scope %q", scope) }
 home, err := os.UserHomeDir()
 if err != nil { return "", err }
 return filepath.Join(home, ".local", "share", "secretary", "native", scope, "codex-pair.json"), nil
}

func ReadCodexPairInventory(scope string) (CodexPairInventory, error) {
 path, err := CodexPairInventoryPath(scope)
 if err != nil { return CodexPairInventory{}, err }
 info, err := os.Lstat(path)
 if err != nil { return CodexPairInventory{}, err }
 if !info.Mode().IsRegular() { return CodexPairInventory{}, fmt.Errorf("unsafe native pair inventory: %s", path) }
 data, err := os.ReadFile(path)
 if err != nil { return CodexPairInventory{}, err }
 var pair CodexPairInventory
 if err := json.Unmarshal(data, &pair); err != nil { return pair, fmt.Errorf("invalid native pair inventory: %w", err) }
 if pair.Schema != 1 || pair.Scope != scope || pair.Harness != "codex" { return pair, fmt.Errorf("native pair inventory mismatch: %s", path) }
 if !pair.Compatibility.Validated { return pair, fmt.Errorf("native Codex pair protocol has not been validated for %s", scope) }
 if err := verifyCodexBinary(pair.Codex); err != nil { return pair, fmt.Errorf("Codex CLI: %w", err) }
 if err := verifyCodexBinary(pair.ACP); err != nil { return pair, fmt.Errorf("Codex ACP: %w", err) }
 return pair, nil
}

func verifyCodexBinary(record CodexBinaryRecord) error {
 if !filepath.IsAbs(record.Path) || !filepath.IsAbs(record.ResolvedPath) || record.Version == "" ||
   (record.Provenance != "managed" && record.Provenance != "external") {
  return fmt.Errorf("invalid executable record")
 }
 resolved, err := filepath.EvalSymlinks(record.Path)
 if err != nil { return fmt.Errorf("missing selected binary %s: %w", record.Path, err) }
 if resolved != record.ResolvedPath { return fmt.Errorf("selected executable moved: %s", record.Path) }
 info, err := os.Stat(resolved)
 if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 { return fmt.Errorf("selected executable unavailable: %s", resolved) }
 bytes, err := os.ReadFile(resolved)
 if err != nil { return err }
 digest := sha256.Sum256(bytes)
 if hex.EncodeToString(digest[:]) != record.SHA256 { return fmt.Errorf("selected executable bytes changed: %s", resolved) }
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 output, err := exec.CommandContext(ctx, resolved, "--version").CombinedOutput()
 if err != nil { return fmt.Errorf("selected executable version probe failed: %w", err) }
 if strings.TrimSpace(string(output)) != record.Version { return fmt.Errorf("selected executable version changed: %s", resolved) }
 return nil
}

func ApplyCodexPairInventory(scope string) error {
 pair, err := ReadCodexPairInventory(scope)
 if err != nil { return err }
 if err := os.Setenv("CODEX_PATH", pair.Codex.Path); err != nil { return err }
 if err := os.Setenv("SECRETARY_CODEX_COMMAND", pair.Codex.Path); err != nil { return err }
 return os.Setenv("SECRETARY_ACP_COMMAND", pair.ACP.Path)
}

func DisableUnselectedCodexRuntime() {
 _ = os.Setenv("CODEX_PATH", "/__secretary_codex_inventory_unavailable__")
 _ = os.Setenv("SECRETARY_CODEX_COMMAND", "/__secretary_codex_inventory_unavailable__")
 _ = os.Setenv("SECRETARY_ACP_COMMAND", "/__secretary_codex_acp_inventory_unavailable__")
}
