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
  curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o "$stage/latest.json" "https://api.github.com/repos/GitHubFoxy/secretary/releases/latest" || fail "Cannot resolve latest published release; set SECRETARY_VERSION"
  tag="$(plutil -extract tag_name raw -o - "$stage/latest.json")" || fail "Malformed GitHub release metadata"
  version="${tag#v}"
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid release version"
name="secretary-v$version-darwin-$arch"
release_url="$base/download/v$version"
if [[ -n "${SECRETARY_RELEASE_BASE:-}" ]]; then release_url="$SECRETARY_RELEASE_BASE"; fi
curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o "$stage/manifest.json" "$release_url/$name.json" || fail "Missing versioned macOS release manifest"
field() { plutil -extract "$1" raw -o - "$stage/manifest.json"; }
[[ "$(field schema)" == 1 && "$(field version)" == "$version" && "$(field os)" == darwin && "$(field arch)" == "$arch" ]] || fail "Incompatible or malformed release manifest"
revision="$(field revision)"
[[ "$revision" =~ ^[[:xdigit:]]{40}$ ]] || fail "Invalid release revision"
[[ "$(field archive)" == "$name.tar.gz" ]] || fail "Unexpected archive name in manifest"
expected="$(field sha256)"
[[ "$expected" =~ ^[[:xdigit:]]{64}$ ]] || fail "Invalid release SHA-256"
curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o "$stage/$name.tar.gz" "$release_url/$name.tar.gz" || fail "Release archive unavailable"
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

