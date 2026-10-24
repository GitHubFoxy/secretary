#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
# Source the real CLI functions, excluding the top-level command dispatcher.
sed '/^case "${1:-}" in/,$d' "$root/secretary" > "$work/functions.zsh"
mkdir -p "$work/release/bin/video-tools" "$work/state" "$work/bin" "$work/scripts"
cp "$root/scripts/secretary-interactive.zsh" "$work/scripts/secretary-interactive.zsh"
printf '1.0\n' > "$work/release/VERSION"
printf 'abcdef\n' > "$work/release/REVISION"
: > "$work/state/config.toml"
: > "$work/state/environment"
for name in secretaryd secretary-mcp; do printf '#!/bin/sh\nexit 0\n' > "$work/bin/$name"; chmod +x "$work/bin/$name"; done
cat > "$work/bin/secretary-node" <<'EOF'
#!/bin/sh
[ "$#" -eq 2 ] && [ "$2" = "--codex-pair-scope=server" ] || exit 1
case "$1" in
  --codex-pair-action=readiness) exit 0 ;;
  --codex-pair-action=doctor) echo "Codex fixture, ACP fixture, code-mode host fixture: ready"; exit 0 ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$work/bin/secretary-node"
for name in ffmpeg ffprobe; do
  printf '#!/bin/sh\nif [ "$1" = "-hide_banner" ]; then echo " V..... libwebp WebP encoder"; else echo "%s version 6.1"; fi\n' "$name" > "$work/release/bin/video-tools/$name"
  chmod +x "$work/release/bin/video-tools/$name"
done
manifest() {
  python3 - "$work/release" <<'PY'
import hashlib,json,pathlib,sys
r=pathlib.Path(sys.argv[1])
m={"origin":"test-release","required_encoder":"libwebp"}
for name in ("ffmpeg","ffprobe"):
    data=(r/"bin"/"video-tools"/name).read_bytes()
    m[name]={"version":"6.1","sha256":hashlib.sha256(data).hexdigest()}
(r/"VIDEO-TOOLS.json").write_text(json.dumps(m))
PY
}
check() {
  local expected="$1" label="$2" output
  if output="$(zsh -c '
    source "$1"
    ROOT="$2/release"
    STATE="$2/state"
    ENV_FILE="$STATE/environment"
    BIN="$2/bin"
    NODE_BIN="$BIN/secretary-node"
    configured_harness() { print codex; }
    native_harness_ready() { return 0; }
    doctor
  ' _ "$work/functions.zsh" "$work" 2>&1)"; then
    [[ "$expected" == pass ]] || { echo "$label unexpectedly passed: $output" >&2; exit 1; }
    [[ "$output" == *"Doctor found no problems."* ]] || exit 1
  else
    [[ "$expected" == fail ]] || { echo "$label unexpectedly failed: $output" >&2; exit 1; }
    [[ "$output" != *"Doctor found no problems."* ]] || { echo "$label false success" >&2; exit 1; }
  fi
}
manifest
check pass healthy
printf '\n# tampered but identical version\n' >> "$work/release/bin/video-tools/ffmpeg"
check fail tampered
manifest
printf '#!/bin/sh\necho "ffmpeg version 6.1"\n' > "$work/release/bin/video-tools/ffmpeg"
chmod +x "$work/release/bin/video-tools/ffmpeg"
manifest
check fail missing-webp
printf '#!/bin/sh\nif [ "$1" = "-hide_banner" ]; then echo " V..... libwebp WebP encoder"; else echo "ffmpeg version 6.1"; fi\n' > "$work/release/bin/video-tools/ffmpeg"
chmod +x "$work/release/bin/video-tools/ffmpeg"
manifest
rm "$work/release/bin/video-tools/ffprobe"
check fail missing-ffprobe
echo "Server video doctor contract checks passed"
