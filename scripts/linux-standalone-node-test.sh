#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 3 ]] || { echo "Usage: $0 PACKAGE_INPUT DIST REVISION" >&2; exit 1; }
input="$1"; dist="$2"; revision="$3"
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; *) exit 1 ;; esac
root="$(cd "$(dirname "$0")/.." && pwd)"
home="$(mktemp -d)"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/mock" "$home/run" "$home/provider"
python3 - "$home/run/bus" <<'PY'
import socket, sys
sock = socket.socket(socket.AF_UNIX)
sock.bind(sys.argv[1])
sock.close()
PY
cat > "$home/mock/systemctl" <<'SH'
#!/usr/bin/env bash
case "$*" in
  "--user show-environment"|"--user daemon-reload"|"--user enable --now secretary-standalone-node.service"|"--user disable --now secretary-standalone-node.service"|"--user restart secretary-standalone-node.service"|"--user start secretary-standalone-node.service"|"--user stop secretary-standalone-node.service") exit 0 ;;
  "--user show secretary-standalone-node.service -p MainPID --value") echo 0 ;;
  *) exit 1 ;;
esac
SH
cat > "$home/mock/claude" <<'SH'
#!/bin/sh
[ "$1" = "--version" ] && echo fixture
exit 0
SH
for command in go npm node; do
  cat > "$home/mock/$command" <<'SH'
#!/bin/sh
echo "Forbidden build tool invoked" >&2
exit 97
SH
done
chmod +x "$home/mock/"*
export HOME="$home" PATH="$home/mock:$home/provider:$PATH" XDG_RUNTIME_DIR="$home/run"
export SECRETARY_RELEASE_BASE="file://$dist" SECRETARY_VERSION=0.0.0-ci SECRETARY_NODE_PAIRING_TOKEN=owner-approved-test-token
installer="$root/scripts/install-linux-standalone-node.sh"
bash "$installer" --server https://secretary.example.test --name remote-ci --harness claude_code
state="$HOME/.local/share/secretary"
[[ -L "$state/standalone-node/current" ]]
[[ -f "$state/node/config.json" && -f "$state/node/enrollment.env" ]]
[[ ! -f "$state/secretary.db" && ! -f "$state/environment" && ! -f "$state/config.toml" ]]
[[ ! -f "$HOME/.config/systemd/user/secretary.service" ]]
[[ ! -e "$state/standalone-node/current/bin/secretaryd" ]]
grep -Fq '"standalone": true' "$state/node/config.json" || grep -Fq '"standalone":true' "$state/node/config.json"
grep -Fq '"server_url": "https://secretary.example.test"' "$state/node/config.json" || grep -Fq '"server_url":"https://secretary.example.test"' "$state/node/config.json"
mkdir -p "$state/node/data"
printf 'native-session\n' > "$state/node/data/session-canary"
printf '{"node":"remote-ci"}\n' > "$state/node/data/identity.json"
cp "$state/node/config.json" "$home/config.before"
cp "$state/node/enrollment.env" "$home/enrollment.before"
bash "$installer" --harness claude_code
cmp "$home/config.before" "$state/node/config.json"
cmp "$home/enrollment.before" "$state/node/enrollment.env"
[[ "$(cat "$state/node/data/session-canary")" == native-session ]]
bash "$installer" enable-service
unit="$HOME/.config/systemd/user/secretary-standalone-node.service"
grep -Fq 'Description=Secretary standalone Execution Node' "$unit"
grep -Fq 'EnvironmentFile=-' "$unit"
grep -Fq 'ExecStart=' "$unit"
[[ ! -f "$HOME/.config/systemd/user/secretary.service" ]]
bash "$installer" status | grep -F 'installed: version=0.0.0-ci'
bash "$installer" doctor --harness claude_code
bash "$root/scripts/package-linux-binary-release.sh" 0.0.1-ci "$revision" "$arch" "$input" "$dist"
export SECRETARY_VERSION=0.0.1-ci
archive="$dist/secretary-v0.0.1-ci-linux-$arch.tar.gz"
cp "$archive" "$home/archive.before"
printf 'corruption\n' >> "$archive"
if bash "$installer" --harness claude_code 2>/dev/null; then echo "Corrupt archive accepted" >&2; exit 1; fi
[[ "$(readlink "$state/standalone-node/current")" == *0.0.0-ci* ]]
cp "$home/archive.before" "$archive"
bash "$installer" --harness claude_code
[[ "$(readlink "$state/standalone-node/current")" == *0.0.1-ci* ]]
cmp "$home/config.before" "$state/node/config.json"
cmp "$home/enrollment.before" "$state/node/enrollment.env"
[[ "$(cat "$state/node/data/session-canary")" == native-session ]]
bash "$installer" disable-service
[[ ! -f "$unit" ]]
bash "$installer" --server https://secretary.example.test --name remote-ci --harness claude_code 2>/dev/null && { echo "Paired identity rewrite accepted" >&2; exit 1; }
[[ "$(cat "$state/node/data/session-canary")" == native-session ]]
echo 'Standalone Linux Node fixture completed'
