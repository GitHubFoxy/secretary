#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
[[ "$(uname -s)" == Darwin ]] || { echo "macOS required" >&2; exit 1; }
[[ $# -eq 3 ]] || { echo "Usage: $0 PACKAGE_INPUT DIST_DIR REVISION" >&2; exit 1; }
input="$1"
dist="$2"
revision="$3"
version="${SECRETARY_TEST_VERSION:-0.0.0-ci}"
upgrade_version="${version}-upgrade"
arch="$(uname -m | sed 's/x86_64/amd64/')"
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
export SECRETARY_VERSION="$version"
bash "$root/scripts/install-macos-binary.sh"
"$HOME/.local/bin/secretary" doctor
"$HOME/.local/bin/secretary" start --no-open
status="$("$HOME/.local/bin/secretary" status)"
[[ "$status" == *"installed: version=${version}"* ]]
[[ "$status" == *"running Server: pid="*"version=${version} revision=$revision"* ]]
session_http="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session)"
[[ "$session_http" != 000 ]]
"$HOME/.local/bin/secretary" stop
[[ -f "$HOME/.local/share/secretary/environment" ]]
[[ -f "$HOME/.local/share/secretary/node/config.json" ]]
[[ -d "$HOME/Applications/Secretary Node.app" ]]
state="$HOME/.local/share/secretary"
initial_target="$state/releases/secretary-v${version}-darwin-$arch"
upgrade_target="$state/releases/secretary-v${upgrade_version}-darwin-$arch"
printf 'existing-session\n' > "$state/node/data/session-fixture"
printf 'existing-identity\n' > "$state/node/data/identity.json"
cp "$state/secretary.db" "$home/db.before"
cp "$state/environment" "$home/credentials.before"
cp "$state/config.toml" "$home/config.before"
cp "$state/node/config.json" "$home/node.before"
cp "$state/profiles/secretary.md" "$home/profile.before"
cp -R "$HOME/Applications/Secretary Node.app" "$home/app.before"
bash "$root/scripts/install-macos-binary.sh"
cmp "$home/credentials.before" "$state/environment"
cmp "$home/config.before" "$state/config.toml"
cmp "$home/node.before" "$state/node/config.json"
cmp "$home/profile.before" "$state/profiles/secretary.md"
cmp "$home/db.before" "$state/secretary.db"
[[ "$(cat "$state/node/data/identity.json")" == existing-identity ]]
[[ "$(cat "$state/node/data/session-fixture")" == existing-session ]]
cached_server="$state/releases/secretary-v${version}-darwin-$(uname -m | sed 's/x86_64/amd64/')/bin/secretary-mcp"
cp "$cached_server" "$home/mcp.before"
printf 'tampered-cached-binary' >> "$cached_server"
if bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Tampered cached release unexpectedly accepted" >&2
  exit 1
fi
[[ "$(readlink "$state/current")" == "$initial_target" ]]
cp "$home/mcp.before" "$cached_server"
bash "$root/scripts/install-macos-binary.sh"
cmp "$home/mcp.before" "$cached_server"
cmp "$home/db.before" "$state/secretary.db"
bash "$root/scripts/package-macos-binary-release.sh" "$upgrade_version" "$revision" "$(uname -m | sed 's/x86_64/amd64/')" "$input" "$dist"
export SECRETARY_VERSION="$upgrade_version"
upgrade_archive="$dist/secretary-v${upgrade_version}-darwin-$(uname -m | sed 's/x86_64/amd64/').tar.gz"
cp "$upgrade_archive" "$home/upgrade-backup"
printf 'corruption' >> "$upgrade_archive"
if bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Tampered release unexpectedly accepted" >&2
  exit 1
fi
[[ "$(readlink "$state/current")" == "$initial_target" ]]
cp "$home/upgrade-backup" "$upgrade_archive"
mkdir -p "$home/break"
cat > "$home/break/cp" <<'EOF'
#!/bin/bash
for arg in "$@"; do
  case "$arg" in */.release.*/secretary-*/RELEASE-MANIFEST.json)
    exit 92 ;;
  esac
done
exec /bin/cp "$@"
EOF
chmod +x "$home/break/cp"
if PATH="$home/break:$PATH" bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Interrupted release staging unexpectedly succeeded" >&2
  exit 1
fi
[[ "$(readlink "$state/current")" == "$initial_target" ]]
[[ ! -e "$state/releases/secretary-v${upgrade_version}-darwin-$(uname -m | sed 's/x86_64/amd64/')" ]]
diff -qr "$home/app.before" "$HOME/Applications/Secretary Node.app"
cmp "$home/db.before" "$state/secretary.db"
rm "$home/break/cp"
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
[[ "$(readlink "$state/current")" == "$initial_target" ]]
diff -qr "$home/app.before" "$HOME/Applications/Secretary Node.app"
"$HOME/.local/bin/secretary" doctor
status="$("$HOME/.local/bin/secretary" status)"
[[ "$status" == *"installed: version=${version}"* ]]
cmp "$home/db.before" "$state/secretary.db"
cmp "$home/credentials.before" "$state/environment"
bash "$root/scripts/install-macos-binary.sh"
[[ "$(readlink "$state/current")" == "$upgrade_target" ]]
cmp "$home/db.before" "$state/secretary.db"
[[ "$(cat "$state/node/data/identity.json")" == existing-identity ]]
[[ "$(cat "$state/node/data/session-fixture")" == existing-session ]]
cmp "$home/config.before" "$state/config.toml"
cmp "$home/credentials.before" "$state/environment"
cmp "$home/profile.before" "$state/profiles/secretary.md"
cmp "$home/node.before" "$state/node/config.json"
test -f "$state/releases/secretary-v${upgrade_version}-darwin-$(uname -m | sed 's/x86_64/amd64/')/RELEASE-MANIFEST.json"
clean_home="$home/clean-activation-failure"
mkdir -p "$clean_home"
if HOME="$clean_home" PATH="$home/break:$PATH" bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Clean install with failed activation unexpectedly succeeded" >&2
  exit 1
fi
[[ ! -e "$clean_home/Applications/Secretary Node.app" ]]
[[ ! -L "$clean_home/.local/share/secretary/current" ]]
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ ! -L "$clean_home/.local/bin/$executable" ]]
done
HOME="$clean_home" bash "$root/scripts/install-macos-binary.sh"
[[ -d "$clean_home/Applications/Secretary Node.app" ]]
[[ "$(readlink "$clean_home/.local/share/secretary/current")" == "$clean_home/.local/share/secretary/releases/secretary-v${upgrade_version}-darwin-$arch" ]]
# Source-managed CLI symlinks and binaries must survive a side-by-side install.
source_home="$home/source-profile"
mkdir -p "$source_home/.local/bin" "$source_home/Library/LaunchAgents"
printf 'source-owned Node LaunchAgent; must not be modified\n' > "$source_home/Library/LaunchAgents/dev.secretary.cli-node.plist"
cp "$source_home/Library/LaunchAgents/dev.secretary.cli-node.plist" "$home/source-launchagent.before"
printf '#!/bin/sh\nprintf "source-managed\\n"\n' > "$source_home/source-cli"
chmod +x "$source_home/source-cli"
ln -s "$source_home/source-cli" "$source_home/.local/bin/secretary"
printf '#!/bin/sh\nprintf "source-daemon\\n"\n' > "$source_home/.local/bin/secretaryd"
chmod +x "$source_home/.local/bin/secretaryd"
cp "$source_home/.local/bin/secretaryd" "$home/source-daemon.before"
HOME="$source_home" bash "$root/scripts/install-macos-binary.sh"
[[ "$(readlink "$source_home/.local/bin/secretary")" == "$source_home/source-cli" ]]
cmp "$home/source-daemon.before" "$source_home/.local/bin/secretaryd"
cmp "$home/source-launchagent.before" "$source_home/Library/LaunchAgents/dev.secretary.cli-node.plist"
[[ "$("$source_home/.local/bin/secretary")" == source-managed ]]
[[ "$("$source_home/.local/bin/secretaryd")" == source-daemon ]]
source_state="$source_home/.local/share/secretary"
source_private_bin="$source_state/bin"
[[ -L "$source_state/current" && -d "$source_home/Applications/Secretary Node.app" ]]
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ -L "$source_private_bin/$executable" ]]
  expected="$source_state/current/bin/$executable"
  [[ "$executable" != secretary ]] || expected="$source_state/current/secretary"
  [[ "$(readlink "$source_private_bin/$executable")" == "$expected" ]]
  if [[ "$executable" != secretary && "$executable" != secretaryd ]]; then
    [[ ! -e "$source_home/.local/bin/$executable" && ! -L "$source_home/.local/bin/$executable" ]]
  fi
