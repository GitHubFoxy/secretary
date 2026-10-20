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

)

const codexPinnedVersion = "0.163.0"
const codexACPPinnedVersion = "2.2.2"
const codexACPWindowsLegacyVersion = "0.16.0"

// codex-acp 0.16.0 accepts --config and --help but not --version. For
// managed installations its version is attested by the pinned release archive;
// for external adapters, do not claim a version the executable cannot report.
const codexACPManagedVersionSuffix = " (pinned upstream archive; no --version)"
const codexACPManagedVersionless = "codex-acp " + codexACPPinnedVersion + codexACPManagedVersionSuffix
const codexACPExternalVersionless = "codex-acp (version unavailable; no --version)"

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
 case "windows/amd64": triple, acpTriple = "x86_64-pc-windows-msvc", "x86_64-pc-windows-msvc"
 case "windows/arm64": triple, acpTriple = "aarch64-pc-windows-msvc", "aarch64-pc-windows-msvc"
 default: return nil, fmt.Errorf("unsupported native Codex host: %s/%s", runtime.GOOS, runtime.GOARCH)
 }
 codexDigest, acpDigest := "", ""
 switch runtime.GOOS + "/" + runtime.GOARCH {
 case "darwin/arm64": codexDigest, acpDigest = "61c4d9a92521bdb42b0c1d9f87c673f6da628a06de5945cbdd9a0010632f6d10", "377f1137945b54d8e689cb600767f6dcfe6e548e01c1f29018862c009c692c8d"
 case "darwin/amd64": codexDigest, acpDigest = "562a1aea0e5686283a467687d268ae33705f02fd4d40f1bc4110a415c5c1c7b1", "212fafcf971c75cd4f5060be2d5951bc322f71c49028dbd2531931494c33c784"
 case "linux/arm64": codexDigest, acpDigest = "a7261e65f4a96d8f4ca65a069c6cf2dc2c33defc341e48078acefda9461fffe4", "cb72820f53c91a63982a804bd6ab251bde51bc9371d8e638946f02d7d78580b5"
 case "linux/amd64": codexDigest, acpDigest = "beb339e8ecd9ba057a6bb2dc81ad8ca474812edfbf0fc5056ca818797648dde4", "0a9ad6c31ec9b2b87dccb7e9da3faf5d387e74470d24dbced75a160ed7b22d06"
 case "windows/amd64": codexDigest, acpDigest = "dabdb4d16aaf10fea40b74811c96efddcd9138f122163b69ed604fcb3eb704c3", "8f290b2956c9276c1748199641b87005e2236b04a22c23050473a2c7fad59cd7"
 case "windows/arm64": codexDigest, acpDigest = "c12842937952bc1dd757c82ae3f31c83f21b794344cf361b506b0218d4881c74", "1640fac9ff4893a9aed895ad3e8d529a7b8cdad4f65ff85d3b5c69323857e7b4"
 }
 if runtime.GOOS == "windows" {
  return []codexAsset{
   {"https://github.com/openai/codex/releases/download/rust-v"+codexPinnedVersion+"/codex-"+triple+".exe.zip", codexDigest, "codex.exe"},
   {"https://github.com/zed-industries/codex-acp/releases/download/v"+codexACPWindowsLegacyVersion+"/codex-acp-"+codexACPWindowsLegacyVersion+"-"+acpTriple+".zip", acpDigest, "codex-acp.exe"},
  }, nil
 }
 return []codexAsset{
  {"https://github.com/openai/codex/releases/download/rust-v"+codexPinnedVersion+"/codex-"+triple+".tar.gz", codexDigest, "codex"},
 }, nil
}

const codexArchiveLimit int64 = 256 << 20
const codexDownloadTimeout = 10 * time.Minute

func fetchCodexExecutable(ctx context.Context, asset codexAsset, stage string) error {
 archivePath := filepath.Join(stage, asset.Name+codexArchiveExtension(asset))
 if strings.HasPrefix(asset.URL, "file://") {
  source, err := os.Open(strings.TrimPrefix(asset.URL, "file://"))
  if err != nil { return err }
  defer source.Close()
  target, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
  if err != nil { return err }
  n, copyErr := io.Copy(target, io.LimitReader(source, codexArchiveLimit+1))
  closeErr := target.Close()
  if copyErr != nil { return copyErr }
  if closeErr != nil { return closeErr }
  if n > codexArchiveLimit { return errors.New("native release archive too large") }
 } else {
  if err := downloadCodexRelease(ctx, asset.URL, archivePath); err != nil { return err }
 }
 digest, err := codexFileSHA256(archivePath)
 if err != nil { return err }
 if digest != asset.SHA256 { return fmt.Errorf("%s release checksum mismatch", asset.Name) }
 return unpackCodexArchiveFile(archivePath, asset, stage)
}

