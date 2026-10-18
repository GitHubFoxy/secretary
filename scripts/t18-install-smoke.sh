#!/usr/bin/env bash
# This validates real source build and clean-user configuration, NOT a provider
# backed Claude session or native steering. Never classify it as live acceptance.
set -euo pipefail

original_mod_cache="$(go env GOMODCACHE)"
original_build_cache="$(go env GOCACHE)"
root="$(cd "$(dirname "$0")/.." && pwd)"
home="$(mktemp -d)"
trap 'rm -rf "$home"' EXIT
mkdir -p "$home/fake-bin"
cat > "$home/fake-bin/claude" <<'EOF'
#!/usr/bin/env sh
case "$*" in
  "--version") echo "claude-code-fixture (not live)"; exit 0 ;;
  "auth status") exit 0 ;;
  *) echo "fake Claude CLI cannot make provider calls" >&2; exit 64 ;;
esac
EOF
chmod +x "$home/fake-bin/claude"
export HOME="$home"
export PATH="$home/fake-bin:$PATH"
export GOCACHE="$original_build_cache"
export GOMODCACHE="$original_mod_cache"

# Only Claude is discoverable in the clean-user fixture. Actual binaries built
# are Secretary binaries; fake provider authentication is never live proof.
cd "$root"
./secretary setup --non-interactive --harness claude_code
state="$HOME/.local/share/secretary"
test -s "$state/config.toml"
test -s "$state/environment"
test -s "$state/node/config.json"
grep -q '^harness = "claude_code"$' "$state/config.toml"
grep -q '^default_harness = "claude_code"$' "$state/config.toml"
test -x "$HOME/.local/bin/secretaryd"
test -x "$HOME/.local/bin/secretary-node"
./secretary setup --non-interactive --harness claude_code
./secretary doctor
printf 'T18 fixture PASS on %s: clean one-harness setup and repeat; provider-backed run NOT TESTED\n' "$(uname -s)"