done
status="$(HOME="$source_home" SECRETARY_BINARY_STAGE_BIN="$source_private_bin" "$source_state/current/secretary" status)"
[[ "$status" == *"installed: version=${upgrade_version}"* ]] || { echo "Side-by-side binary CLI cannot report its release" >&2; exit 1; }
HOME="$source_home" bash "$root/scripts/install-macos-binary.sh"
cmp "$home/source-daemon.before" "$source_home/.local/bin/secretaryd"
cmp "$home/source-launchagent.before" "$source_home/Library/LaunchAgents/dev.secretary.cli-node.plist"
[[ "$(readlink "$source_home/.local/bin/secretary")" == "$source_home/source-cli" ]]

# Private bin tampering must fail closed and must not overwrite the source.
rm -f "$source_private_bin/secretaryd"
printf '#!/bin/sh\nexit 0\n' > "$source_private_bin/secretaryd"
chmod +x "$source_private_bin/secretaryd"
if HOME="$source_home" bash "$root/scripts/install-macos-binary.sh" >/dev/null 2>&1; then
  echo "Unmanaged private release binary unexpectedly accepted" >&2
  exit 1
fi
cmp "$home/source-daemon.before" "$source_home/.local/bin/secretaryd"
[[ "$(readlink "$source_home/.local/bin/secretary")" == "$source_home/source-cli" ]]
echo "macOS binary install/rollback/upgrade fixture completed"
