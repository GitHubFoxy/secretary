#!/usr/bin/env bash
set -euo pipefail
[[ "$(uname -s)" == Darwin ]] || { echo "macOS required" >&2; exit 1; }
[[ $# == 2 ]] || { echo "Usage: $0 DIST REVISION" >&2; exit 1; }
root="$(cd "$(dirname "$0")/.." && pwd)"
dist="$1"
revision="$2"
arch="$(uname -m)"
[[ "$arch" == arm64 ]] || arch=amd64
home="$(mktemp -d "${RUNNER_TEMP:-/tmp}/standalone-node.XXXXXXXX")"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/fixtures" "$home/.local/share/secretary-node/data"
cat > "$home/fixtures/claude" <<'EOF'
#!/bin/sh
if [ "$1" = "--version" ]; then echo native-claude-fixture; exit 0; fi
exit 1
EOF
for name in go swift npm; do
  printf '#!/bin/sh\necho Developer tool invoked >&2\nexit 93\n' > "$home/fixtures/$name"
  chmod +x "$home/fixtures/$name"
done
chmod +x "$home/fixtures/claude"
export HOME="$home" PATH="$home/fixtures:$PATH"
export SECRETARY_NODE_SERVER="https://node-test.example.invalid"
export SECRETARY_NODE_NAME="fixture-node"
export SECRETARY_NODE_HARNESS=claude
export SECRETARY_RELEASE_BASE="file://$dist"
export SECRETARY_VERSION=0.0.0-ci
export SECRETARY_NODE_PINNED_SHA256="$(shasum -a 256 "$dist/secretary-v0.0.0-ci-darwin-$arch.tar.gz" | awk '{print $1}')"
state="$HOME/.local/share/secretary-node"
printf 'fixture-identity' > "$state/data/identity.json"
printf 'native checkpoint' > "$state/data/node-state.json"
printf 'native credentials' > "$state/data/credentials.txt"
cat > "$state/config.json" <<EOF
{"server_url":"$SECRETARY_NODE_SERVER","node":"$SECRETARY_NODE_NAME","data_dir":"$state/data","capacity":1,"include_opencode":false,"standalone":true}
EOF
chmod 600 "$state/config.json" "$state/data/"*
if SECRETARY_NODE_TRUSTED_TEAM_ID=AAAAAAAAAA bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Accepted untrusted signing identity" >&2; exit 1; fi
bash "$root/scripts/install-macos-standalone-node.sh"
[[ -x "$state/current/bin/secretary-node" ]]
[[ -d "$HOME/Applications/Secretary Node.app" ]]
[[ ! -e "$HOME/.local/bin/secretaryd" && ! -e "$HOME/.local/bin/secretaryctl" ]]
[[ ! -e "$state/current/bin/secretaryd" && ! -e "$state/current/bin/secretaryctl" ]]
[[ ! -e "$state/current/bin/ffmpeg" && ! -e "$state/current/bin/ffprobe" ]]
[[ ! -e "$HOME/.local/share/secretary/config.toml" ]]
cp "$state/config.json" "$home/config.before"
cp "$state/data/identity.json" "$home/identity.before"
cp "$state/data/node-state.json" "$home/session.before"
cp "$state/data/credentials.txt" "$home/credentials.before"
bash "$root/scripts/install-macos-standalone-node.sh"
cmp "$home/config.before" "$state/config.json"
cmp "$home/identity.before" "$state/data/identity.json"
cmp "$home/session.before" "$state/data/node-state.json"
cmp "$home/credentials.before" "$state/data/credentials.txt"
old="$(readlink "$state/current")"
cp "$dist/secretary-v0.0.0-ci-darwin-$arch.tar.gz" "$home/archive.before"
printf 'tampered' >> "$dist/secretary-v0.0.0-ci-darwin-$arch.tar.gz"
if bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Accepted corrupt archive" >&2; exit 1; fi
[[ "$(readlink "$state/current")" == "$old" ]]
mv "$home/archive.before" "$dist/secretary-v0.0.0-ci-darwin-$arch.tar.gz"
if SECRETARY_NODE_SERVER=https://different.example.invalid bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Changed paired Server" >&2; exit 1; fi
if SECRETARY_NODE_NAME='bad"name' bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Accepted unsafe identity" >&2; exit 1; fi
bash "$root/scripts/install-macos-standalone-node.sh"
[[ "$(readlink "$state/current")" == "$old" ]]
cmp "$home/session.before" "$state/data/node-state.json"
export SECRETARY_VERSION=0.0.1-ci
export SECRETARY_NODE_PINNED_SHA256="$(shasum -a 256 "$dist/secretary-v0.0.1-ci-darwin-$arch.tar.gz" | awk '{print $1}')"
cp -R "$HOME/Applications/Secretary Node.app" "$home/app.before"
mkdir -p "$home/fault"
cat > "$home/fault/mv" <<'EOF'
#!/bin/bash
for arg in "$@"; do
  case "$arg" in */current.new) exit 92 ;; esac
done
exec /bin/mv "$@"
EOF
chmod +x "$home/fault/mv"
if PATH="$home/fault:$PATH" bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Accepted failed pointer activation" >&2; exit 1; fi
[[ "$(readlink "$state/current")" == "$old" ]]
cmp "$home/identity.before" "$state/data/identity.json"
diff -qr "$home/app.before" "$HOME/Applications/Secretary Node.app"
cat > "$home/fault/codesign" <<'EOF'
#!/bin/bash
case "$*" in *"/Applications/Secretary Node.app"*) exit 94 ;; esac
exec /usr/bin/codesign "$@"
EOF
chmod +x "$home/fault/codesign"
rm "$home/fault/mv"
if PATH="$home/fault:$PATH" bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Accepted failed post-activation companion check" >&2; exit 1; fi
[[ "$(readlink "$state/current")" == "$old" ]]
cmp "$home/identity.before" "$state/data/identity.json"
diff -qr "$home/app.before" "$HOME/Applications/Secretary Node.app"
rm "$home/fault/codesign"
bash "$root/scripts/install-macos-standalone-node.sh"
[[ "$(readlink "$state/current")" == *0.0.1-ci* ]]
cmp "$home/config.before" "$state/config.json"
cmp "$home/identity.before" "$state/data/identity.json"
cmp "$home/session.before" "$state/data/node-state.json"
cmp "$home/credentials.before" "$state/data/credentials.txt"
mkdir -p "$HOME/Library/LaunchAgents"
touch "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
if bash "$root/scripts/install-macos-standalone-node.sh" >/dev/null 2>&1; then echo "Accepted conflicting legacy LaunchAgent" >&2; exit 1; fi
rm "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
[[ ! -f "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist" ]]
echo "Node-only macOS fixture completed; no live owner session asserted"
