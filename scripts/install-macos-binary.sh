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
for tool in ffmpeg ffprobe; do
  [[ -x "$payload/bin/video-tools/$tool" && ! -L "$payload/bin/video-tools/$tool" ]] || fail "Missing Server video tool: $tool"
done
[[ -f "$payload/VIDEO-TOOLS.json" && ! -L "$payload/VIDEO-TOOLS.json" ]] || fail "Missing video tool manifest"
video_version="$(plutil -extract ffmpeg.version raw -o - "$payload/VIDEO-TOOLS.json")" || fail "Unreadable video version"
[[ -n "$video_version" && "$video_version" == "$(plutil -extract ffprobe.version raw -o - "$payload/VIDEO-TOOLS.json")" ]] || fail "Incompatible video pair"
for tool in ffmpeg ffprobe; do
  expected="$(plutil -extract "$tool.sha256" raw -o - "$payload/VIDEO-TOOLS.json")" || fail "Missing video hash"
  actual="$(shasum -a 256 "$payload/bin/video-tools/$tool" | awk '{print $1}')"
  [[ "$actual" == "$expected" ]] || fail "Video tool checksum mismatch: $tool"
  observed="$("$payload/bin/video-tools/$tool" -version | head -n 1 | awk '{print $3}')" || fail "Broken video tool: $tool"
  [[ "$observed" == "$video_version" ]] || fail "Video tool version mismatch: $tool"
done
[[ -f "$payload/secretary" && -f "$payload/scripts/secretary-interactive.zsh" ]] || fail "Incomplete Secretary CLI"
[[ -f "$payload/VERSION" && "$(cat "$payload/VERSION")" == "$version" ]] || fail "Packaged version mismatch"
[[ -f "$payload/REVISION" && "$(cat "$payload/REVISION")" == "$revision" ]] || fail "Packaged revision mismatch"
[[ -x "$payload/Secretary Node.app/Contents/MacOS/SecretaryNode" ]] || fail "Native Secretary Node.app missing"
codesign --verify "$payload/Secretary Node.app" || fail "Secretary Node.app signature verification failed; do not bypass Gatekeeper"

releases="$HOME/.local/share/secretary/releases"
current_release="$HOME/.local/share/secretary/current"
bin="$HOME/.local/bin"
app="$HOME/Applications/Secretary Node.app"
mkdir -p "$releases" "$bin" "$HOME/Applications"
[[ ! -L "$releases" && ! -L "$bin" ]] || fail "Installation directories may not be symlinks"
target="$releases/$name"
if [[ -e "$target" ]]; then
  [[ -d "$target" && ! -L "$target" && -f "$target/REVISION" && "$(cat "$target/REVISION")" == "$revision" ]] || fail "Existing release directory conflicts with requested version"
  [[ -x "$target/bin/secretaryd" && -x "$target/bin/secretary-node" ]] || fail "Existing release is incomplete"
  cmp -s "$stage/manifest.json" "$target/RELEASE-MANIFEST.json" || fail "Existing cached release provenance differs from selected manifest"
  diff -qr -x RELEASE-MANIFEST.json "$payload" "$target" >/dev/null || fail "Cached release contents differ from verified archive; remove the damaged release directory before retrying"
else
  release_stage="$(mktemp -d "$releases/.release.XXXXXXXX")" || fail "Cannot create release staging directory"
  trap 'rm -rf "$release_stage" "$stage"' EXIT
  cp -R "$payload" "$release_stage/$name" || fail "Cannot stage release contents"
  cp "$stage/manifest.json" "$release_stage/$name/RELEASE-MANIFEST.json" || fail "Cannot stage release provenance"
  cmp -s "$stage/manifest.json" "$release_stage/$name/RELEASE-MANIFEST.json" || fail "Staged release provenance mismatch"
  for binary in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
    [[ -x "$release_stage/$name/bin/$binary" ]] || fail "Staged release binary missing: $binary"
  done
  codesign --verify "$release_stage/$name/Secretary Node.app" || fail "Staged native app signature invalid"
  mv "$release_stage/$name" "$target" || fail "Cannot publish verified release"
  rmdir "$release_stage"
  release_stage=""
  trap 'rm -rf "$stage"' EXIT
fi
if [[ -e "$current_release" || -L "$current_release" ]]; then
  [[ -L "$current_release" ]] || fail "Existing current release path is not a symlink"
  previous_release="$(readlink "$current_release")"
  case "$previous_release" in "$releases/"*) ;; *) fail "Existing current release is unmanaged" ;; esac
else
  previous_release=""
fi
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  if [[ -e "$path" || -L "$path" ]]; then
    [[ -L "$path" ]] || fail "Existing $path is not managed; source installation is left unchanged"
    [[ "$(readlink "$path")" == "$current_release/"* ]] || fail "Existing $path belongs to another installation; source installation is left unchanged"
  fi
done
legacy="$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
[[ ! -e "$legacy" ]] || fail "CLI Node LaunchAgent conflicts with menu-managed Node; uninstall it first"
[[ ! -e "$app" || -d "$app" ]] || fail "Existing app location is not a directory"
if [[ -d "$app" ]]; then
  existing_id="$(plutil -extract CFBundleIdentifier raw -o - "$app/Contents/Info.plist" 2>/dev/null || true)"
  new_id="$(plutil -extract CFBundleIdentifier raw -o - "$target/Secretary Node.app/Contents/Info.plist")"
  [[ "$existing_id" == "$new_id" ]] || fail "Another application occupies $app"
fi

app_stage="$stage/new-node.app"
cp -R "$target/Secretary Node.app" "$app_stage"
codesign --verify "$app_stage" || fail "Staged Node.app did not verify"
SECRETARY_BINARY_PREINSTALLED_ONLY=1 SECRETARY_BINARY_STAGE_BIN="$stage/setup-bin" "$target/secretary" setup --non-interactive || fail "Private setup failed; active release has not changed"

backup="$stage/previous-app"
had_app=false
[[ ! -d "$app" ]] || had_app=true
activated=false
created_links=()
pointer_stage="$HOME/.local/share/secretary/.current.$$.new"
activation_rollback() {
  if [[ "$activated" != true ]]; then
    if [[ -L "$current_release" && "$(readlink "$current_release")" == "$target" ]]; then
      rm -f "$current_release"
      if [[ -n "$previous_release" ]]; then ln -s "$previous_release" "$current_release"; fi
    fi
    for created in "${created_links[@]}"; do rm -f "$created"; done
    if [[ -d "$backup" ]]; then
      rm -rf "$app"
      mv "$backup" "$app"
    elif [[ "$had_app" == false ]]; then
      rm -rf "$app"
    fi
    rm -f "$pointer_stage"
  fi
}
trap 'activation_rollback; rm -rf "$stage"' EXIT
if [[ -d "$app" ]]; then mv "$app" "$backup"; fi
mv "$app_stage" "$app" || fail "Cannot activate native Node.app"
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  if [[ ! -L "$path" ]]; then
    dest="$current_release/bin/$executable"
    [[ "$executable" == secretary ]] && dest="$current_release/secretary"
    ln -s "$dest" "$path" || fail "Cannot activate $executable"
    created_links+=("$path")
  fi
done
ln -s "$target" "$pointer_stage" || fail "Cannot stage release switch"
mv -fh "$pointer_stage" "$current_release" || fail "Cannot activate release pointer"
activated=true
printf 'Installed Secretary %s (%s) for macOS %s. Start Server with secretary start --no-open; launch Secretary Node from Spotlight.\n' "$version" "$revision" "$arch"
