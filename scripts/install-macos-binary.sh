#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Secretary install: %s\n' "$*" >&2; exit 1; }
[[ "$(uname -s)" == Darwin ]] || fail "Only macOS is supported by this installer"
for utility in curl tar shasum plutil awk mktemp; do
  command -v "$utility" >/dev/null 2>&1 || fail "Required macOS utility missing: $utility"
done
case "$(uname -m)" in
  arm64) arch=arm64 ;;
  x86_64) arch=amd64 ;;
  *) fail "Unsupported macOS architecture: $(uname -m)" ;;
esac

base="https://github.com/GitHubFoxy/secretary/releases"
stage="$(mktemp -d "${TMPDIR:-/tmp}/secretary-binary.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT

version="${SECRETARY_VERSION:-}"
if [[ -z "$version" ]]; then
  curl -fsSL --retry 2 -o "$stage/latest.json" "https://api.github.com/repos/GitHubFoxy/secretary/releases/latest" || fail "Cannot resolve latest published release; set SECRETARY_VERSION"
  tag="$(plutil -extract tag_name raw -o - "$stage/latest.json")" || fail "Malformed GitHub release metadata"
  version="${tag#v}"
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid release version"
name="secretary-v$version-darwin-$arch"
release_url="$base/download/v$version"
if [[ -n "${SECRETARY_RELEASE_BASE:-}" ]]; then release_url="$SECRETARY_RELEASE_BASE"; fi
curl -fsSL --retry 2 -o "$stage/manifest.json" "$release_url/$name.json" || fail "Missing versioned macOS release manifest"
field() { plutil -extract "$1" raw -o - "$stage/manifest.json"; }
[[ "$(field schema)" == 1 && "$(field version)" == "$version" && "$(field os)" == darwin && "$(field arch)" == "$arch" ]] || fail "Incompatible or malformed release manifest"
revision="$(field revision)"
[[ "$revision" =~ ^[[:xdigit:]]{40}$ ]] || fail "Invalid release revision"
[[ "$(field archive)" == "$name.tar.gz" ]] || fail "Unexpected archive name in manifest"
expected="$(field sha256)"
[[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || fail "Invalid release SHA-256"
curl -fsSL --retry 2 -o "$stage/$name.tar.gz" "$release_url/$name.tar.gz" || fail "Release archive unavailable"
actual="$(shasum -a 256 "$stage/$name.tar.gz" | awk '{print $1}')"
[[ "$actual" == "$expected" ]] || fail "Release SHA-256 mismatch; existing installation untouched"
[[ "$(wc -c < "$stage/$name.tar.gz" | tr -d '[:space:]')" == "$(field bytes)" ]] || fail "Release archive size mismatch"

mkdir -p "$stage/unpacked"
tar -tzf "$stage/$name.tar.gz" > "$stage/entries" || fail "Damaged release archive"
while IFS= read -r entry; do
  [[ "$entry" == "$name/"* || "$entry" == "$name" ]] || fail "Unexpected archive root"
  [[ "$entry" != *"/../"* && "$entry" != *"/./"* && "$entry" != */.. && "$entry" != /* ]] || fail "Unsafe archive path"
done < "$stage/entries"
tar -xzf "$stage/$name.tar.gz" -C "$stage/unpacked" || fail "Cannot unpack release"
payload="$stage/unpacked/$name"
for binary in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ -x "$payload/bin/$binary" && ! -L "$payload/bin/$binary" ]] || fail "Missing packaged $binary"
done
[[ -f "$payload/secretary" && -f "$payload/scripts/secretary-interactive.zsh" ]] || fail "Incomplete Secretary CLI"
[[ -f "$payload/VERSION" && "$(cat "$payload/VERSION")" == "$version" ]] || fail "Packaged version mismatch"
[[ -f "$payload/REVISION" && "$(cat "$payload/REVISION")" == "$revision" ]] || fail "Packaged revision mismatch"
[[ -x "$payload/Secretary Node.app/Contents/MacOS/SecretaryNode" ]] || fail "Native Secretary Node.app missing"
codesign --verify "$payload/Secretary Node.app" || fail "Secretary Node.app signature verification failed; do not bypass Gatekeeper"

releases="$HOME/.local/share/secretary/releases"
bin="$HOME/.local/bin"
app="$HOME/Applications/Secretary Node.app"
mkdir -p "$releases" "$bin" "$HOME/Applications"
[[ ! -L "$releases" && ! -L "$bin" ]] || fail "Installation directories may not be symlinks"
target="$releases/$name"
if [[ -e "$target" ]]; then
  [[ -d "$target" && ! -L "$target" && -f "$target/REVISION" && "$(cat "$target/REVISION")" == "$revision" ]] || fail "Existing release directory conflicts with requested version"
else
  mv "$payload" "$target"
fi
for name_in_bin in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  current="$bin/$name_in_bin"
  if [[ -e "$current" || -L "$current" ]]; then
    [[ -L "$current" ]] || fail "Existing $current is not a symlink; source installation is left unchanged"
    link="$(readlink "$current")"
    case "$link" in
      "$releases/"*) ;;
      *) fail "Existing $current belongs to a source or external installation; choose the binary installer on a clean profile" ;;
    esac
  fi
done

legacy="$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
[[ ! -e "$legacy" ]] || fail "CLI Node LaunchAgent conflicts with menu-managed Node; uninstall it first"
if [[ -e "$app" && ! -d "$app" ]]; then fail "Existing app location is not a directory"; fi
if [[ -d "$app" ]]; then
  existing_id="$(plutil -extract CFBundleIdentifier raw -o - "$app/Contents/Info.plist" 2>/dev/null || true)"
  new_id="$(plutil -extract CFBundleIdentifier raw -o - "$target/Secretary Node.app/Contents/Info.plist")"
  [[ "$existing_id" == "$new_id" ]] || fail "Another application occupies $app"
fi
app_stage="$HOME/Applications/.secretary-node-$version-$arch.staging.app"
[[ ! -e "$app_stage" ]] || fail "Staging app already exists at $app_stage"
cp -R "$target/Secretary Node.app" "$app_stage"
codesign --verify "$app_stage" || fail "Staged Node.app did not verify"

SECRETARY_BINARY_STAGE_BIN="$stage/setup-bin" "$target/secretary" setup --non-interactive || fail "Setup failed; no executable or app activation was changed"
backup="$stage/previous-app"
if [[ -d "$app" ]]; then mv "$app" "$backup"; fi
if ! mv "$app_stage" "$app"; then
  [[ ! -d "$backup" ]] || mv "$backup" "$app"
  fail "Cannot activate Secretary Node.app"
fi
for name_in_bin in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  source_path="$target/$name_in_bin"
  [[ "$name_in_bin" == secretary ]] || source_path="$target/bin/$name_in_bin"
  temp_link="$bin/.$name_in_bin.$$.new"
  ln -s "$source_path" "$temp_link"
  mv -f "$temp_link" "$bin/$name_in_bin"
done
printf 'Installed Secretary %s (%s) for macOS %s. Start Server with secretary start --no-open; launch Secretary Node from Spotlight.\n' "$version" "$revision" "$arch"