root="$HOME/.local/share/secretary"
releases="$root/releases"
current_release="$root/current"
public_bin="$HOME/.local/bin"
bin="$public_bin"
app="$HOME/Applications/Secretary Node.app"
mkdir -p "$releases" "$public_bin" "$HOME/Applications"
[[ ! -L "$root" && ! -L "$releases" && ! -L "$public_bin" ]] || fail "Installation directories may not be symlinks"
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
# Before alpha.8 macOS linked CLIs directly to the immutable versioned
# release directory. Those old links are ours only when they point EXACTLY to a
# complete, locally installed Secretary macOS release for this architecture.
# Migrate such links to current; never rewrite an arbitrary user/source link.
release_link_relative() {
  if [[ "$1" == secretary ]]; then printf 'secretary'; else printf 'bin/%s' "$1"; fi
}
is_installer_owned_link() {
  local path="$1" relative="$2" linked legacy_root legacy_name old_version old_revision receipt
  [[ -L "$path" ]] || return 1
  linked="$(readlink "$path")" || return 1
  [[ "$linked" == "$current_release/$relative" ]] && return 0
  # Require the exact old path shape: no /../, alternate traversal, or
  # relative user-managed link can be upgraded automatically.
  case "$linked" in "$releases/secretary-v"*"-$arch/$relative") ;; *) return 1 ;; esac
  legacy_root="${linked%/$relative}"
  legacy_name="${legacy_root##*/}"
  [[ "$legacy_root" == "$releases/$legacy_name" ]] || return 1
  [[ -d "$legacy_root" && ! -L "$legacy_root" && ! -L "$legacy_root/bin" ]] || return 1
  [[ -f "$legacy_root/VERSION" && ! -L "$legacy_root/VERSION" &&
     -f "$legacy_root/REVISION" && ! -L "$legacy_root/REVISION" &&
     -f "$legacy_root/$relative" && ! -L "$legacy_root/$relative" ]] || return 1
  receipt="$legacy_root/RELEASE-MANIFEST.json"
  [[ -f "$receipt" && ! -L "$receipt" ]] || return 1
  old_version="$(cat "$legacy_root/VERSION")" || return 1
  old_revision="$(cat "$legacy_root/REVISION")" || return 1
  [[ "$old_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || return 1
  [[ "$old_revision" =~ ^[[:xdigit:]]{40}$ ]] || return 1
  [[ "$legacy_name" == "secretary-v${old_version}-darwin-$arch" ]] || return 1
  [[ "$(plutil -extract version raw -o - "$receipt" 2>/dev/null)" == "$old_version" &&
     "$(plutil -extract revision raw -o - "$receipt" 2>/dev/null)" == "$old_revision" &&
     "$(plutil -extract os raw -o - "$receipt" 2>/dev/null)" == darwin &&
     "$(plutil -extract arch raw -o - "$receipt" 2>/dev/null)" == "$arch" &&
     "$(plutil -extract archive raw -o - "$receipt" 2>/dev/null)" == "$legacy_name.tar.gz" ]]
}
# A foreign public executable still forces *all* release links to the private
# bin. In that mode, leave every public file/symlink exactly as it was.
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$public_bin/$executable"
  if [[ -e "$path" || -L "$path" ]]; then
    if ! is_installer_owned_link "$path" "$(release_link_relative "$executable")"; then
      bin="$root/bin"
      break
    fi
  fi
done
if [[ "$bin" != "$public_bin" ]]; then
  [[ ! -L "$bin" ]] || fail "Unsafe private release binary directory"
  mkdir -p "$bin"
fi
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  if [[ -e "$path" || -L "$path" ]]; then
    is_installer_owned_link "$path" "$(release_link_relative "$executable")" || fail "Conflicting release executable: $path"
  fi
done
# Keep an existing source-managed app bundle intact as well as its CLI.
if [[ "$bin" != "$public_bin" && ( -e "$app" || -L "$app" ) ]]; then
  app="$HOME/Applications/Secretary Node (Release).app"
fi
legacy="$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
legacy_node_agent=false
if [[ -e "$legacy" || -L "$legacy" ]]; then
  legacy_node_agent=true
fi
[[ ! -e "$app" || -d "$app" ]] || fail "Existing app location is not a directory"
if [[ -d "$app" ]]; then
  existing_id="$(plutil -extract CFBundleIdentifier raw -o - "$app/Contents/Info.plist" 2>/dev/null || true)"
  new_id="$(plutil -extract CFBundleIdentifier raw -o - "$target/Secretary Node.app/Contents/Info.plist")"
  [[ "$existing_id" == "$new_id" ]] || fail "Another application occupies $app"
fi

app_stage="$stage/new-node.app"
cp -R "$target/Secretary Node.app" "$app_stage"
codesign --verify "$app_stage" || fail "Staged Node.app did not verify"
mkdir -p "$stage/setup-bin"
for executable in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  ln -s "$target/bin/$executable" "$stage/setup-bin/$executable"
done
SECRETARY_BINARY_PREINSTALLED_ONLY=1 SECRETARY_BINARY_STAGE_BIN="$stage/setup-bin" "$target/secretary" setup --non-interactive || fail "Private setup failed; active release has not changed"

backup="$stage/previous-app"
had_app=false
[[ ! -d "$app" ]] || had_app=true
activated=false
created_link_names=""
pointer_stage="$HOME/.local/share/secretary/.current.$.new"
# Retain the previous symlink itself before each swap. Do not reconstruct
# symlinks from arrays during an EXIT trap (macOS ships Bash 3.2): each
# rollback entry is a durable original target until activation succeeds.
link_stage="$(mktemp -d "$bin/.secretary-links.XXXXXXXX")" || fail "Cannot stage release links"
activation_rollback() {
  if [[ "$activated" != true ]]; then
    if [[ -L "$current_release" && "$(readlink "$current_release")" == "$target" ]]; then
      rm -f "$current_release"
      if [[ -n "$previous_release" ]]; then ln -s "$previous_release" "$current_release"; fi
    fi
    # Do not expand an empty Bash array under `set -u`: macOS Bash 3.2
    # aborts the EXIT trap before previous release links are restored.
    for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
      case " $created_link_names " in
        *" $executable "*) rm -f "$bin/$executable" ;;
      esac
    done
    for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
      # Every backup is made before the corresponding link is replaced.
      # A previous migration can be restored even if a later mv fails.
      old_link="$link_stage/old.$executable"
      if [[ -L "$old_link" ]]; then
        if ! mv -fh "$old_link" "$bin/$executable"; then
          printf 'WARNING: Cannot restore legacy release link %s\n' "$bin/$executable" >&2
        fi
      fi
    done
    if [[ -d "$backup" ]]; then
      rm -rf "$app"
      mv "$backup" "$app"
    elif [[ "$had_app" == false ]]; then
      rm -rf "$app"
    fi
    rm -f "$pointer_stage"
  fi
}
trap 'activation_rollback; rm -rf "$link_stage" "$stage"' EXIT
if [[ -d "$app" ]]; then mv "$app" "$backup"; fi
mv "$app_stage" "$app" || fail "Cannot activate native Node.app"
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  dest="$current_release/$(release_link_relative "$executable")"
  if [[ ! -e "$path" && ! -L "$path" ]]; then
    ln -s "$dest" "$path" || fail "Cannot activate $executable"
    created_link_names="$created_link_names $executable"
  elif [[ "$(readlink "$path")" != "$dest" ]]; then
    is_installer_owned_link "$path" "$(release_link_relative "$executable")" ||
      fail "Release executable changed ownership during setup: $path"
    previous_link_target="$(readlink "$path")"
    # Save a physical copy of the legacy symlink before touching the public
    # path. Rollback moves this copy back; it never has to infer provenance.
    ln -s "$previous_link_target" "$link_stage/old.$executable" ||
      fail "Cannot back up owned release link: $executable"
    replacement="$link_stage/$executable"
    ln -s "$dest" "$replacement" || fail "Cannot stage owned link migration: $executable"
    mv -fh "$replacement" "$path" || fail "Cannot migrate owned release link: $executable"
  fi
