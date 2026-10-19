package node

import (
 "archive/tar"
 "compress/gzip"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "time"

 "github.com/beruseruko/secretary/internal/core"
)

const codexPinnedVersion = "0.163.0"
const codexACPPinnedVersion = "0.16.0"

type codexAsset struct {
 URL string
 SHA256 string
 Name string
}

func codexNativeAssets() ([]codexAsset, error) {
 triple, acpTriple := "", ""
 switch runtime.GOOS + "/" + runtime.GOARCH {
 case "darwin/arm64": triple, acpTriple = "aarch64-apple-darwin", "aarch64-apple-darwin"
 case "darwin/amd64": triple, acpTriple = "x86_64-apple-darwin", "x86_64-apple-darwin"
 case "linux/arm64": triple, acpTriple = "aarch64-unknown-linux-musl", "aarch64-unknown-linux-gnu"
 case "linux/amd64": triple, acpTriple = "x86_64-unknown-linux-musl", "x86_64-unknown-linux-gnu"
 default: return nil, fmt.Errorf("unsupported native Codex host: %s/%s", runtime.GOOS, runtime.GOARCH)
 }
 codexDigest, acpDigest := "", ""
 switch runtime.GOOS + "/" + runtime.GOARCH {
 case "darwin/arm64": codexDigest, acpDigest = "61c4d9a92521bdb42b0c1d9f87c673f6da628a06de5945cbdd9a0010632f6d10", "377f1137945b54d8e689cb600767f6dcfe6e548e01c1f29018862c009c692c8d"
 case "darwin/amd64": codexDigest, acpDigest = "562a1aea0e5686283a467687d268ae33705f02fd4d40f1bc4110a415c5c1c7b1", "212fafcf971c75cd4f5060be2d5951bc322f71c49028dbd2531931494c33c784"
 case "linux/arm64": codexDigest, acpDigest = "a7261e65f4a96d8f4ca65a069c6cf2dc2c33defc341e48078acefda9461fffe4", "cb72820f53c91a63982a804bd6ab251bde51bc9371d8e638946f02d7d78580b5"
 case "linux/amd64": codexDigest, acpDigest = "beb339e8ecd9ba057a6bb2dc81ad8ca474812edfbf0fc5056ca818797648dde4", "0a9ad6c31ec9b2b87dccb7e9da3faf5d387e74470d24dbced75a160ed7b22d06"
 }
 return []codexAsset{
  {"https://github.com/openai/codex/releases/download/rust-v"+codexPinnedVersion+"/codex-"+triple+".tar.gz", codexDigest, "codex"},
  {"https://github.com/zed-industries/codex-acp/releases/download/v"+codexACPPinnedVersion+"/codex-acp-"+codexACPPinnedVersion+"-"+acpTriple+".tar.gz", acpDigest, "codex-acp"},
 }, nil
}

