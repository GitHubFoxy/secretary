#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
[[ $# -eq 3 ]] || { echo "Usage: $0 PACKAGE_INPUT DIST REVISION" >&2; exit 1; }
input="$1"; dist="$2"; revision="$3"
arch=amd64
case "$(uname -m)" in x86_64) ;; aarch64) arch=arm64 ;; *) exit 1 ;; esac
home="$(mktemp -d)"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/mock"
cat > "$home/mock/systemctl" <<'SH'
#!/bin/bash
case "$*" in
  "--user show-environment") exit 0 ;;
  "--user daemon-reload") exit 0 ;;
  "--user enable --now secretary.service"|"--user enable --now secretary-node.service") exit 0 ;;
  *) exit 1 ;;
esac
SH
cat > "$home/mock/claude" <<'SH'
#!/bin/sh
case "$*" in "--version") echo "fixture" ;; "auth status") exit 0 ;; *) exit 1 ;; esac
SH
for tool in go npm node; do
  cat > "$home/mock/$tool" <<'SH'
#!/bin/sh
echo "Installer invoked a forbidden build tool" >&2
exit 97
SH
done
chmod +x "$home/mock/"*
mkdir -p "$home/run"
touch "$home/run/bus"
export HOME="$home" PATH="$home/mock:$PATH" XDG_RUNTIME_DIR="$home/run"
export SECRETARY_VERSION=0.0.0-ci SECRETARY_RELEASE_BASE="file://$dist"
bash "$root/scripts/install-linux-binary.sh" && { echo "Non-systemd host was accepted" >&2; exit 1; }
rm "$home/run/bus"
python3 - "$home/run/bus" <<'PY'
import socket, sys
s = socket.socket(socket.AF_UNIX)
s.bind(sys.argv[1])
s.close()
PY
bash "$root/scripts/install-linux-binary.sh"
state="$home/.local/share/secretary"
[[ -L "$state/current" && -f "$state/environment" && -f "$state/node/config.json" ]]
[[ -f "$home/.config/systemd/user/secretary.service" && -f "$home/.config/systemd/user/secretary-node.service" ]]
grep -Fqx "WorkingDirectory=$state/current" "$home/.config/systemd/user/secretary.service"
"$home/.local/bin/secretary" status | grep -F 'installed: version=0.0.0-ci'
cp "$state/environment" "$home/credentials.before"
cp "$state/config.toml" "$home/config.before"
cp "$state/node/config.json" "$home/node.before"
mkdir -p "$state/node/data"
printf 'native-session\n' > "$state/node/data/session-fixture"
printf 'node-identity\n' > "$state/node/data/identity.json"
bash "$root/scripts/install-linux-binary.sh"
cmp "$home/credentials.before" "$state/environment"
cmp "$home/config.before" "$state/config.toml"
cmp "$home/node.before" "$state/node/config.json"
[[ "$(cat "$state/node/data/session-fixture")" == native-session ]]
[[ "$(cat "$state/node/data/identity.json")" == node-identity ]]
bash "$root/scripts/package-linux-binary-release.sh" 0.0.1-ci "$revision" "$arch" "$input" "$dist"
export SECRETARY_VERSION=0.0.1-ci
archive="$dist/secretary-v0.0.1-ci-linux-$arch.tar.gz"
cp "$archive" "$home/archive.backup"
printf 'corrupt\n' >> "$archive"
if bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then exit 1; fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
cp "$home/archive.backup" "$archive"
cp "$dist/secretary-v0.0.1-ci-linux-$arch.json" "$home/manifest.backup"
python3 - "$dist/secretary-v0.0.1-ci-linux-$arch.json" <<'PY'
import json, sys
p = sys.argv[1]
m = json.load(open(p))
m["arch"] = "wrong"
open(p, "w").write(json.dumps(m))
PY
if bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then exit 1; fi
cp "$home/manifest.backup" "$dist/secretary-v0.0.1-ci-linux-$arch.json"
mkdir -p "$home/break"
cat > "$home/break/cp" <<'SH'
#!/bin/bash
for arg in "$@"; do
  case "$arg" in */.release.*/secretary-v*/RELEASE-MANIFEST.json) exit 91 ;; esac
done
exec /bin/cp "$@"
SH
chmod +x "$home/break/cp"
if PATH="$home/break:$PATH" bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then exit 1; fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
rm "$home/break/cp"
bash "$root/scripts/install-linux-binary.sh"
[[ "$(readlink "$state/current")" == *0.0.1-ci* ]]
cmp "$home/credentials.before" "$state/environment"
cmp "$home/config.before" "$state/config.toml"
cmp "$home/node.before" "$state/node/config.json"
[[ "$(cat "$state/node/data/session-fixture")" == native-session ]]
[[ "$(cat "$state/node/data/identity.json")" == node-identity ]]
