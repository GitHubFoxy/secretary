#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf '%s\n' "$*" >&2; exit 1; }

[[ $# -eq 5 ]] || fail "Usage: $0 VERSION REVISION ARCH INPUT_DIRECTORY OUTPUT_DIRECTORY"
version="$1"
revision="$2"
arch="$3"
input="$4"
output="$5"

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid release version"
[[ "$revision" =~ ^[[:xdigit:]]{40}$ ]] || fail "Revision must be a full git SHA"
case "$arch" in arm64|amd64) ;; *) fail "Unsupported macOS architecture: $arch" ;; esac
[[ -d "$input" && ! -L "$input" ]] || fail "Input release directory must exist and not be a symlink"
[[ ! -e "$output" || ( -d "$output" && ! -L "$output" ) ]] || fail "Output directory must not be a symlink"
for name in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ -f "$input/bin/$name" && -x "$input/bin/$name" && ! -L "$input/bin/$name" ]] || fail "Missing executable: bin/$name"
done
[[ -f "$input/secretary" && ! -L "$input/secretary" ]] || fail "Missing Secretary CLI"
[[ -f "$input/scripts/secretary-interactive.zsh" ]] || fail "Missing CLI interactive support"
[[ -f "$input/internal/config/defaults/config.toml" ]] || fail "Missing default configuration"
[[ -f "$input/Secretary Node.app/Contents/Info.plist" && -x "$input/Secretary Node.app/Contents/MacOS/SecretaryNode" ]] || fail "Missing native Secretary Node.app"
[[ -d "$input/internal/config/defaults" ]] || fail "Missing default profiles"
for tool in ffmpeg ffprobe; do
  [[ -x "$input/bin/video-tools/$tool" && ! -L "$input/bin/video-tools/$tool" ]] || fail "Missing Server video executable: $tool"
done
video_ffmpeg="$("$input/bin/video-tools/ffmpeg" -version | head -n 1 | awk '{print $3}')"
video_ffprobe="$("$input/bin/video-tools/ffprobe" -version | head -n 1 | awk '{print $3}')"
[[ -n "$video_ffmpeg" && "$video_ffmpeg" == "$video_ffprobe" ]] || fail "Incompatible FFmpeg/FFprobe versions"
"$input/bin/video-tools/ffmpeg" -hide_banner -encoders | awk '$2=="libwebp" && substr($1,1,1)=="V" {found=1} END {exit !found}' || fail "Packaged FFmpeg lacks libwebp encoder"
mkdir -p "$output"
release="secretary-v$version-darwin-$arch"
archive="$output/$release.tar.gz"
manifest="$output/$release.json"
[[ ! -e "$archive" && ! -e "$manifest" ]] || fail "Release output already exists; refusing to overwrite"
stage="$(mktemp -d "$output/.release.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/$release/bin" "$stage/$release/scripts" "$stage/$release/internal/config"
for name in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  cp -p "$input/bin/$name" "$stage/$release/bin/$name"
done
mkdir -p "$stage/$release/bin/video-tools"
cp -p "$input/bin/video-tools/ffmpeg" "$stage/$release/bin/video-tools/ffmpeg"
cp -p "$input/bin/video-tools/ffprobe" "$stage/$release/bin/video-tools/ffprobe"
cp -p "$input/secretary" "$stage/$release/secretary"
cp -p "$input/scripts/secretary-interactive.zsh" "$stage/$release/scripts/secretary-interactive.zsh"
cp -R "$input/internal/config/defaults" "$stage/$release/internal/config/defaults"
cp -R "$input/Secretary Node.app" "$stage/$release/Secretary Node.app"
printf '%s\n' "$version" > "$stage/$release/VERSION"
printf '%s\n' "$revision" > "$stage/$release/REVISION"
python3 - "$stage/$release" "$video_ffmpeg" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
entries = {}
for tool in ("ffmpeg", "ffprobe"):
    entries[tool] = {"version": version, "sha256": hashlib.sha256((root / "bin" / "video-tools" / tool).read_bytes()).hexdigest()}
(root / "VIDEO-TOOLS.json").write_text(json.dumps({"origin": "https://github.com/eugeneware/ffmpeg-static/releases/tag/b6.1.1", "required_encoder": "libwebp", **entries}, sort_keys=True) + "\n")
PY
tar -czf "$stage/$release.tar.gz" -C "$stage" "$release"
digest="$(shasum -a 256 "$stage/$release.tar.gz" | awk '{print $1}')"
bytes="$(wc -c < "$stage/$release.tar.gz" | tr -d '[:space:]')"
printf '{"schema":1,"version":"%s","revision":"%s","os":"darwin","arch":"%s","archive":"%s.tar.gz","sha256":"%s","bytes":%s}\n' "$version" "$revision" "$arch" "$release" "$digest" "$bytes" > "$stage/$release.json"
mv "$stage/$release.tar.gz" "$archive"
mv "$stage/$release.json" "$manifest"
printf '%s\n' "$manifest" "$archive"
