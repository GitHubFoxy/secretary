#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$(uname -s)" == Darwin ]] || { echo "macOS required" >&2; exit 1; }
[[ $# -eq 3 ]] || { echo "Usage: $0 PACKAGE_INPUT DIST_DIR REVISION" >&2; exit 1; }
input="$1"
dist="$2"
revision="$3"
home="$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/secretary-install-user.XXXXXXXX")"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/fixtures"
cat > "$home/fixtures/claude" <<'EOF'
#!/bin/sh
case "$*" in
  "--version") echo "preinstalled harness fixture" ;;
  "auth status") exit 0 ;;
  *) exit 1 ;;
esac
EOF
for utility in go swift npm; do
  cat > "$home/fixtures/$utility" <<'EOF'
#!/bin/sh
echo "Unexpected developer tool invoked during binary install" >&2
exit 97
EOF
  chmod +x "$home/fixtures/$utility"
done
chmod +x "$home/fixtures/claude"
export HOME="$home" PATH="$home/fixtures:$PATH" SECRETARY_RELEASE_BASE="file://$dist"
export SECRETARY_VERSION=0.0.0-ci
bash "$root/scripts/install-macos-binary.sh"
"$HOME/.local/bin/secretary" doctor
"$HOME/.local/bin/secretary" start --no-open
status="$("$HOME/.local/bin/secretary" status)"
[[ "$status" == *"installed: version=0.0.0-ci"* ]]
[[ "$status" == *"running Server: pid="*"version=0.0.0-ci revision=$revision"* ]]
session_http="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session)"
[[ "$session_http" != 000 ]]
"$HOME/.local/bin/secretary" stop
[[ -f "$HOME/.local/share/secretary/environment" ]]
[[ -f "$HOME/.local/share/secretary/node/config.json" ]]
[[ -d "$HOME/Applications/Secretary Node.app" ]]
state="$HOME/.local/share/secretary"
printf 'existing-session\n' > "$state/node/data/session-fixture"
printf 'existing-identity\n' > "$state/node/data/identity.json"
printf 'existing-db\n' > "$state/secretary.db"
cp "$state/environment" "$home/credentials.before"
cp "$state/config.toml" "$home/config.before"
cp "$state/node/config.json" "$home/node.before"
cp "$state/profiles/secretary.md" "$home/profile.before"
bash "$root/scripts/install-macos-binary.sh"
cmp "$home/credentials.before" "$state/environment"
cmp "$home/config.before" "$state/config.toml"
cmp "$home/node.before" "$state/node/config.json"
cmp "$home/profile.before" "$state/profiles/secretary.md"
[[ "$(cat "$state/secretary.db")" == existing-db ]]
[[ "$(cat "$state/node/data/identity.json")" == existing-identity ]]
[[ "$(cat "$state/node/data/session-fixture")" == existing-session ]]
bash "$root/scripts/package-macos-binary-release.sh" 0.0.1-ci "$revision" "$(uname -m | sed 's/x86_64/amd64/')" "$input" "$dist"
export SECRETARY_VERSION=0.0.1-ci
upgrade_archive="$dist/secretary-v0.0.1-ci-darwin-$(uname -m | sed 's/x86_64/amd64/').tar.gz"
cp "$upgrade_archive" "$home/upgrade-backup"
printf 'corruption' >> "$upgrade_archive"
if bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Tampered release unexpectedly accepted" >&2
  exit 1
fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
cp "$home/upgrade-backup" "$upgrade_archive"
mkdir -p "$home/break"
cat > "$home/break/mv" <<'EOF'
#!/bin/bash
for arg in "$@"; do
  case "$arg" in */.current.*.new)
    exit 91 ;;
  esac
done
exec /bin/mv "$@"
EOF
chmod +x "$home/break/mv"
if PATH="$home/break:$PATH" bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Release activation failure unexpectedly succeeded" >&2
  exit 1
fi
[[ "$(readlink "$state/current")" == *0.0.0-ci* ]]
[[ -d "$HOME/Applications/Secretary Node.app" ]]
cmp "$home/credentials.before" "$state/environment"
bash "$root/scripts/install-macos-binary.sh"
[[ "$(readlink "$state/current")" == *0.0.1-ci* ]]
[[ "$(cat "$state/secretary.db")" == existing-db ]]
[[ "$(cat "$state/node/data/identity.json")" == existing-identity ]]
[[ "$(cat "$state/node/data/session-fixture")" == existing-session ]]
cmp "$home/config.before" "$state/config.toml"
cmp "$home/credentials.before" "$state/environment"
cmp "$home/profile.before" "$state/profiles/secretary.md"
test -f "$state/releases/secretary-v0.0.1-ci-darwin-$(uname -m | sed 's/x86_64/amd64/')/RELEASE-MANIFEST.json"
echo "macOS binary install/rollback/upgrade fixture completed"
