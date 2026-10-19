#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 1 ]] || exit 2
dist="$1"
root="$(cd "$(dirname "$0")/.." && pwd)"
arch="$(uname -m)"
[[ "$arch" == arm64 ]] || arch=amd64
home="$(mktemp -d "${RUNNER_TEMP:-/tmp}/secretary-node-clean.XXXXXXXX")"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/fixtures" "$home/dist" "$home/unpack"
cat > "$home/fixtures/claude" <<'EOF'
#!/bin/sh
[ "$1" = --version ] || exit 1
echo claude-fixture
EOF
chmod +x "$home/fixtures/claude"
for version in 0.0.0-ci 0.0.1-ci; do
  name="secretary-v$version-darwin-$arch"
  tar -xzf "$dist/$name.tar.gz" -C "$home/unpack"
  cat > "$home/unpack/$name/bin/secretary-node" <<'EOF'
#!/bin/bash
set -euo pipefail
config=""
action=""
for arg in "$@"; do
  case "$arg" in --enroll-only) action=enroll ;; --check-worker-sandbox) action=ready ;; esac
done
args=("$@")
for ((i=0;i<${#args[@]};i++)); do
  if [[ "${args[$i]}" == --config ]]; then config="${args[$((i+1))]}"; fi
done
if [[ "$action" == ready ]]; then
  if [[ "${FAULT_NODE_ACTIVATED:-}" == 1 && "$0" == */current/bin/secretary-node ]]; then exit 68; fi
  [[ -f "$config" ]] || exit 69
  exit 0
fi
if [[ "$action" == enroll ]]; then
  [[ "${SECRETARY_NODE_PAIRING_TOKEN:-}" == approved-test-token ]] || exit 70
  [[ -f "$config" ]] || exit 71
  dir="$(/usr/bin/python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["data_dir"])' "$config")"
  [[ ! -e "$dir/identity.json" ]] || exit 72
  printf '{"node":"fixture-node","credential":"YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE","connect_url":"wss://fixture.invalid/v1/nodes/connect"}\n' > "$dir/identity.json"
  chmod 600 "$dir/identity.json"
  printf 'paired\n' >> "$dir/enroll.calls"
  exit 0
fi
exit 73
EOF
  chmod +x "$home/unpack/$name/bin/secretary-node"
  tar -czf "$home/dist/$name.tar.gz" -C "$home/unpack" "$name"
  /usr/bin/python3 - "$dist/$name.json" "$home/dist/$name.tar.gz" "$home/dist/$name.json" <<'PY'
import json,sys,hashlib,pathlib
manifest=json.loads(pathlib.Path(sys.argv[1]).read_text())
archive=pathlib.Path(sys.argv[2]).read_bytes()
manifest["sha256"]=hashlib.sha256(archive).hexdigest()
manifest["bytes"]=len(archive)
pathlib.Path(sys.argv[3]).write_text(json.dumps(manifest))
PY
done
export HOME="$home/user" PATH="$home/fixtures:$PATH"
mkdir -p "$HOME"
export SECRETARY_NODE_SERVER=https://fixture.invalid
export SECRETARY_NODE_NAME=fixture-node SECRETARY_NODE_HARNESS=claude
export SECRETARY_NODE_PAIRING_TOKEN=approved-test-token
export SECRETARY_RELEASE_BASE="file://$home/dist"
export SECRETARY_VERSION=0.0.0-ci
export SECRETARY_NODE_PINNED_SHA256="$(shasum -a 256 "$home/dist/secretary-v0.0.0-ci-darwin-$arch.tar.gz" | awk '{print $1}')"
cat > "$home/fixtures/install" <<'EOF'
#!/bin/bash
if [[ "${FAULT_CONFIG_INSTALL:-}" == 1 ]]; then exit 76; fi
exec /usr/bin/install "$@"
EOF
chmod +x "$home/fixtures/install"
if FAULT_CONFIG_INSTALL=1 bash "$root/scripts/install-macos-standalone-node.sh" > "$home/interrupted.log" 2>&1; then
  echo "Partially configured first install was accepted" >&2; exit 1
fi
state="$HOME/.local/share/secretary-node"
[[ -d "$state/data" && ! -e "$state/config.json" && ! -e "$state/data/identity.json" ]]
bash "$root/scripts/install-macos-standalone-node.sh"
[[ -f "$state/config.json" && -f "$state/data/identity.json" ]]
[[ "$(cat "$state/data/enroll.calls")" == paired ]]
[[ "$(stat -f %Lp "$state/data/identity.json")" == 600 ]]
/usr/bin/python3 - "$state/data/identity.json" <<'PY'
import json,sys,base64
i=json.load(open(sys.argv[1]))
assert i["node"]=="fixture-node"
assert len(base64.urlsafe_b64decode(i["credential"]+"==="))>=32
assert i["connect_url"].startswith("wss://")
PY
old="$(readlink "$state/current")"
cp -R "$HOME/Applications/Secretary Node.app" "$home/old-app"
printf 'saved native session' > "$state/data/node-state.json"
unset SECRETARY_NODE_PAIRING_TOKEN
bash "$root/scripts/install-macos-standalone-node.sh"
[[ "$(cat "$state/data/enroll.calls")" == paired ]]
export SECRETARY_VERSION=0.0.1-ci
export SECRETARY_NODE_PINNED_SHA256="$(shasum -a 256 "$home/dist/secretary-v0.0.1-ci-darwin-$arch.tar.gz" | awk '{print $1}')"
if FAULT_NODE_ACTIVATED=1 bash "$root/scripts/install-macos-standalone-node.sh" > "$home/activation.log" 2>&1; then
  echo "Accepted failed activated Node readiness" >&2; exit 1
fi
grep -q 'Activated Node failed readiness' "$home/activation.log"
[[ "$(readlink "$state/current")" == "$old" ]]
diff -qr "$home/old-app" "$HOME/Applications/Secretary Node.app"
[[ "$(cat "$state/data/node-state.json")" == 'saved native session' ]]
[[ "$(cat "$state/data/enroll.calls")" == paired ]]
bash "$root/scripts/install-macos-standalone-node.sh"
[[ "$(readlink "$state/current")" == *0.0.1-ci* ]]
echo "Clean install, recovery, enrollment and activated readiness rollback fixture completed"