func codexFileSHA256(path string) (string, error) {
 file, err := os.Open(path)
 if err != nil { return "", err }
 defer file.Close()
 hash := sha256.New()
 if _, err := io.Copy(hash, file); err != nil { return "", err }
 return hex.EncodeToString(hash.Sum(nil)), nil
}

func downloadCodexRelease(ctx context.Context, url, archivePath string) error {
 return downloadCodexReleaseWithClient(ctx, &http.Client{Timeout: codexDownloadTimeout}, url, archivePath, 2*time.Second)
}

func downloadCodexReleaseWithClient(ctx context.Context, client *http.Client, url, archivePath string, delay time.Duration) error {
 var lastErr error
 for attempt := 0; attempt <= 5; attempt++ {
  if err := ctx.Err(); err != nil { return err }
  if attempt > 0 {
   select {
   case <-ctx.Done(): return ctx.Err()
   case <-time.After(delay):
   }
  }
  offset := int64(0)
  if info, err := os.Stat(archivePath); err == nil {
   if !info.Mode().IsRegular() { return errors.New("unsafe native archive download target") }
   offset = info.Size()
  } else if !os.IsNotExist(err) { return err }
  if offset > codexArchiveLimit { return errors.New("native release archive too large") }
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
  if err != nil { return err }
  if offset > 0 { req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset)) }
  resp, err := client.Do(req)
  if err != nil { lastErr=err; continue }
  if resp.StatusCode != http.StatusOK && !(offset > 0 && resp.StatusCode == http.StatusPartialContent) {
   resp.Body.Close()
   lastErr = fmt.Errorf("native release download HTTP %d", resp.StatusCode)
   if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
    if err := os.Truncate(archivePath, 0); err != nil { return err }
    continue
   }
   if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 { continue }
   return lastErr
  }
  if resp.StatusCode == http.StatusPartialContent {
   var first, last, total int64
   n, parseErr := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &first, &last, &total)
   if parseErr != nil || n != 3 || first != offset || last < first || total <= last || total > codexArchiveLimit {
    resp.Body.Close()
    return errors.New("invalid native release Content-Range")
   }
  }
  flags := os.O_WRONLY|os.O_CREATE|os.O_TRUNC
  if resp.StatusCode == http.StatusPartialContent { flags=os.O_WRONLY|os.O_APPEND }
  target, err := os.OpenFile(archivePath, flags, 0600)
  if err != nil { resp.Body.Close(); return err }
  start := int64(0)
  if resp.StatusCode == http.StatusPartialContent { start=offset }
  remaining := codexArchiveLimit-start
  if resp.ContentLength > remaining {
   resp.Body.Close()
   target.Close()
   return errors.New("native release archive too large")
  }
  written, copyErr := io.Copy(target, io.LimitReader(resp.Body, remaining+1))
  closeErr := target.Close()
  resp.Body.Close()
  if written > remaining { return errors.New("native release archive too large") }
  if copyErr == nil { copyErr=closeErr }
  if copyErr != nil { lastErr=copyErr; continue }
  if resp.ContentLength >= 0 && written != resp.ContentLength {
   lastErr=io.ErrUnexpectedEOF
   continue
  }
  if resp.StatusCode == http.StatusPartialContent {
   var first, last, total int64
   _, _ = fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &first, &last, &total)
   if start+written != total {
    lastErr=io.ErrUnexpectedEOF
    continue
   }
  }
  return nil
 }
 return fmt.Errorf("native release download failed after 6 attempts: %w", lastErr)
}

func codexArchiveExtension(asset codexAsset) string {
 if strings.HasSuffix(asset.URL, ".zip") { return ".zip" }
 return ".tar.gz"
}

func unpackCodexArchive(compressed []byte, asset codexAsset, stage string) error {
 if strings.HasSuffix(asset.URL, ".zip") {
  return unpackCodexZip(bytes.NewReader(compressed), int64(len(compressed)), asset, stage)
 }
 return unpackCodexTar(bytes.NewReader(compressed), asset, stage)
}

