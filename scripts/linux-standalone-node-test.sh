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
  "--user restart secretary-standalone-node.service") printf 'restart %s\n' "$(readlink "$HOME/.local/share/secretary/standalone-node/current")" >> "$HOME/restarts.log"; exit 0 ;;
  "--user show-environment"|"--user daemon-reload"|"--user enable --now secretary-standalone-node.service"|"--user disable --now secretary-standalone-node.service"|"--user start secretary-standalone-node.service"|"--user stop secretary-standalone-node.service") exit 0 ;;
  "--user show secretary-standalone-node.service -p MainPID --value")
    if [[ "${TEST_ROLLBACK_HEALTH:-}" == true ]]; then echo "$TEST_SERVICE_PID"; else echo 0; fi ;;
  "--user show secretary-standalone-node.service -p ActiveState --value")
    if [[ "${TEST_ROLLBACK_HEALTH:-}" == true ]]; then echo active; else echo failed; fi ;;
  *) exit 1 ;;
esac
SH
cat > "$home/mock/readlink" <<'SH'
#!/usr/bin/env bash
if [[ "${TEST_ROLLBACK_HEALTH:-}" == true && "$1" == -f && "$2" == /proc/*/exe ]]; then
  current="$(/usr/bin/readlink "$HOME/.local/share/secretary/standalone-node/current")"
  if [[ "$current" == *0.0.0-ci* ]]; then printf '%s/bin/secretary-node\n' "$current"; else printf '/usr/bin/false\n'; fi
else
  exec /usr/bin/readlink "$@"
fi
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
pin() { SECRETARY_MANIFEST_SHA256="$(sha256sum "$dist/secretary-v${SECRETARY_VERSION}-linux-$arch.json" | cut -d' ' -f1)"; export SECRETARY_MANIFEST_SHA256; }
pin
installer="$root/scripts/install-linux-standalone-node.sh"
bash "$installer" --server https://secretary.example.test --name remote-ci --harness claude_code
state="$HOME/.local/share/secretary/standalone-node"
[[ -L "$state/current" ]]
[[ "$(readlink "$state/current")" == "$state/releases/secretary-v0.0.0-ci-linux-$arch" ]]
[[ -x "$state/current/bin/secretary-node" ]]
[[ -f "$state/node/config.json" && -f "$state/node/enrollment.env" ]]
[[ ! -f "$HOME/.local/share/secretary/secretary.db" && ! -f "$HOME/.local/share/secretary/environment" && ! -f "$HOME/.local/share/secretary/config.toml" ]]
[[ ! -f "$HOME/.config/systemd/user/secretary.service" ]]
[[ ! -e "$state/current/bin/secretaryd" ]]
grep -Fq '"standalone": true' "$state/node/config.json" || grep -Fq '"standalone":true' "$state/node/config.json"
grep -Fq '"server_url": "https://secretary.example.test"' "$state/node/config.json" || grep -Fq '"server_url":"https://secretary.example.test"' "$state/node/config.json"
mkdir -p "$state/node/data"
printf 'native-session\n' > "$state/node/data/session-canary"
printf '{"node":"remote-ci"}\n' > "$state/node/data/identity.json"
cp "$state/node/config.json" "$home/config.before"
cp "$state/node/enrollment.env" "$home/enrollment.before"
if bash "$installer" --harness codex > /dev/null 2>&1; then echo "Harness conflict accepted" >&2; exit 1; fi
bash "$installer" --harness claude_code
cmp "$home/config.before" "$state/node/config.json"
cmp "$home/enrollment.before" "$state/node/enrollment.env"
[[ "$(cat "$state/node/data/session-canary")" == native-session ]]
unit="$HOME/.config/systemd/user/secretary-standalone-node.service"
mkdir -p "$(dirname "$unit")"
printf '[Unit]\nDescription=Secretary standalone Execution Node\n[Service]\nExecStart=/bin/true\n' > "$unit"
if bash "$installer" enable-service >/dev/null 2>&1; then echo "Foreign unit accepted" >&2; exit 1; fi
[[ -f "$unit" ]]
rm "$unit"
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
pin
archive="$dist/secretary-v0.0.1-ci-linux-$arch.tar.gz"
cp "$archive" "$home/archive.before"
printf 'corruption\n' >> "$archive"
if bash "$installer" --harness claude_code 2>/dev/null; then echo "Corrupt archive accepted" >&2; exit 1; fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
cp "$home/archive.before" "$archive"
cp "$dist/secretary-v0.0.1-ci-linux-$arch.json" "$home/manifest.original"
python3 - "$dist/secretary-v0.0.1-ci-linux-$arch.json" "$archive" <<'PY'
import hashlib, json, pathlib, sys
manifest, archive = map(pathlib.Path, sys.argv[1:])
with archive.open("ab") as out: out.write(b"forged")
data = archive.read_bytes()
m = json.loads(manifest.read_text())
m["sha256"] = hashlib.sha256(data).hexdigest()
m["bytes"] = len(data)
manifest.write_text(json.dumps(m))
PY
if bash "$installer" --harness claude_code > /dev/null 2>&1; then echo "Forged release pair accepted" >&2; exit 1; fi
cp "$home/archive.before" "$archive"
cp "$home/manifest.original" "$dist/secretary-v0.0.1-ci-linux-$arch.json"
printf 'unmanaged alteration\n' >> "$unit"
if bash "$installer" --harness claude_code > /dev/null 2>&1; then echo "Tampered unit accepted before activation" >&2; exit 1; fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
sed -i '$d' "$unit"
export TEST_ROLLBACK_HEALTH=true TEST_SERVICE_PID="$BASHPID"
if bash "$installer" --harness claude_code > /dev/null 2>&1; then echo "Unhealthy upgraded Node accepted" >&2; exit 1; fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
[[ "$(grep -c '^restart ' "$home/restarts.log")" -ge 2 ]]
[[ "$(tail -n 1 "$home/restarts.log")" == *0.0.0-ci* ]]
unset TEST_ROLLBACK_HEALTH
bash "$installer" disable-service
bash "$installer" --harness claude_code
[[ "$(readlink "$state/current")" == "$state/releases/secretary-v0.0.1-ci-linux-$arch" ]]
[[ -x "$state/current/bin/secretary-node" ]]
cmp "$home/config.before" "$state/node/config.json"
cmp "$home/enrollment.before" "$state/node/enrollment.env"
[[ "$(cat "$state/node/data/session-canary")" == native-session ]]
[[ ! -f "$unit" ]]
bash "$installer" --server https://secretary.example.test --name remote-ci --harness claude_code 2>/dev/null && { echo "Paired identity rewrite accepted" >&2; exit 1; }
[[ "$(cat "$state/node/data/session-canary")" == native-session ]]
echo 'Standalone Linux Node fixture completed'
