#!/usr/bin/env bash
# Release-builder only: compile the maintained ACP adapter to a standalone
# binary. The owner never needs Bun, Node.js, npm or a global ACP installation.
set -euo pipefail
[[ $# -eq 2 ]] || { echo "Usage: $0 PLATFORM_ARCH OUTPUT_DIRECTORY" >&2; exit 2; }
platform="$1"; out="$2"
case "$platform" in
 linux-amd64) bun_target=bun-linux-x64-baseline ;;
 linux-arm64) bun_target=bun-linux-arm64 ;;
 darwin-arm64) bun_target=bun-darwin-arm64 ;;
 darwin-amd64) bun_target=bun-darwin-x64-baseline ;;
 *) echo "Unsupported packaged native ACP target: $platform" >&2; exit 2 ;;
esac
command -v bun >/dev/null || { echo "Release CI must install pinned Bun" >&2; exit 1; }
command -v npm >/dev/null || { echo "Release CI must install Node for the build only" >&2; exit 1; }
repo=https://github.com/agentclientprotocol/codex-acp.git
commit=202e66e63343ec4f5a5e35ab590251d813d129ca  # upstream v2.2.2
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
git init -q "$tmp/source"
git -C "$tmp/source" remote add origin "$repo"
git -C "$tmp/source" fetch -q --depth=1 origin "$commit"
[[ "$(git -C "$tmp/source" rev-parse FETCH_HEAD)" == "$commit" ]] || { echo "ACP source commit mismatch" >&2; exit 1; }
git -C "$tmp/source" checkout -q --detach FETCH_HEAD
(
 cd "$tmp/source"
 [[ "$(node -p 'require("./package.json").version')" == 2.2.2 ]] || exit 1
 npm ci --ignore-scripts --no-audit --no-fund
 bun build ./src/index.ts --minify --compile "--target=$bun_target" --outfile "$tmp/codex-acp-2.2.2"
)
"$tmp/codex-acp-2.2.2" --version | grep -F '2.2.2'
mkdir -p "$out"
tar -czf "$out/codex-acp.tar.gz" -C "$tmp" codex-acp-2.2.2
if command -v sha256sum >/dev/null; then
 (cd "$out" && sha256sum codex-acp.tar.gz > codex-acp.tar.gz.sha256)
else
 (cd "$out" && shasum -a 256 codex-acp.tar.gz > codex-acp.tar.gz.sha256)
fi
