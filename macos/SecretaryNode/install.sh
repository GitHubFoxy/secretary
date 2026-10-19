#!/bin/bash
set -euo pipefail
[[ "$(uname -s)" == Darwin ]] || { echo "macOS only" >&2; exit 1; }
root="$(cd "$(dirname "$0")" && pwd)"
app="$HOME/Applications/Secretary Node.app"
legacy="$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
if [[ -e "$legacy" ]] || launchctl print "gui/$(id -u)/dev.secretary.cli-node" >/dev/null 2>&1; then
  echo "Existing CLI Node LaunchAgent conflicts with menu-managed Node. Run 'secretary node uninstall-service' first." >&2
  exit 1
fi
command -v swift >/dev/null || { echo "Install Xcode command line tools for Swift" >&2; exit 1; }
command -v codesign >/dev/null || { echo "codesign unavailable" >&2; exit 1; }
[[ -x "$HOME/.local/bin/secretary-node" ]] || { echo "Run secretary setup to install secretary-node first" >&2; exit 1; }
swift build --package-path "$root" -c release
binary="$root/.build/release/SecretaryNode"
[[ -x "$binary" ]] || { echo "Release executable missing: $binary" >&2; exit 1; }
mkdir -p "$HOME/Applications"
stage="$(mktemp -d "$HOME/Applications/.secretary-node.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT
bundle="$stage/Secretary Node.app"
mkdir -p "$bundle/Contents/MacOS"
cp "$binary" "$bundle/Contents/MacOS/SecretaryNode"
cp "$root/Info.plist" "$bundle/Contents/Info.plist"
codesign --force --sign - "$bundle"
codesign --verify "$bundle"
if [[ -d "$app" ]]; then
  # Running copies retain their executable until quit; updating does not change login preferences.
  backup="$stage/previous.app"
  mv "$app" "$backup"
  if ! mv "$bundle" "$app"; then mv "$backup" "$app"; exit 1; fi
else
  mv "$bundle" "$app"
fi
echo "Installed $app. Launch 'Secretary Node' from Spotlight. Configure Launch at Login in its menu."
