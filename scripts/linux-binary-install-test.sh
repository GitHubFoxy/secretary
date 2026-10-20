#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
[[ $# -eq 3 ]] || { echo "Usage: $0 PACKAGE_INPUT DIST REVISION" >&2; exit 1; }
input="$1"; dist="$2"; revision="$3"
version="${SECRETARY_TEST_VERSION:-0.0.0-ci}"
upgrade_version="${version}-upgrade"
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
  "--user enable --now secretary.service") exit 0 ;;
  "--user enable --now secretary-node.service") [[ -z "${FAIL_NODE_SERVICE:-}" ]] ;;
  "--user disable --now secretary-node.service secretary.service") exit 0 ;;
  "--user show secretary.service -p ActiveState --value"|"--user show secretary-node.service -p ActiveState --value")
    if [[ -n "${FAIL_CRASH_AFTER_START:-}" && "$*" == *"secretary-node.service"* ]]; then echo failed; else echo active; fi ;;
  "--user show secretary.service -p MainPID --value"|"--user show secretary-node.service -p MainPID --value") echo "${TEST_SERVICE_PID:?}" ;;
  *) exit 1 ;;
esac
SH
mkdir -p "$home/provider bin"
cat > "$home/provider bin/claude" <<'SH'
#!/bin/sh
case "$*" in "--version") echo "fixture" ;; "auth status") exit 0 ;; *) exit 1 ;; esac
SH
cat > "$home/mock/curl" <<'SH'
#!/bin/bash
if [[ "$*" == *"127.0.0.1:8081/v1/web/session"* ]]; then
  if [[ -n "${FAIL_HEALTH:-}" ]]; then exit 7; fi
  if [[ -n "${FAIL_HTTP_503:-}" ]]; then printf '503'; exit 0; fi
  printf '401'
  exit 0
fi
exec /usr/bin/curl "$@"
SH
for tool in go npm node; do
  cat > "$home/mock/$tool" <<'SH'
#!/bin/sh
echo "Installer invoked a forbidden build tool" >&2
exit 97
SH
done
chmod +x "$home/mock/"* "$home/provider bin/claude"
mkdir -p "$home/run"
touch "$home/run/bus"
export HOME="$home" PATH="$home/mock:$home/provider bin:$PATH" XDG_RUNTIME_DIR="$home/run"
export SECRETARY_VERSION="$version" SECRETARY_RELEASE_BASE="file://$dist"
export TEST_SERVICE_PID="${BASHPID}"
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
initial_target="$state/releases/secretary-v${version}-linux-$arch"
upgrade_target="$state/releases/secretary-v${upgrade_version}-linux-$arch"
[[ -L "$state/current" && -f "$state/environment" && -f "$state/node/config.json" ]]
[[ -f "$home/.config/systemd/user/secretary.service" && -f "$home/.config/systemd/user/secretary-node.service" ]]
grep -Fqx "WorkingDirectory=$state/current" "$home/.config/systemd/user/secretary.service"
"$home/.local/bin/secretary" status | grep -F "installed: version=$version"
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
bash "$root/scripts/package-linux-binary-release.sh" "$upgrade_version" "$revision" "$arch" "$input" "$dist"
export SECRETARY_VERSION="$upgrade_version"
archive="$dist/secretary-v${upgrade_version}-linux-$arch.tar.gz"
cp "$archive" "$home/archive.backup"
printf 'corrupt\n' >> "$archive"
if bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then exit 1; fi
[[ "$(readlink "$state/current")" == "$initial_target" ]]
cp "$home/archive.backup" "$archive"
cp "$dist/secretary-v${upgrade_version}-linux-$arch.json" "$home/manifest.backup"
python3 - "$dist/secretary-v${upgrade_version}-linux-$arch.json" <<'PY'
import json, sys
p = sys.argv[1]
m = json.load(open(p))
m["arch"] = "wrong"
open(p, "w").write(json.dumps(m))
PY
if bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then exit 1; fi
cp "$home/manifest.backup" "$dist/secretary-v${upgrade_version}-linux-$arch.json"
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
[[ "$(readlink "$state/current")" == "$initial_target" ]]
rm "$home/break/cp"
bash "$root/scripts/install-linux-binary.sh"
[[ "$(readlink "$state/current")" == "$upgrade_target" ]]
cmp "$home/credentials.before" "$state/environment"
cmp "$home/config.before" "$state/config.toml"
cmp "$home/node.before" "$state/node/config.json"
[[ "$(cat "$state/node/data/session-fixture")" == native-session ]]
[[ "$(cat "$state/node/data/identity.json")" == node-identity ]]

