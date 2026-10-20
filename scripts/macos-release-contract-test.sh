#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
input="$work/input"
output="$work/output"
mkdir -p "$input/bin" "$input/scripts" "$input/internal/config/defaults" "$input/Secretary Node.app/Contents/MacOS"
bash "$root/scripts/create-native-acp-fixture.sh" "$input"
for name in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  printf '#!/bin/sh\nexit 0\n' > "$input/bin/$name"
  chmod +x "$input/bin/$name"
done
mkdir -p "$input/bin/video-tools"
for tool in ffmpeg ffprobe; do
  printf '#!/bin/sh\nif [ "$1" = "-hide_banner" ] && [ "$2" = "-encoders" ]; then echo " V..... libwebp WebP encoder"; else echo "%s version 6.1"; fi\n' "$tool" > "$input/bin/video-tools/$tool"
  chmod +x "$input/bin/video-tools/$tool"
done
printf '#!/bin/sh\nexit 0\n' > "$input/secretary"
printf 'exit 0\n' > "$input/scripts/secretary-interactive.zsh"
printf '[secretary]\n' > "$input/internal/config/defaults/config.toml"
printf 'profile\n' > "$input/internal/config/defaults/secretary.md"
printf '#!/bin/sh\nexit 0\n' > "$input/Secretary Node.app/Contents/MacOS/SecretaryNode"
printf '<plist/>\n' > "$input/Secretary Node.app/Contents/Info.plist"
chmod +x "$input/Secretary Node.app/Contents/MacOS/SecretaryNode"
revision=0123456789abcdef0123456789abcdef01234567
bash "$root/scripts/package-macos-binary-release.sh" 1.2.3 "$revision" arm64 "$input" "$output" >/dev/null
manifest="$output/secretary-v1.2.3-darwin-arm64.json"
archive="$output/secretary-v1.2.3-darwin-arm64.tar.gz"
[[ -f "$manifest" && -f "$archive" ]]
grep -q '"schema":1' "$manifest"
tar -xOf "$archive" secretary-v1.2.3-darwin-arm64/VIDEO-TOOLS.json | grep -q '"required_encoder": "libwebp"'
# A binary lacking the encoder must not produce a release.
cp "$input/bin/video-tools/ffmpeg" "$work/working-ffmpeg"
printf '#!/bin/sh\necho "ffmpeg version 6.1"\n' > "$input/bin/video-tools/ffmpeg"
if bash "$root/scripts/package-macos-binary-release.sh" 1.2.5 "$revision" arm64 "$input" "$work/no-webp" >/dev/null 2>&1; then
  echo "Release packaging accepted FFmpeg without libwebp" >&2; exit 1
fi
cp "$work/working-ffmpeg" "$input/bin/video-tools/ffmpeg"
grep -q "\"revision\":\"$revision\"" "$manifest"
expected="$(shasum -a 256 "$archive" | awk '{print $1}')"
grep -q "\"sha256\":\"$expected\"" "$manifest"
tar -tzf "$archive" | grep -q 'secretary-v1.2.3-darwin-arm64/Secretary Node.app/Contents/MacOS/SecretaryNode'
tar -xOf "$archive" secretary-v1.2.3-darwin-arm64/VERSION | grep -qx 1.2.3
if bash "$root/scripts/package-macos-binary-release.sh" 1.2.3 "$revision" arm64 "$input" "$output" >/dev/null 2>&1; then
  echo "Packaging silently overwrote a published release" >&2
  exit 1
fi
if bash "$root/scripts/package-macos-binary-release.sh" 1.2.3 "$revision" ppc64 "$input" "$work/unsupported" >/dev/null 2>&1; then
  echo "Unsupported platform accepted" >&2
  exit 1
fi
rm "$input/bin/secretary-node"
if bash "$root/scripts/package-macos-binary-release.sh" 1.2.4 "$revision" arm64 "$input" "$work/incomplete" >/dev/null 2>&1; then
  echo "Missing Node binary accepted" >&2
  exit 1
fi
echo "macOS release artifact contract tests passed"
