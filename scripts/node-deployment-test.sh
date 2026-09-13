#!/bin/zsh
set -euo pipefail

ROOT="${0:A:h:h}"
ORIGINAL_HOME="$HOME"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/secretary-node-test.XXXXXX")"
FAKE_BIN="$TEST_HOME/bin"
trap 'rm -rf "$TEST_HOME"' EXIT
mkdir -p "$FAKE_BIN"
cat > "$FAKE_BIN/fx" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$FAKE_BIN/launchctl" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$FAKE_BIN/fx" "$FAKE_BIN/launchctl"
export HOME="$TEST_HOME"
export PATH="$FAKE_BIN:/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin:${PATH:-}"
export MISE_DATA_DIR="${MISE_DATA_DIR:-$ORIGINAL_HOME/.local/share/mise}"
export GOMODCACHE="${GOMODCACHE:-$ORIGINAL_HOME/go/pkg/mod}"
export GOCACHE="${GOCACHE:-$ORIGINAL_HOME/Library/Caches/go-build}"
export MISE_YES=1

fail() { print -u2 -- "FAIL: $1"; exit 1; }
assert_contains() { [[ "$1" == *"$2"* ]] || fail "$3"; }
assert_not_contains() { [[ "$1" != *"$2"* ]] || fail "$3"; }

output="$($ROOT/sex setup)" || fail "sex setup"
assert_contains "$output" "Full-access trusted Node policy" "setup did not show trusted Node policy"
output="$($ROOT/sex node setup --server https://secretary.example.ts.net --name macbook --workspace frontend=/Users/me/src/frontend)" || fail "node setup"
assert_contains "$output" "outbound connection only" "node setup did not state private networking"
config="$HOME/.local/share/secretary/node/config.json"
[[ -f "$config" ]] || fail "missing Node config"
assert_contains "$(cat "$config")" '"project_id":"frontend"' "workspace mapping missing"
for secret in credential token SECRETARY_; do
  assert_not_contains "$(cat "$config")" "$secret" "Node config contains credential fields: $secret"
done
output="$($ROOT/sex node doctor)" || fail "node doctor"
assert_contains "$output" "no problems" "node doctor failed"
$ROOT/sex node install-service >/dev/null || fail "node install-service"
plist="$HOME/Library/LaunchAgents/dev.secretary.sex-node.plist"
[[ -f "$plist" ]] || fail "missing Node launchd plist"
assert_contains "$(cat "$plist")" '<string>node</string><string>serve</string>' "Node plist does not use foreground serve"
for secret in PAIRING_TOKEN credential SECRETARY_CAPABILITY; do
  assert_not_contains "$(cat "$plist")" "$secret" "Node plist contains credentials: $secret"
done
print "All Node deployment tests passed."