new_home="$home/failed-service"
mkdir -p "$new_home"
if HOME="$new_home" FAIL_NODE_SERVICE=1 bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then
  echo "Failed service activation was accepted" >&2
  exit 1
fi
[[ ! -L "$new_home/.local/share/secretary/current" ]]
[[ ! -e "$new_home/.config/systemd/user/secretary.service" ]]
[[ ! -e "$new_home/.config/systemd/user/secretary-node.service" ]]
HOME="$new_home" bash "$root/scripts/install-linux-binary.sh"
[[ -L "$new_home/.local/share/secretary/current" ]]

for failure in FAIL_CRASH_AFTER_START FAIL_HEALTH FAIL_HTTP_503; do
  broken_home="$home/health-$failure"
  mkdir -p "$broken_home"
  if HOME="$broken_home" env "$failure=1" bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then
    echo "Unhealthy systemd activation was accepted: $failure" >&2
    exit 1
  fi
  [[ ! -L "$broken_home/.local/share/secretary/current" ]]
  [[ ! -e "$broken_home/.config/systemd/user/secretary.service" ]]
  [[ ! -e "$broken_home/.config/systemd/user/secretary-node.service" ]]
done
# Linux release install must never silently replace the user's explicitly
# selected harness. Invalid choices and half-selected Codex pairs fail closed.
if HOME="$home/invalid-choice" SECRETARY_INSTALL_HARNESS=invalid SECRETARY_VERSION="$version" bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then
  echo "Invalid installer harness selection was accepted" >&2
  exit 1
fi
[[ ! -e "$home/invalid-choice/.local/share/secretary/current" ]]
if HOME="$home/half-pair" SECRETARY_INSTALL_CODEX_CLI=/bin/true SECRETARY_VERSION="$version" bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then
  echo "Half-selected native Codex pair was accepted" >&2
  exit 1
fi
[[ ! -e "$home/half-pair/.local/share/secretary/current" ]]

# The harness explicitly selected by the owner must install without launching
# an implicitly downloaded Codex binary.
claude_home="$home/explicit-claude"
mkdir -p "$claude_home"
HOME="$claude_home" SECRETARY_INSTALL_HARNESS=claude_code SECRETARY_VERSION="$version" bash "$root/scripts/install-linux-binary.sh"
[[ -L "$claude_home/.local/share/secretary/current" ]]
[[ ! -d "$claude_home/.local/share/secretary/native/releases" ]]

# A full explicitly selected native Codex/ACP pair is pinned as external
# instead of attempting the potentially incompatible managed upstream binary.
cat > "$home/provider bin/fixture-codex" <<'SH'
#!/bin/sh
case "$*" in
  "--version") echo "codex-cli 0.163.0" ;;
  "login status") echo "Not logged in" >&2; exit 1 ;;
  *) exit 1 ;;
esac
SH
cat > "$home/provider bin/fixture-codex-acp" <<'SH'
#!/bin/sh
case "$*" in
  "--version") echo "codex-acp 0.16.0" ;;
  *) exit 1 ;;
esac
SH
chmod +x "$home/provider bin/fixture-codex" "$home/provider bin/fixture-codex-acp"
codex_home="$home/explicit-codex"
mkdir -p "$codex_home"
HOME="$codex_home" SECRETARY_VERSION="$version" \
  SECRETARY_INSTALL_CODEX_CLI="$home/provider bin/fixture-codex" \
  SECRETARY_INSTALL_CODEX_ACP="$home/provider bin/fixture-codex-acp" \
  bash "$root/scripts/install-linux-binary.sh"
[[ -L "$codex_home/.local/share/secretary/current" ]]
[[ ! -d "$codex_home/.local/share/secretary/native/releases" ]]
python3 - "$codex_home" "$home/provider bin/fixture-codex" "$home/provider bin/fixture-codex-acp" <<'PY'
import json, pathlib, sys
home, cli, acp = sys.argv[1:]
for scope in ("server", "node"):
    pair = json.loads((pathlib.Path(home) / ".local/share/secretary/native" / scope / "codex-pair.json").read_text())
    assert pair["codex"]["path"] == cli
    assert pair["acp"]["path"] == acp
    assert pair["codex"]["provenance"] == pair["acp"]["provenance"] == "external"