func unpackCodexArchiveFile(archivePath string, asset codexAsset, stage string) error {
 file, err := os.Open(archivePath)
 if err != nil { return err }
 defer file.Close()
 if strings.HasSuffix(asset.URL, ".zip") {
  info, err := file.Stat()
  if err != nil { return err }
  return unpackCodexZip(file, info.Size(), asset, stage)
 }
 return unpackCodexTar(file, asset, stage)
}

func unpackCodexZip(reader io.ReaderAt, size int64, asset codexAsset, stage string) error {
  archive, err := zip.NewReader(reader, size)
  if err != nil { return err }
  count := 0
  for _, member := range archive.File {
   name := filepath.Base(strings.ReplaceAll(member.Name, "\\", "/"))
   if member.FileInfo().Mode()&os.ModeSymlink != 0 { return errors.New("native release zip contains symlink") }
   if member.FileInfo().IsDir() || !strings.HasSuffix(strings.ToLower(name), ".exe") { continue }
   if !strings.HasPrefix(strings.ToLower(name), strings.TrimSuffix(asset.Name, ".exe")+"-") && name != asset.Name { continue }
   count++
   if count != 1 || member.UncompressedSize64 > 512<<20 { return fmt.Errorf("invalid %s native release zip", asset.Name) }
   input, err := member.Open()
   if err != nil { return err }
   output, err := os.OpenFile(filepath.Join(stage, asset.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
   if err != nil { input.Close(); return err }
   _, copyErr := io.Copy(output, io.LimitReader(input, (512<<20)+1))
   closeErr := output.Close()
   input.Close()
   if copyErr != nil { return copyErr }
   if closeErr != nil { return closeErr }
  }
  if count != 1 { return fmt.Errorf("native release zip missing %s", asset.Name) }
  return nil
}

func unpackCodexTar(reader io.Reader, asset codexAsset, stage string) error {
 archive, err := gzip.NewReader(reader)
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
  // Codex releases can contain an ELF executable larger than 128 MiB. Never
  // silently truncate a verified upstream archive during extraction: the
  // resulting incomplete ELF can crash on its first --version probe.
  if header.Size <= 0 || header.Size > 512<<20 {
   return fmt.Errorf("invalid %s native executable size: %d", asset.Name, header.Size)
  }
  output, err := os.OpenFile(filepath.Join(stage, asset.Name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
  if err != nil { return err }
  _, copyErr := io.CopyN(output, tarReader, header.Size)
  closeErr := output.Close()
  if copyErr != nil { return fmt.Errorf("extract %s native executable: %w", asset.Name, copyErr) }
  if closeErr != nil { return closeErr }
 }
 if count != 1 { return fmt.Errorf("native archive missing %s", asset.Name) }
 return nil
}

func codexBinaryRecord(path, provenance string) (CodexBinaryRecord, error) {
 return codexBinaryRecordWithACPPolicy(path, provenance, false)
}

func codexACPBinaryRecord(path, provenance string) (CodexBinaryRecord, error) {
 return codexBinaryRecordWithACPPolicy(path, provenance, true)
}

func codexBinaryRecordWithACPPolicy(path, provenance string, isACP bool) (CodexBinaryRecord, error) {
 absolute, err := filepath.Abs(path)
 if err != nil { return CodexBinaryRecord{}, err }
 target, err := filepath.EvalSymlinks(absolute)
 if err != nil { return CodexBinaryRecord{}, err }
 info, err := os.Stat(target)
 if err != nil { return CodexBinaryRecord{}, err }
 if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0111==0) || (runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(target), ".exe")) { return CodexBinaryRecord{}, fmt.Errorf("native Codex pair requires direct executable (Windows .exe only; .cmd and .ps1 launchers are not supported): %s", target) }
 contents, err := os.ReadFile(target)
 if err != nil { return CodexBinaryRecord{}, err }
 sum := sha256.Sum256(contents)
 ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
 defer cancel()
 version, err := exec.CommandContext(ctx, target, "--version").CombinedOutput()
 if err != nil {
  // A versionless ACP adapter is still valid. Only accept the documented
  // CLI usage rejection, not an arbitrary exit 2 or a failed Codex CLI.
  var exitErr *exec.ExitError
  stderr := strings.ToLower(string(version))
  unsupported := isACP && errors.As(err, &exitErr) && exitErr.ExitCode() == 2 &&
   strings.Contains(stderr, "--version") && strings.Contains(stderr, "unexpected argument")
  if !unsupported { return CodexBinaryRecord{}, fmt.Errorf("version probe %s: %w", target, err) }
  help, helpErr := exec.CommandContext(ctx, target, "--help").CombinedOutput()
  if helpErr != nil || len(bytes.TrimSpace(help)) == 0 {
   return CodexBinaryRecord{}, fmt.Errorf("ACP help probe %s: %v", target, helpErr)
  }
  label := codexACPExternalVersionless
  if provenance == "managed" {
   label = codexACPManagedVersionless
   if runtime.GOOS == "windows" { label = "codex-acp " + codexACPWindowsLegacyVersion + codexACPManagedVersionSuffix }
  }
  return CodexBinaryRecord{Path:absolute, ResolvedPath:target, Version:label, SHA256:hex.EncodeToString(sum[:]), Provenance:provenance}, nil
 }
 if strings.TrimSpace(string(version)) == "" { return CodexBinaryRecord{}, fmt.Errorf("empty version: %s", target) }
 return CodexBinaryRecord{Path:absolute, ResolvedPath:target, Version:strings.TrimSpace(string(version)), SHA256:hex.EncodeToString(sum[:]), Provenance:provenance}, nil
}

func codexNativePairReady(ctx context.Context, pair CodexPairInventory) error {
 probeCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
 defer cancel()
 output, err := exec.CommandContext(probeCtx, pair.Codex.Path, "login", "status").CombinedOutput()
 if err != nil {
  if probeCtx.Err() != nil { return fmt.Errorf("Codex native login status transport: %w", probeCtx.Err()) }
  response := strings.ToLower(string(output))
  if strings.Contains(response, "not logged in") || strings.Contains(response, "not authenticated") || strings.Contains(response, "login required") {
   return fmt.Errorf("Codex native authentication required: run %s login", pair.Codex.Path)
  }
  if strings.Contains(response, "quota") || strings.Contains(response, "usage limit") || strings.Contains(response, "rate limit") {
   return fmt.Errorf("Codex native quota/provider restriction: %w", err)
  }
  return fmt.Errorf("Codex native login status executable/protocol failure: %w", err)
 }
 models, _, err := (ExecCommandRunner{}).ObserveCodexACPWithCommand(probeCtx, pair.Codex.Path, pair.ACP.Path)
 if err != nil { return fmt.Errorf("Codex native ACP initialize/session-new failure: %w", err) }
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
 if err := verifyCodexACPBinary(pair.ACP); err != nil { return pair, err }
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

// The Secretary release archive is checksum-verified before installation. It
// contains a standalone Bun-compiled build of a pinned modern ACP adapter.
func packagedCodexACPAsset() (codexAsset, error) {
 exe, err := os.Executable()
 if err != nil { return codexAsset{}, err }
 exe, err = filepath.EvalSymlinks(exe)
 if err != nil { return codexAsset{}, err }
 archivePath := filepath.Join(filepath.Dir(exe), "native-acp", "codex-acp.tar.gz")
 receiptPath := archivePath+".sha256"
 for _, path := range []string{archivePath,receiptPath} {
  info, statErr := os.Lstat(path)
  if statErr != nil { return codexAsset{}, fmt.Errorf("missing packaged modern codex-acp %s: %w", codexACPPinnedVersion, statErr) }
  if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 { return codexAsset{}, errors.New("unsafe packaged ACP artifact") }
 }
 receipt, err := os.ReadFile(receiptPath)
 if err != nil { return codexAsset{}, err }
 fields := strings.Fields(string(receipt))
 if len(fields)!=2 || len(fields[0])!=64 || fields[1]!="codex-acp.tar.gz" { return codexAsset{}, errors.New("invalid packaged ACP checksum receipt") }
 compressed, err := os.ReadFile(archivePath)
 if err != nil { return codexAsset{}, err }
 digest := sha256.Sum256(compressed)
 if !strings.EqualFold(fields[0],hex.EncodeToString(digest[:])) { return codexAsset{}, errors.New("packaged modern ACP checksum mismatch") }
 return codexAsset{URL:"file://"+archivePath,SHA256:strings.ToLower(fields[0]),Name:"codex-acp"},nil
}

func managedACPVersionForPlatform() string {
 if runtime.GOOS=="windows" { return codexACPWindowsLegacyVersion }
 return codexACPPinnedVersion
}

func provisionManagedPair(ctx context.Context) (string, string, error) {
 assets, err := codexNativeAssets()
 if err != nil { return "", "", err }
 if runtime.GOOS != "windows" {
  bundledACP, bundleErr := packagedCodexACPAsset()
  if bundleErr != nil { return "", "", bundleErr }
  assets = append(assets, bundledACP)
 }
 home, err := os.UserHomeDir()
 if err != nil { return "", "", err }
 root := filepath.Join(home, ".local", "share", "secretary", "native", "releases", codexPinnedVersion+"-"+managedACPVersionForPlatform())
 return installManagedCodexAssets(ctx, assets, root, runtime.GOOS+"-"+runtime.GOARCH)
}

func installManagedCodexAssets(ctx context.Context, assets []codexAsset, root, target string) (string, string, error) {
 final := filepath.Join(root, target)
 names := []string{"codex", "codex-acp"}
 if runtime.GOOS == "windows" { names = []string{"codex.exe", "codex-acp.exe"} }
 cli, acp := filepath.Join(final, names[0]), filepath.Join(final, names[1])
 if info, err := os.Lstat(final); err == nil {
  if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 { return "", "", errors.New("unsafe managed release directory") }
  if err := verifyManagedCodexArchives(assets, final); err != nil { return "", "", err }
  cliRecord, err := codexBinaryRecord(cli, "managed")
  if err != nil { return "", "", fmt.Errorf("partial or changed managed Codex CLI: %w", err) }
  acpRecord, err := codexACPBinaryRecord(acp, "managed")
  if err != nil { return "", "", fmt.Errorf("partial or changed managed Codex ACP: %w", err) }
  if !strings.Contains(cliRecord.Version, codexPinnedVersion) || !strings.Contains(acpRecord.Version, managedACPVersionForPlatform()) { return "", "", errors.New("managed Codex pair version mismatch") }
  return cli, acp, nil
 } else if !os.IsNotExist(err) { return "", "", err }
 if err := os.MkdirAll(root, 0700); err != nil { return "", "", err }
 stage, err := os.MkdirTemp(root, ".codex-stage-")
 if err != nil { return "", "", err }
 defer os.RemoveAll(stage)
 for _, asset := range assets {
  if err := fetchCodexExecutable(ctx, asset, stage); err != nil { return "", "", err }
 }
 cliRecord, err := codexBinaryRecord(filepath.Join(stage, names[0]), "managed")
 if err != nil { return "", "", err }
 acpRecord, err := codexACPBinaryRecord(filepath.Join(stage, names[1]), "managed")
 if err != nil { return "", "", err }
 if !strings.Contains(cliRecord.Version, codexPinnedVersion) || !strings.Contains(acpRecord.Version, managedACPVersionForPlatform()) { return "", "", errors.New("downloaded Codex pair versions differ from pinned release") }
 if err := os.Rename(stage, final); err != nil { return "", "", err }
 return cli, acp, nil
}

func verifyManagedCodexArchives(assets []codexAsset, final string) error {
 for _, asset := range assets {
  archivePath := filepath.Join(final, asset.Name+codexArchiveExtension(asset))
  info, err := os.Lstat(archivePath)
  if err != nil || !info.Mode().IsRegular() { return fmt.Errorf("managed %s trusted archive missing or unsafe", asset.Name) }
  if info.Size() > codexArchiveLimit { return errors.New("managed native release archive too large") }
  digest, err := codexFileSHA256(archivePath)
  if err != nil { return err }
  if digest != asset.SHA256 { return fmt.Errorf("managed %s trusted archive digest mismatch", asset.Name) }
  stage, err := os.MkdirTemp("", "codex-verified-")
  if err != nil { return err }
  unpackErr := unpackCodexArchiveFile(archivePath, asset, stage)
  if unpackErr != nil { os.RemoveAll(stage); return unpackErr }
  trustedDigest, err := codexFileSHA256(filepath.Join(stage, asset.Name))
  os.RemoveAll(stage)
  if err != nil { return err }
  actualPath := filepath.Join(final, asset.Name)
  info, err = os.Lstat(actualPath)
  if err != nil || !info.Mode().IsRegular() { return fmt.Errorf("managed %s selected binary missing or unsafe", asset.Name) }
  actualDigest, err := codexFileSHA256(actualPath)
  if err != nil { return err }
  if actualDigest != trustedDigest { return fmt.Errorf("managed %s executable bytes differ from pinned upstream archive", asset.Name) }
 }
 return nil
}

type codexPendingUpgrade struct {
 Previous CodexPairInventory `json:"previous"`
 Candidate CodexPairInventory `json:"candidate"`
}

func recoverPendingCodexUpgrade(scope string) error {
 location, err := CodexPairInventoryPath(scope)
 if err != nil { return err }
 pendingPath := filepath.Join(filepath.Dir(location), "codex-pair.pending.json")
 info, err := os.Lstat(pendingPath)
 if os.IsNotExist(err) { return nil }
 if err != nil { return err }
 if !info.Mode().IsRegular() { return errors.New("unsafe pending native pair transaction") }
 data, err := os.ReadFile(pendingPath)
 if err != nil { return err }
 var pending codexPendingUpgrade
 if err := json.Unmarshal(data, &pending); err != nil { return err }
 if pending.Previous.Scope != scope || pending.Candidate.Scope != scope ||
  pending.Previous.Schema != 1 || pending.Candidate.Schema != 1 ||
  pending.Previous.Harness != "codex" || pending.Candidate.Harness != "codex" {
  return errors.New("native pair pending transaction mismatch")
 }
 current, err := readCodexPairUnchecked(scope)
 if err != nil { return fmt.Errorf("native pair interrupted activation needs explicit repair: %w", err) }
 if current.Codex.SHA256 == pending.Previous.Codex.SHA256 && current.ACP.SHA256 == pending.Previous.ACP.SHA256 &&
  current.Codex.Path == pending.Previous.Codex.Path && current.ACP.Path == pending.Previous.ACP.Path {
  return os.Remove(pendingPath)
 }
 if current.Codex.SHA256 != pending.Candidate.Codex.SHA256 || current.ACP.SHA256 != pending.Candidate.ACP.SHA256 ||
  current.Codex.Path != pending.Candidate.Codex.Path || current.ACP.Path != pending.Candidate.ACP.Path {
  return errors.New("native pair pending transaction conflicts with active inventory")
 }
 if err := verifyCodexBinary(pending.Previous.Codex); err != nil { return fmt.Errorf("rollback CLI unavailable: %w", err) }
 if err := verifyCodexACPBinary(pending.Previous.ACP); err != nil { return fmt.Errorf("rollback ACP unavailable: %w", err) }
 if err := saveCodexPair(filepath.Join(filepath.Dir(location), "codex-pair.previous.json"), pending.Previous); err != nil { return err }
 return os.Remove(pendingPath)
}

func activateCodexUpgrade(scope string, oldPair, candidate CodexPairInventory) error {
 location, err := CodexPairInventoryPath(scope)
 if err != nil { return err }
 pending := filepath.Join(filepath.Dir(location), "codex-pair.pending.json")
 journal, err := json.Marshal(codexPendingUpgrade{Previous: oldPair, Candidate: candidate})
 if err != nil { return err }
 if err := os.MkdirAll(filepath.Dir(pending), 0700); err != nil { return err }
 if info, err := os.Lstat(pending); err == nil && info != nil { return errors.New("unreconciled native pair upgrade transaction") } else if err != nil && !os.IsNotExist(err) { return err }
 stage, err := os.CreateTemp(filepath.Dir(pending), ".codex-pending-")
 if err != nil { return err }
 defer os.Remove(stage.Name())
 if err := stage.Chmod(0600); err != nil { stage.Close(); return err }
 if _, err := stage.Write(journal); err != nil { stage.Close(); return err }
 if err := stage.Sync(); err != nil { stage.Close(); return err }
 if err := stage.Close(); err != nil { return err }
 if err := os.Rename(stage.Name(), pending); err != nil { return err }
 if err := saveCodexPair(location, candidate); err != nil { return err }
 if err := saveCodexPair(filepath.Join(filepath.Dir(location), "codex-pair.previous.json"), oldPair); err != nil { return err }
 return os.Remove(pending)
}

func ManageCodexPair(ctx context.Context, action, scope, cliPath, acpPath string) (string, error) {
 return manageCodexPairWithProbe(ctx, action, scope, cliPath, acpPath, codexNativePairReady)
}

func manageCodexPairWithProbe(ctx context.Context, action, scope, cliPath, acpPath string, probe func(context.Context, CodexPairInventory) error) (string, error) {
 unlock, err := lockCodexPairScope(scope)
 if err != nil { return "", err }
 defer unlock()
 location, err := CodexPairInventoryPath(scope)
 if err != nil { return "", err }
 if err := recoverPendingCodexUpgrade(scope); err != nil { return "", err }
 existing, existingErr := readCodexPairUnchecked(scope)
 switch action {
 case "env", "show", "doctor", "readiness":
  if existingErr != nil { return "", existingErr }
  if action == "env" { return "CODEX_PATH="+existing.Codex.Path+"\nSECRETARY_ACP_COMMAND="+existing.ACP.Path, nil }
  if action == "show" { bytes, _ := json.MarshalIndent(existing, "", "  "); return string(bytes), nil }
  if action == "readiness" {
   if err := probe(ctx, existing); err != nil { return "", err }
   if err := verifyCodexBinary(existing.Codex); err != nil { return "", err }
   if err := verifyCodexACPBinary(existing.ACP); err != nil { return "", err }
   existing.Compatibility.Validated = true
   if err := saveCodexPair(location, existing); err != nil { return "", err }
  }
  if action == "doctor" {
   if !existing.Compatibility.Validated { return "", errors.New("native Codex pair not protocol-validated") }
   if err := probe(ctx, existing); err != nil { return "", err }
  }
  return fmt.Sprintf("Codex %s and ACP %s: %s", existing.Codex.Version, existing.ACP.Version, action), nil
 case "install", "upgrade":
  if (cliPath=="") != (acpPath=="") { return "", errors.New("both external Codex and ACP paths must be selected together") }
  if action=="install" && existingErr!=nil { if _, err:=os.Lstat(location); err==nil { return "", existingErr } }
  if action=="install" && existingErr==nil {
   if existing.Codex.Provenance=="managed" || existing.ACP.Provenance=="managed" {
    if existing.Codex.Provenance!="managed" || existing.ACP.Provenance!="managed" { return "", errors.New("partial managed/external pair provenance") }
    managedCLI, managedACP, err := provisionManagedPair(ctx)
    if err != nil { return "", err }
    if existing.Codex.Path!=managedCLI || existing.ACP.Path!=managedACP { return "", errors.New("selected managed pair paths differ from verified pinned release") }
   }
   if cliPath!="" {
    requested, err := filepath.Abs(cliPath)
    if err!=nil { return "",err }
    requestedACP, err := filepath.Abs(acpPath)
    if err!=nil { return "",err }
    if requested!=existing.Codex.Path || requestedACP!=existing.ACP.Path { return "", errors.New("pair already selected; explicit upgrade required") }
   }
   return "Existing native Codex pair preserved", nil
  }
  if action=="upgrade" && existingErr!=nil && !os.IsNotExist(existingErr) { return "", existingErr }
  if action=="upgrade" && existingErr==nil && existing.Codex.Provenance=="managed" && cliPath=="" { return "", errors.New("managed pair is already pinned to this release; choose a new compatible pair explicitly") }
  provenance:="external"
  if cliPath=="" {
   cliPath, acpPath, err = provisionManagedPair(ctx)
   if err != nil { return "", err }
   provenance="managed"
  }
  cli, err := codexBinaryRecord(cliPath, provenance)
  if err != nil { return "", err }
  acp, err := codexACPBinaryRecord(acpPath, provenance)
  if err != nil { return "", err }
  pair := CodexPairInventory{Schema:1,Scope:scope,Harness:"codex",Codex:cli,ACP:acp}
  if action=="upgrade" {
   if err := probe(ctx,pair); err != nil { return "", err }
   if err := verifyCodexBinary(pair.Codex); err != nil { return "", err }
   if err := verifyCodexACPBinary(pair.ACP); err != nil { return "", err }
   pair.Compatibility.Validated=true
  }
  if action == "upgrade" && existingErr == nil {
   if err := activateCodexUpgrade(scope, existing, pair); err != nil { return "", err }
  } else {
   if err := saveCodexPair(location,pair); err != nil { return "", err }
  }
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
  if err := verifyCodexACPBinary(pair.ACP); err!=nil { return "",err }
  if err := probe(ctx,pair); err!=nil { return "",err }
  pair.Compatibility.Validated=true
  if err := saveCodexPair(location,pair); err!=nil { return "",err }
  if err := os.Remove(previous); err!=nil { return "",err }
  return "Previous native Codex pair restored",nil
 default:
  return "", fmt.Errorf("unsupported native pair action %q", action)
 }
}