func fetchCodexExecutable(ctx context.Context, asset codexAsset, stage string) error {
 req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
 if err != nil { return err }
 client := &http.Client{Timeout: 2*time.Minute}
 resp, err := client.Do(req)
 if err != nil { return err }
 defer resp.Body.Close()
 if resp.StatusCode != http.StatusOK { return fmt.Errorf("native release download HTTP %d", resp.StatusCode) }
 compressed, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
 if err != nil { return err }
 digest := sha256.Sum256(compressed)
 if hex.EncodeToString(digest[:]) != asset.SHA256 { return fmt.Errorf("%s release checksum mismatch", asset.Name) }
 archive, err := gzip.NewReader(strings.NewReader(string(compressed)))
 if err != nil { return err }
 defer archive.Close()
 tarReader := tar.NewReader(archive)
 count := 0
 for {
  header, err := tarReader.Next()
  if errors.Is(err, io.EOF) { break }
  if err != nil { return err }
  name := filepath.Base(header.Name)
  if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA { continue }
  if name != asset.Name && !strings.HasPrefix(name, asset.Name+"-") { continue }
  count++
  if count != 1 { return fmt.Errorf("duplicate %s in native archive", asset.Name) }
  output, err := os.OpenFile(filepath.Join(stage, asset.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
  if err != nil { return err }
  _, copyErr := io.Copy(output, io.LimitReader(tarReader, 128<<20))
  closeErr := output.Close()
  if copyErr != nil { return copyErr }
  if closeErr != nil { return closeErr }
 }
 if count != 1 { return fmt.Errorf("native archive missing %s", asset.Name) }
 return nil
}

func codexBinaryRecord(path, provenance string) (CodexBinaryRecord, error) {
 absolute, err := filepath.Abs(path)
 if err != nil { return CodexBinaryRecord{}, err }
 target, err := filepath.EvalSymlinks(absolute)
 if err != nil { return CodexBinaryRecord{}, err }
 info, err := os.Stat(target)
 if err != nil { return CodexBinaryRecord{}, err }
 if !info.Mode().IsRegular() || info.Mode()&0111==0 { return CodexBinaryRecord{}, fmt.Errorf("not executable: %s", target) }
 contents, err := os.ReadFile(target)
 if err != nil { return CodexBinaryRecord{}, err }
 sum := sha256.Sum256(contents)
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 version, err := exec.CommandContext(ctx, target, "--version").CombinedOutput()
 if err != nil { return CodexBinaryRecord{}, fmt.Errorf("version probe %s: %w", target, err) }
 if strings.TrimSpace(string(version)) == "" { return CodexBinaryRecord{}, fmt.Errorf("empty version: %s", target) }
 return CodexBinaryRecord{Path:absolute, ResolvedPath:target, Version:strings.TrimSpace(string(version)), SHA256:hex.EncodeToString(sum[:]), Provenance:provenance}, nil
}

func codexNativePairReady(ctx context.Context, pair CodexPairInventory) error {
 auth, err := exec.CommandContext(ctx, pair.Codex.Path, "login", "status").CombinedOutput()
 if err != nil { return fmt.Errorf("native Codex authorization required; run %s login (status: %s)", pair.Codex.Path, strings.TrimSpace(string(auth))) }
 before := os.Getenv("SECRETARY_ACP_COMMAND")
 defer os.Setenv("SECRETARY_ACP_COMMAND", before)
 if err := os.Setenv("SECRETARY_ACP_COMMAND", pair.ACP.Path); err != nil { return err }
 models, _, err := (ExecCommandRunner{}).ObserveCodexACP(ctx, pair.Codex.Path)
 if err != nil { return fmt.Errorf("native Codex/ACP protocol not ready: %w", err) }
 if len(models) == 0 { return errors.New("native Codex ACP returned no supported models") }
 return nil
}

func readCodexPairUnchecked(scope string) (CodexPairInventory, error) {
 path, err := CodexPairInventoryPath(scope)
 if err != nil { return CodexPairInventory{}, err }
 info, err := os.Lstat(path)
 if err != nil { return CodexPairInventory{}, err }
 if !info.Mode().IsRegular() { return CodexPairInventory{}, errors.New("unsafe native inventory") }
 data, err := os.ReadFile(path)
 if err != nil { return CodexPairInventory{}, err }
 var pair CodexPairInventory
 if err := json.Unmarshal(data, &pair); err != nil { return pair, err }
 if pair.Schema != 1 || pair.Scope != scope || pair.Harness != "codex" { return pair, errors.New("invalid native pair record") }
 if err := verifyCodexBinary(pair.Codex); err != nil { return pair, err }
 if err := verifyCodexBinary(pair.ACP); err != nil { return pair, err }
 return pair, nil
}

func saveCodexPair(path string, pair CodexPairInventory) error {
 if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { return err }
 if _, err := os.Lstat(path); err == nil {
  info, err := os.Lstat(path)
  if err != nil { return err }
  if !info.Mode().IsRegular() { return fmt.Errorf("unsafe native inventory: %s", path) }
 } else if !os.IsNotExist(err) { return err }
 content, err := json.MarshalIndent(pair, "", "  ")
 if err != nil { return err }
 file, err := os.CreateTemp(filepath.Dir(path), ".codex-inventory-")
 if err != nil { return err }
 defer os.Remove(file.Name())
 if err := file.Chmod(0600); err != nil { file.Close(); return err }
 if _, err := file.Write(content); err != nil { file.Close(); return err }
 if err := file.Sync(); err != nil { file.Close(); return err }
 if err := file.Close(); err != nil { return err }
 return os.Rename(file.Name(), path)
}

func provisionManagedPair(ctx context.Context) (string, string, error) {
 assets, err := codexNativeAssets()
 if err != nil { return "", "", err }
 home, err := os.UserHomeDir()
 if err != nil { return "", "", err }
 root := filepath.Join(home, ".local", "share", "secretary", "native", "releases", codexPinnedVersion+"-"+codexACPPinnedVersion)
 final := filepath.Join(root, runtime.GOOS+"-"+runtime.GOARCH)
 cli, acp := filepath.Join(final, "codex"), filepath.Join(final, "codex-acp")
 if _, err := os.Lstat(final); err == nil {
  if _, err := codexBinaryRecord(cli, "managed"); err != nil { return "", "", fmt.Errorf("partial or changed managed Codex CLI: %w", err) }
  if _, err := codexBinaryRecord(acp, "managed"); err != nil { return "", "", fmt.Errorf("partial or changed managed Codex ACP: %w", err) }
  return cli, acp, nil
 } else if !os.IsNotExist(err) { return "", "", err }
 if err := os.MkdirAll(root, 0700); err != nil { return "", "", err }
 stage, err := os.MkdirTemp(root, ".codex-stage-")
 if err != nil { return "", "", err }
 defer os.RemoveAll(stage)
 for _, asset := range assets {
  if err := fetchCodexExecutable(ctx, asset, stage); err != nil { return "", "", err }
 }
 if _, err := codexBinaryRecord(filepath.Join(stage, "codex"), "managed"); err != nil { return "", "", err }
 if _, err := codexBinaryRecord(filepath.Join(stage, "codex-acp"), "managed"); err != nil { return "", "", err }
 if err := os.Rename(stage, final); err != nil { return "", "", err }
 return cli, acp, nil
}

func ManageCodexPair(ctx context.Context, action, scope, cliPath, acpPath string) (string, error) {
 location, err := CodexPairInventoryPath(scope)
 if err != nil { return "", err }
 existing, existingErr := readCodexPairUnchecked(scope)
 switch action {
 case "env", "show", "doctor", "readiness":
  if existingErr != nil { return "", existingErr }
  if action == "env" { return "CODEX_PATH="+existing.Codex.Path+"\nSECRETARY_ACP_COMMAND="+existing.ACP.Path, nil }
  if action == "show" { bytes, _ := json.MarshalIndent(existing, "", "  "); return string(bytes), nil }
  if action == "readiness" {
   if err := codexNativePairReady(ctx, existing); err != nil { return "", err }
   existing.Compatibility.Validated = true
   if err := saveCodexPair(location, existing); err != nil { return "", err }
  }
  if action == "doctor" {
   if !existing.Compatibility.Validated { return "", errors.New("native Codex pair not protocol-validated") }
   if err := codexNativePairReady(ctx, existing); err != nil { return "", err }
  }
  return fmt.Sprintf("Codex %s and ACP %s: %s", existing.Codex.Version, existing.ACP.Version, action), nil
 case "install", "upgrade":
  if (cliPath=="") != (acpPath=="") { return "", errors.New("both external Codex and ACP paths must be selected together") }
  if action=="install" && existingErr==nil {
   if cliPath!="" {
    requested, err := filepath.Abs(cliPath)
    if err!=nil { return "",err }
    if requested!=existing.Codex.Path { return "", errors.New("pair already selected; explicit upgrade required") }
   }
   return "Existing native Codex pair preserved", nil
  }
  if action=="upgrade" && existingErr!=nil && !os.IsNotExist(existingErr) { return "", existingErr }
  provenance:="external"
  if cliPath=="" {
   cliPath, acpPath, err = provisionManagedPair(ctx)
   if err != nil { return "", err }
   provenance="managed"
  }
  cli, err := codexBinaryRecord(cliPath, provenance)
  if err != nil { return "", err }
  acp, err := codexBinaryRecord(acpPath, provenance)
  if err != nil { return "", err }
  pair := CodexPairInventory{Schema:1,Scope:scope,Harness:"codex",Codex:cli,ACP:acp}
  if action=="upgrade" {
   if err := codexNativePairReady(ctx,pair); err != nil { return "", err }
   pair.Compatibility.Validated=true
   if existingErr==nil {
    if err := saveCodexPair(filepath.Join(filepath.Dir(location),"codex-pair.previous.json"),existing); err != nil { return "", err }
   }
  }
  if err := saveCodexPair(location,pair); err != nil { return "", err }
  return fmt.Sprintf("Selected native Codex %s / ACP %s (%s)",cli.Version,acp.Version,provenance),nil
 case "rollback":
  previous := filepath.Join(filepath.Dir(location),"codex-pair.previous.json")
  info, err := os.Lstat(previous)
  if err != nil { return "",err }
  if !info.Mode().IsRegular() { return "",errors.New("unsafe previous native pair record") }
  data, err := os.ReadFile(previous)
  if err != nil { return "",err }
  var pair CodexPairInventory
  if err := json.Unmarshal(data,&pair); err!=nil { return "",err }
  if pair.Schema!=1 || pair.Scope!=scope || pair.Harness!="codex" { return "",errors.New("previous native pair mismatch") }
  if err := verifyCodexBinary(pair.Codex); err!=nil { return "",err }
  if err := verifyCodexBinary(pair.ACP); err!=nil { return "",err }
  if err := codexNativePairReady(ctx,pair); err!=nil { return "",err }
  pair.Compatibility.Validated=true
  if err := saveCodexPair(location,pair); err!=nil { return "",err }
  if err := os.Remove(previous); err!=nil { return "",err }
  return "Previous native Codex pair restored",nil
 default:
  return "", fmt.Errorf("unsupported native pair action %q", action)
 }
}
