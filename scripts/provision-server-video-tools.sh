#!/usr/bin/env bash
set -euo pipefail
umask 077

[[ $# -eq 1 ]] || { echo "Usage: $0 OUTPUT_DIRECTORY" >&2; exit 1; }
destination="$1"
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) platform=darwin-arm64 ;;
  Darwin:x86_64) platform=darwin-x64 ;;
  Linux:x86_64) platform=linux-x64 ;;
  Linux:aarch64) platform=linux-arm64 ;;
  *) echo "Unsupported Server video platform" >&2; exit 1 ;;
esac

case "$platform" in
  darwin-arm64) ffmpeg_hash=8923876afa8db5585022d7860ec7e589af192f441c56793971276d450ed3bbfa; ffprobe_hash=d986a8ec7b030899fe66a8a288ed809a3543338705a3ce178cfb85869c5d80be ;;
  darwin-x64) ffmpeg_hash=929b375c1182d956c51f7ac25e0b2b0411fb01f6f407aa15c9758efeb4242106; ffprobe_hash=d4da574d6e2e197bd259b47d69cf262df9e312af24ad960444f6d806d3d4c186 ;;
  linux-arm64) ffmpeg_hash=754a678672298bc68156adff58aa7385a592c2b30b1d0ae8750c45c915c4bac0; ffprobe_hash=2ab6aba60ee84412dff9188720703376cb4e7aaf7e0b5e43aa8249f2acae5bf8 ;;
  linux-x64) ffmpeg_hash=bfe8a8fc511530457b528c48d77b5737527b504a3797a9bc4866aeca69c2dffa; ffprobe_hash=25d9b6ccb05e3d9de9e04e31e2506d8dd7f9f0418981965ac6df12e8d3afd067 ;;
esac

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/bin"
for tool in ffmpeg ffprobe; do
  hash="$ffmpeg_hash"
  [[ "$tool" == ffprobe ]] && hash="$ffprobe_hash"
  curl -fsSL --retry 3 "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.1.1/$tool-$platform.gz" -o "$stage/$tool.gz"
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "$hash" "$stage/$tool.gz" | sha256sum -c -
  else
    [[ "$(shasum -a 256 "$stage/$tool.gz" | awk '{print $1}')" == "$hash" ]]
  fi
  gzip -dc "$stage/$tool.gz" > "$stage/bin/$tool"
  chmod 755 "$stage/bin/$tool"
done

ffmpeg_version="$("$stage/bin/ffmpeg" -version | head -n 1 | awk '{print $3}')"
ffprobe_version="$("$stage/bin/ffprobe" -version | head -n 1 | awk '{print $3}')"
[[ "$ffmpeg_version" == "$ffprobe_version" && -n "$ffmpeg_version" ]] || { echo "FFmpeg/FFprobe versions differ" >&2; exit 1; }
"$stage/bin/ffmpeg" -hide_banner -encoders | awk '$2=="libwebp" && substr($1,1,1)=="V" {found=1} END {exit !found}' || { echo "Pinned FFmpeg lacks required libwebp encoder" >&2; exit 1; }
[[ ! -e "$destination" ]] || { echo "Destination already exists" >&2; exit 1; }
mv "$stage/bin" "$destination"