done
ln -s "$target" "$pointer_stage" || fail "Cannot stage release switch"
mv -fh "$pointer_stage" "$current_release" || fail "Cannot activate release pointer"
activated=true
if [[ -n "$previous_release" && "$previous_release" != "$target" ]]; then
  printf 'NOTICE: macOS release upgraded on disk. Existing Server/Node processes are NOT restarted automatically (running Workers may be active). At a safe boundary, restart the managed release Server and quit/relaunch the menu-managed Secretary Node app. Verify their live revisions; installed status alone is not proof that a running process upgraded.\n' >&2
  if [[ -f "$root/server.pid" ]]; then
    old_pid="$(cat "$root/server.pid")"
    if [[ "$old_pid" =~ ^[1-9][0-9]*$ ]] && kill -0 "$old_pid" 2>/dev/null; then
      printf 'NOTICE: Server PID %s may still execute the previous release. Restart explicitly using the release CLI after existing work finishes.\n' "$old_pid" >&2
    fi
  fi
fi
printf 'Installed Secretary %s (%s) for macOS %s. Launch Secretary Node from Spotlight.\n' "$version" "$revision" "$arch"
if [[ "$bin" != "$public_bin" ]]; then
  printf 'Existing executables in %s preserved. Source-managed CLI remains unchanged.\n' "$public_bin"
  printf 'Release CLI: SECRETARY_BINARY_STAGE_BIN=%q %q status\n' "$bin" "$current_release/secretary"
  printf 'Start release Server: SECRETARY_BINARY_STAGE_BIN=%q %q start --no-open\n' "$bin" "$current_release/secretary"
else
  printf 'Start Server: secretary start --no-open\n'
fi
if [[ "$app" != "$HOME/Applications/Secretary Node.app" ]]; then
  printf 'Existing Secretary Node.app preserved. Release app: %s. Open explicitly: open %q\n' "$app" "$app"
fi
if [[ "$legacy_node_agent" == true ]]; then
  printf 'WARNING: Preserved existing CLI Node LaunchAgent at %s. Stop the source-managed Node before launching the release menu-managed Node; the installer did not stop or change the old agent.\n' "$legacy" >&2
fi