PY
collision_home="$home/previous-source-executables"
mkdir -p "$collision_home/.local/bin"
printf '#!/bin/sh\nprintf "legacy\\n"\n' > "$collision_home/.local/bin/secretaryd"
chmod +x "$collision_home/.local/bin/secretaryd"
cp "$collision_home/.local/bin/secretaryd" "$home/legacy-secretaryd"
HOME="$collision_home" SECRETARY_VERSION="$version" bash "$root/scripts/install-linux-binary.sh"
collision_state="$collision_home/.local/share/secretary"
collision_bin="$collision_state/bin"
cmp "$home/legacy-secretaryd" "$collision_home/.local/bin/secretaryd"
[[ ! -e "$collision_home/.local/bin/secretary" && ! -L "$collision_home/.local/bin/secretary" ]]
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ -L "$collision_bin/$executable" ]]
  expected="$collision_state/current/bin/$executable"
  [[ "$executable" != secretary ]] || expected="$collision_state/current/secretary"
  [[ "$(readlink "$collision_bin/$executable")" == "$expected" ]]
done
grep -Fqx "Environment=\"SECRETARY_BINARY_STAGE_BIN=$collision_bin\"" "$collision_home/.config/systemd/user/secretary.service"
grep -Fqx "Environment=\"SECRETARY_BINARY_STAGE_BIN=$collision_bin\"" "$collision_home/.config/systemd/user/secretary-node.service"
SECRETARY_BINARY_STAGE_BIN="$collision_bin" "$collision_state/current/secretary" status | grep -F "installed: version=$version"
SECRETARY_BINARY_STAGE_BIN="$collision_bin" "$collision_state/current/secretary" doctor | grep -F "Doctor found no problems"
HOME="$collision_home" SECRETARY_VERSION="$version" bash "$root/scripts/install-linux-binary.sh"
cmp "$home/legacy-secretaryd" "$collision_home/.local/bin/secretaryd"
[[ ! -e "$collision_home/.local/bin/secretary" && ! -L "$collision_home/.local/bin/secretary" ]]

source_link_home="$home/previous-source-symlink"
mkdir -p "$source_link_home/.local/bin"
ln -s /tmp/existing-source-checkout/secretary "$source_link_home/.local/bin/secretary"
HOME="$source_link_home" SECRETARY_VERSION="$version" bash "$root/scripts/install-linux-binary.sh"
[[ "$(readlink "$source_link_home/.local/bin/secretary")" == /tmp/existing-source-checkout/secretary ]]
[[ -L "$source_link_home/.local/share/secretary/bin/secretary" ]]
SECRETARY_BINARY_STAGE_BIN="$source_link_home/.local/share/secretary/bin" \
  "$source_link_home/.local/share/secretary/current/secretary" status | grep -F "installed: version=$version"

source_home="$home/source-managed"
mkdir -p "$source_home/.config/systemd/user"
cat > "$source_home/.config/systemd/user/secretary.service" <<'SH'
[Unit]
Description=Secretary Server
[Service]
WorkingDirectory=/tmp/source-checkout
ExecStart=/tmp/source-checkout/secretary serve
SH
if HOME="$source_home" bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then
  echo "Source-managed systemd unit was accepted" >&2
  exit 1
fi
[[ ! -L "$source_home/.local/share/secretary/current" ]]
grep -Fqx 'ExecStart=/tmp/source-checkout/secretary serve' "$source_home/.config/systemd/user/secretary.service"
source_node_home="$home/source-managed-node"
mkdir -p "$source_node_home/.config/systemd/user"
cat > "$source_node_home/.config/systemd/user/secretary-node.service" <<'SH'
[Unit]
Description=Secretary co-located Execution Node
[Service]
WorkingDirectory=/tmp/source-checkout
ExecStart=/tmp/source-checkout/secretary node serve
SH
if HOME="$source_node_home" bash "$root/scripts/install-linux-binary.sh" >/dev/null 2>&1; then
  echo "Source-managed Node systemd unit was accepted" >&2
  exit 1
fi
[[ ! -L "$source_node_home/.local/share/secretary/current" ]]
grep -Fqx 'ExecStart=/tmp/source-checkout/secretary node serve' "$source_node_home/.config/systemd/user/secretary-node.service"
special_home="$home/user with spaces%sign"
mkdir -p "$special_home"
HOME="$special_home" bash "$root/scripts/install-linux-binary.sh"
special_unit="$special_home/.config/systemd/user/secretary.service"
grep -Fq 'user\x20with\x20spaces%%sign' "$special_unit"
grep -Fq 'ExecStart="' "$special_unit"
grep -Fq 'Environment="PATH=' "$special_unit"
grep -Fq "$home/provider bin" "$special_unit"
