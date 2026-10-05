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
cat > "$FAKE_BIN/opencode" <<'EOF'
#!/bin/sh
printf '%s|%s|openai=%s|aws=%s\n' "${XDG_DATA_HOME:-missing}" "$*" "${OPENAI_API_KEY:+present}" "${AWS_ACCESS_KEY_ID:+present}" >> "$TICKET33_OPENCODE_STORE_LOG"
case "$1:$2" in
  --version:*) printf 'opencode v2.0.22\n' ;;
  auth:list)
    mkdir -p "$XDG_DATA_HOME/opencode"
    if [ ! -e "$XDG_DATA_HOME/opencode/opencode.db" ]; then printf 'native-db-initialized\n' > "$XDG_DATA_HOME/opencode/opencode.db"; fi
    if [ "${FAKE_OPENCODE_AUTH_MODE:-ready}" = "missing" ]; then printf '[]\n'; else printf 'provider authenticated\n'; fi
    ;;
  auth:login)
    mkdir -p "$XDG_DATA_HOME/opencode"
    printf 'owner login fixture\n' > "$XDG_DATA_HOME/opencode/auth.json"
    printf 'owner login fixture\n'
    ;;
esac
EOF
chmod +x "$FAKE_BIN/fx" "$FAKE_BIN/launchctl" "$FAKE_BIN/opencode"
export HOME="$TEST_HOME"
export PATH="$FAKE_BIN:/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin:${PATH:-}"
export MISE_DATA_DIR="${MISE_DATA_DIR:-$ORIGINAL_HOME/.local/share/mise}"
export GOMODCACHE="${GOMODCACHE:-$ORIGINAL_HOME/go/pkg/mod}"
export GOCACHE="${GOCACHE:-$ORIGINAL_HOME/Library/Caches/go-build}"
export MISE_YES=1
export OPENAI_API_KEY=fixture-private-provider-key AWS_ACCESS_KEY_ID=fixture-private-access-id AWS_SECRET_ACCESS_KEY=fixture-private-cloud-key
TICKET33_OPENCODE_STORE_LOG="$TEST_HOME/opencode-store.log"
export TICKET33_OPENCODE_STORE_LOG
mkdir -p "$HOME/.local/share/opencode"
printf 'personal-store-canary\n' > "$HOME/.local/share/opencode/opencode.db"
chmod 600 "$HOME/.local/share/opencode/opencode.db"

fail() { print -u2 -- "FAIL: $1"; exit 1; }
assert_contains() { [[ "$1" == *"$2"* ]] || fail "$3"; }
assert_not_contains() { [[ "$1" != *"$2"* ]] || fail "$3"; }
assert_file() { [[ -f "$1" ]] || fail "missing file: $1"; }

output="$($ROOT/sex setup)" || fail "sex setup"
assert_contains "$output" "Full-access trusted Node policy" "setup did not show trusted Node policy"
assert_file "$HOME/.local/share/secretary/opencode-native/opencode/opencode.db"
assert_file "$HOME/.local/share/secretary/node/data/opencode-native/opencode/opencode.db"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$HOME/.local/share/opencode" "setup accessed personal OpenCode store"
output="$($ROOT/sex node setup --server https://secretary.example.ts.net --name macbook --workspace frontend=/Users/me/src/frontend)" || fail "node setup"
assert_contains "$output" "outbound connection only" "node setup did not state private networking"
config="$HOME/.local/share/secretary/node/config.json"
[[ -f "$config" ]] || fail "missing Node config"
assert_contains "$(cat "$config")" '"project_id":"frontend"' "workspace mapping missing"
assert_contains "$(cat "$config")" '"include_opencode": true' "Node setup did not enable OpenCode discovery"
for secret in credential token SECRETARY_; do
  assert_not_contains "$(cat "$config")" "$secret" "Node config contains credential fields: $secret"
done
if output="$(FAKE_OPENCODE_AUTH_MODE=missing "$ROOT/sex" node doctor 2>&1)"; then fail "Node doctor accepted missing provider auth"; fi
assert_contains "$output" "missing: OpenCode provider auth in selected Node store" "Node doctor did not expose missing auth"
output="$($ROOT/sex node doctor)" || fail "node doctor"
assert_contains "$output" "ok: OpenCode provider auth in selected Node store" "Node doctor did not check selected auth store"
output="$($ROOT/sex node opencode login)" || fail "explicit Node provider login"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$HOME/.local/share/secretary/node/data/opencode-native|auth login" "Node provider login used the wrong store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$HOME/.local/share/opencode" "Node provider login accessed personal store"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|openai=|aws=" "Node OpenCode subprocess inherited ambient provider credentials"
[[ "$(cat "$HOME/.local/share/opencode/opencode.db")" == "personal-store-canary" ]] || fail "Node setup/login modified personal OpenCode DB"
$ROOT/sex node install-service >/dev/null || fail "node install-service"
node_native="$HOME/.local/share/secretary/node/data/opencode-native/opencode"
[[ "$(cat "$node_native/opencode.db")" == "native-db-initialized" ]] || fail "repeat Node setup reset native DB"
[[ "$(cat "$node_native/auth.json")" == "owner login fixture" ]] || fail "repeat Node setup reset native auth"
[[ "$(cat "$HOME/.local/share/opencode/opencode.db")" == "personal-store-canary" ]] || fail "repeat Node setup modified personal DB"
plist="$HOME/Library/LaunchAgents/dev.secretary.sex-node.plist"
[[ -f "$plist" ]] || fail "missing Node launchd plist"
assert_contains "$(cat "$plist")" '<string>node</string><string>serve</string>' "Node plist does not use foreground serve"
for secret in PAIRING_TOKEN credential SECRETARY_CAPABILITY; do
  assert_not_contains "$(cat "$plist")" "$secret" "Node plist contains credentials: $secret"
done
print "All Node deployment tests passed."
