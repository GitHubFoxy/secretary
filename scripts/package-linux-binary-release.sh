#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Secretary Linux package: %s\n' "$*" >&2; exit 1; }
[[ $# -eq 5 ]] || fail "Usage: $0 VERSION REVISION ARCH INPUT_DIRECTORY OUTPUT_DIRECTORY"
version="$1"; revision="$2"; arch="$3"; input="$4"; output="$5"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid version"
[[ "$revision" =~ ^[[:xdigit:]]{40}$ ]] || fail "Invalid revision"
case "$arch" in amd64|arm64) ;; *) fail "Unsupported architecture: $arch" ;; esac
[[ -d "$input" && ! -L "$input" ]] || fail "Unsafe package input"
[[ ! -e "$output" || ( -d "$output" && ! -L "$output" ) ]] || fail "Unsafe output"
for binary in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ -x "$input/bin/$binary" && ! -L "$input/bin/$binary" ]] || fail "Missing binary: $binary"
done
[[ -f "$input/secretary" && -f "$input/scripts/secretary-interactive.zsh" && -f "$input/internal/config/defaults/config.toml" ]] || fail "Missing release resources"
for tool in ffmpeg ffprobe; do
  [[ -x "$input/bin/video-tools/$tool" && ! -L "$input/bin/video-tools/$tool" ]] || fail "Missing Server video executable: $tool"
done
video_ffmpeg="$("$input/bin/video-tools/ffmpeg" -version | head -n 1 | awk '{print $3}')"
video_ffprobe="$("$input/bin/video-tools/ffprobe" -version | head -n 1 | awk '{print $3}')"
[[ -n "$video_ffmpeg" && "$video_ffmpeg" == "$video_ffprobe" ]] || fail "Incompatible FFmpeg/FFprobe versions"
mkdir -p "$output"
name="secretary-v$version-linux-$arch"
[[ ! -e "$output/$name.tar.gz" && ! -e "$output/$name.json" ]] || fail "Release already exists"
stage="$(mktemp -d "$output/.linux-release.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/$name/bin" "$stage/$name/scripts" "$stage/$name/internal/config"
for binary in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  cp -p "$input/bin/$binary" "$stage/$name/bin/$binary"
done
mkdir -p "$stage/$name/bin/video-tools"
cp -p "$input/bin/video-tools/ffmpeg" "$stage/$name/bin/video-tools/ffmpeg"
cp -p "$input/bin/video-tools/ffprobe" "$stage/$name/bin/video-tools/ffprobe"
cp -p "$input/secretary" "$stage/$name/secretary"
cp -p "$input/scripts/secretary-interactive.zsh" "$stage/$name/scripts/"
cp -R "$input/internal/config/defaults" "$stage/$name/internal/config/"
printf '%s\n' "$version" > "$stage/$name/VERSION"
printf '%s\n' "$revision" > "$stage/$name/REVISION"
python3 - "$stage/$name" "$video_ffmpeg" <<'PY'
import hashlib, json, pathlib, sys
root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
entries = {}
for tool in ("ffmpeg", "ffprobe"):
    entries[tool] = {"version": version, "sha256": hashlib.sha256((root / "bin" / "video-tools" / tool).read_bytes()).hexdigest()}
(root / "VIDEO-TOOLS.json").write_text(json.dumps({"origin": "Secretary CI release packaging; build-host OS distribution", **entries}, sort_keys=True) + "\n")
PY
tar -czf "$stage/$name.tar.gz" -C "$stage" "$name"
digest="$(sha256sum "$stage/$name.tar.gz" | awk '{print $1}')"
bytes="$(wc -c < "$stage/$name.tar.gz" | tr -d '[:space:]')"
printf '{"schema":1,"version":"%s","revision":"%s","os":"linux","arch":"%s","archive":"%s.tar.gz","sha256":"%s","bytes":%s}\n' "$version" "$revision" "$arch" "$name" "$digest" "$bytes" > "$stage/$name.json"
mv "$stage/$name.tar.gz" "$output/$name.tar.gz"
mv "$stage/$name.json" "$output/$name.json"
