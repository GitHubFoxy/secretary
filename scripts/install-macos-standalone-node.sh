#!/usr/bin/env bash
set -euo pipefail
umask 077
fail() { printf 'Secretary Node install: %s\n' "$*" >&2; exit 1; }
[[ "$(uname -s)" == Darwin ]] || fail "macOS is required"
for tool in curl tar shasum plutil codesign awk mktemp install diff cmp; do command -v "$tool" >/dev/null || fail "Missing $tool"; done
case "$(uname -m)" in arm64) arch=arm64 ;; x86_64) arch=amd64 ;; *) fail "Unsupported architecture" ;; esac
server="${SECRETARY_NODE_SERVER:-}"
[[ "$server" =~ ^https://[A-Za-z0-9._:-]+(/[-A-Za-z0-9._~/]*)?$ ]] || fail "SECRETARY_NODE_SERVER must be a private reachable HTTPS endpoint without userinfo"
[[ -n "${SECRETARY_NODE_NAME:-}" && "${SECRETARY_NODE_NAME}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || fail "Set SECRETARY_NODE_NAME to a valid stable Node name"
case "${SECRETARY_NODE_HARNESS:-}" in
  claude) command -v claude >/dev/null || fail "Install and authenticate native Claude Code first" ;;
  codex) ;;
  *) fail "Set SECRETARY_NODE_HARNESS=claude or codex" ;;
esac
root="$HOME/.local/share/secretary-node"
releases="$root/releases"
current="$root/current"
app="$HOME/Applications/Secretary Node.app"
legacy="$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
[[ ! -e "$legacy" ]] || fail "Legacy CLI Node LaunchAgent conflicts; uninstall its service first"
if launchctl print "gui/$(id -u)/dev.secretary.cli-node" >/dev/null 2>&1; then fail "Legacy CLI Node service is still loaded"; fi
[[ ! -e "$HOME/.local/share/secretary/current" && ! -L "$HOME/.local/share/secretary/current" ]] || fail "Combined Secretary installation owns the menu bar app; keep installations separate"
[[ ! -L "$root" && ! -L "$releases" && ! -L "$HOME/Applications" ]] || fail "Managed directories must not be symlinks"
mkdir -p "$releases" "$HOME/Applications"
chmod 700 "$root" "$releases"
stage="$(mktemp -d "${TMPDIR:-/tmp}/secretary-node-install.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT
version="${SECRETARY_VERSION:-}"
if [[ -z "$version" ]]; then
  curl -fsSL --retry 2 -o "$stage/latest.json" "https://api.github.com/repos/GitHubFoxy/secretary/releases/latest" || fail "Cannot resolve latest release"
  tag="$(plutil -extract tag_name raw -o - "$stage/latest.json")" || fail "Malformed release metadata"
  version="${tag#v}"
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid release version"
name="secretary-v$version-darwin-$arch"
url="${SECRETARY_RELEASE_BASE:-https://github.com/GitHubFoxy/secretary/releases/download/v$version}"
curl -fsSL --retry 2 -o "$stage/manifest.json" "$url/$name.json" || fail "Missing release manifest"
field() { plutil -extract "$1" raw -o - "$stage/manifest.json"; }
[[ "$(field schema)" == 1 && "$(field version)" == "$version" && "$(field os)" == darwin && "$(field arch)" == "$arch" ]] || fail "Manifest mismatch"
revision="$(field revision)"
[[ "$revision" =~ ^[[:xdigit:]]{40}$ && "$(field archive)" == "$name.tar.gz" ]] || fail "Invalid manifest revision/archive"
sha="$(field sha256)"
[[ "$sha" =~ ^[[:xdigit:]]{64}$ ]] || fail "Invalid SHA-256"
curl -fsSL --retry 2 -o "$stage/archive.tar.gz" "$url/$name.tar.gz" || fail "Missing published archive"
[[ "$(shasum -a 256 "$stage/archive.tar.gz" | awk '{print $1}')" == "$sha" ]] || fail "Archive digest mismatch"
[[ "$(wc -c < "$stage/archive.tar.gz" | tr -d '[:space:]')" == "$(field bytes)" ]] || fail "Archive length mismatch"
tar -tzf "$stage/archive.tar.gz" > "$stage/entries" || fail "Invalid archive"
while IFS= read -r path; do
  [[ "$path" == "$name" || "$path" == "$name/"* ]] || fail "Unexpected archive root"
  [[ "$path" != /* && "$path" != *"/../"* && "$path" != *"/./"* && "$path" != */.. ]] || fail "Unsafe archive path"
done < "$stage/entries"
mkdir -p "$stage/extract"
tar -xzf "$stage/archive.tar.gz" -C "$stage/extract"
payload="$stage/extract/$name"
[[ -x "$payload/bin/secretary-node" && ! -L "$payload/bin/secretary-node" ]] || fail "Missing Node binary"
[[ -x "$payload/Secretary Node.app/Contents/MacOS/SecretaryNode" ]] || fail "Missing menu bar companion"
[[ "$(cat "$payload/VERSION")" == "$version" && "$(cat "$payload/REVISION")" == "$revision" ]] || fail "Payload provenance mismatch"
codesign --verify "$payload/Secretary Node.app" || fail "Invalid companion signature"
target="$releases/$name-$revision"
if [[ -e "$target" ]]; then
  [[ -d "$target" && ! -L "$target" ]] || fail "Invalid cached release"
  cmp -s "$stage/manifest.json" "$target/RELEASE-MANIFEST.json" || fail "Cached manifest mismatch"
  cmp -s "$payload/bin/secretary-node" "$target/bin/secretary-node" || fail "Cached Node changed"
  diff -qr "$payload/Secretary Node.app" "$target/Secretary Node.app" >/dev/null || fail "Cached companion changed"
else
  mkdir -p "$stage/node-release/bin"
  cp "$payload/bin/secretary-node" "$stage/node-release/bin/secretary-node"
  cp -R "$payload/Secretary Node.app" "$stage/node-release/Secretary Node.app"
  cp "$stage/manifest.json" "$stage/node-release/RELEASE-MANIFEST.json"
  codesign --verify "$stage/node-release/Secretary Node.app" || fail "Staged companion signature invalid"
  mv "$stage/node-release" "$target"
fi
config="$root/config.json"
data="$root/data"
if [[ -e "$config" ]]; then
  [[ -f "$config" && ! -L "$config" ]] || fail "Unsafe existing Node config"
  [[ ! -L "$data" ]] || fail "Unsafe Node state"
  [[ "$(plutil -extract server_url raw -o - "$config" 2>/dev/null || true)" == "$server" ]] || fail "Existing enrollment Server differs; revoke/re-enroll explicitly"
else
  [[ ! -e "$data" && ! -L "$data" ]] || fail "State exists without config; refusing identity reset"
  mkdir -m 700 -p "$data"
  printf '{"server_url":"%s","node":"%s","data_dir":"%s","capacity":1,"include_opencode":false,"standalone":true}\n' "$server" "$SECRETARY_NODE_NAME" "$data" > "$stage/config.json"
  install -m 600 "$stage/config.json" "$config"
fi
if [[ -n "${SECRETARY_NODE_PAIRING_TOKEN:-}" && ! -f "$data/identity.json" ]]; then
  SECRETARY_NODE_PAIRING_TOKEN="$SECRETARY_NODE_PAIRING_TOKEN" "$target/bin/secretary-node" --config "$config" --enroll-only || fail "Remote enrollment failed"
fi
[[ -f "$data/identity.json" ]] || fail "Owner-approved enrollment required: supply SECRETARY_NODE_PAIRING_TOKEN"
if [[ "$SECRETARY_NODE_HARNESS" == codex ]]; then
  "$target/bin/secretary-node" --codex-pair-action install --codex-pair-scope node || fail "Managed Codex CLI/ACP provisioning failed"
fi
[[ ! -e "$current" || -L "$current" ]] || fail "Current Node release is unmanaged"
old=""
if [[ -L "$current" ]]; then old="$(readlink "$current")"; [[ "$old" == "$releases/"* ]] || fail "Current Node release outside managed root"; fi
if [[ -d "$app" ]]; then
  old_id="$(plutil -extract CFBundleIdentifier raw -o - "$app/Contents/Info.plist" 2>/dev/null || true)"
  new_id="$(plutil -extract CFBundleIdentifier raw -o - "$target/Secretary Node.app/Contents/Info.plist")"
  [[ "$old_id" == "$new_id" ]] || fail "Unrelated app occupies target"
elif [[ -e "$app" || -L "$app" ]]; then fail "Unsafe app target"; fi
cp -R "$target/Secretary Node.app" "$stage/Secretary Node.app"
codesign --verify "$stage/Secretary Node.app" || fail "Invalid staged app"
had_app=false
[[ ! -d "$app" ]] || had_app=true
activated=false
rollback() {
  if [[ "$activated" == false ]]; then
    if [[ -L "$current" && "$(readlink "$current")" == "$target" ]]; then
      rm -f "$current"
      [[ -z "$old" ]] || ln -s "$old" "$current"
    fi
    if [[ -d "$stage/old-app" ]]; then rm -rf "$app"; mv "$stage/old-app" "$app"; elif [[ "$had_app" == false ]]; then rm -rf "$app"; fi
  fi
}
trap 'rollback; rm -rf "$stage"' EXIT
if [[ -d "$app" ]]; then mv "$app" "$stage/old-app"; fi
mv "$stage/Secretary Node.app" "$app" || fail "Cannot install menu bar app"
ln -s "$target" "$stage/current.new"
mv -fh "$stage/current.new" "$current" || fail "Cannot switch Node release"
activated=true
printf 'Installed standalone Secretary Node %s (%s). Launch Secretary Node from Spotlight.\n' "$version" "$revision"
