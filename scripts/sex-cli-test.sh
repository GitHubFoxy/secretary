#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
ROOT="${SCRIPT_DIR:h}"
SEX="$ROOT/sex"
ORIGINAL_HOME="$HOME"
REAL_PATH="${PATH:-/usr/bin:/bin}"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/secretary-sex-test.XXXXXX")"
FAKE_BIN="$TEST_HOME/fake-bin"
STATE="$TEST_HOME/.local/share/secretary"
BIN="$TEST_HOME/.local/bin"
PLIST="$TEST_HOME/Library/LaunchAgents/dev.secretary.sex.plist"
LAUNCHCTL_LOG="$TEST_HOME/launchctl.log"
OPEN_LOG="$TEST_HOME/open.log"

fail() {
  print -u2 -- "FAIL: $1"
  exit 1
}

pass() {
  print -- "ok - $1"
}

assert_contains() {
  local value="$1"
  local expected="$2"
  local message="$3"
  [[ "$value" == *"$expected"* ]] || fail "$message: expected '$expected', got '$value'"
}

assert_not_contains() {
  local value="$1"
  local unexpected="$2"
  local message="$3"
  [[ "$value" != *"$unexpected"* ]] || fail "$message: found '$unexpected' in '$value'"
}

assert_file() {
  local path="$1"
  [[ -f "$path" ]] || fail "missing file: $path"
}

assert_executable() {
  local path="$1"
  [[ -x "$path" ]] || fail "missing executable: $path"
}

assert_http_code() {
  local url="$1"
  local expected="$2"
  local actual
  actual="$(curl -sS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || true)"
  [[ "$actual" == "$expected" ]] || fail "$url: expected HTTP $expected, got $actual"
}

wait_for_server() {
  local url="$1"
  local code
  for _ in {1..80}; do
    code="$(curl -sS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || true)"
    if [[ "$code" == "200" || "$code" == "401" ]]; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

kill_matching_processes() {
  local pattern="$1"
  local candidate
  while IFS= read -r candidate; do
    [[ -n "$candidate" && "$candidate" != "$$" ]] || continue
    kill -TERM "$candidate" 2>/dev/null || true
  done < <(pgrep -f -- "$pattern" 2>/dev/null || true)
  sleep 0.1
  while IFS= read -r candidate; do
    [[ -n "$candidate" && "$candidate" != "$$" ]] || continue
    kill -KILL "$candidate" 2>/dev/null || true
  done < <(pgrep -f -- "$pattern" 2>/dev/null || true)
}

stop_background() {
  local pid="$1" child
  [[ -n "$pid" ]] || return 0
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    kill -TERM "$child" 2>/dev/null || true
  done
  kill -TERM "$pid" 2>/dev/null || true
  set +e
  wait "$pid"
  set -e
}

cleanup() {
  set +e
  if [[ -n "${logs_pid:-}" ]]; then
    kill -TERM -- -"$logs_pid" 2>/dev/null || kill -TERM "$logs_pid" 2>/dev/null || true
    wait "$logs_pid" 2>/dev/null || true
  fi
  if [[ -n "${serve_pid:-}" ]]; then
    kill -TERM -- -"$serve_pid" 2>/dev/null || kill -TERM "$serve_pid" 2>/dev/null || true
    wait "$serve_pid" 2>/dev/null || true
  fi
  kill_matching_processes "$TEST_HOME"
  if [[ -f "$STATE/server.pid" ]]; then
    HOME="$TEST_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SEX" stop >/dev/null 2>&1 || true
  fi
  chmod -R u+w "$TEST_HOME" 2>/dev/null || true
  if [[ "${SEX_TEST_KEEP:-0}" == "1" ]]; then
    print -u2 -- "Keeping test HOME: $TEST_HOME"
  else
    rm -rf "$TEST_HOME"
  fi
}
trap cleanup EXIT

[[ -x "$SEX" ]] || fail "sex is not executable: $SEX"
mkdir -p "$FAKE_BIN"

cat > "$FAKE_BIN/codex" <<'EOF'
#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  exit 0
fi
if [ "$1" = "login" ]; then
  exit 0
fi
exit 0
EOF

cat > "$FAKE_BIN/open" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$OPEN_LOG"
EOF

cat > "$FAKE_BIN/launchctl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$LAUNCHCTL_LOG"
exit 0
EOF

chmod +x "$FAKE_BIN/codex" "$FAKE_BIN/open" "$FAKE_BIN/launchctl"
export HOME="$TEST_HOME"
export PATH="$FAKE_BIN:$REAL_PATH"
export MISE_DATA_DIR="${MISE_DATA_DIR:-$ORIGINAL_HOME/.local/share/mise}"
export GOMODCACHE="${GOMODCACHE:-$ORIGINAL_HOME/go/pkg/mod}"
export GOCACHE="${GOCACHE:-$ORIGINAL_HOME/Library/Caches/go-build}"
export MISE_YES=1
export OPEN_LOG LAUNCHCTL_LOG
TICKET33_OPENCODE_STORE_LOG="$TEST_HOME/opencode-store.log"
export TICKET33_OPENCODE_STORE_LOG
mkdir -p "$TEST_HOME/.local/share/opencode"
printf 'personal-store-canary\n' > "$TEST_HOME/.local/share/opencode/opencode.db"
chmod 600 "$TEST_HOME/.local/share/opencode/opencode.db"
unset SECRETARY_ACP_COMMAND SECRETARY_FX_COMMAND SECRETARY_OPENCODE_COMMAND SECRETARY_CAPABILITY SECRETARY_BOOTSTRAP_TOKEN
export OPENAI_API_KEY=fixture-private-provider-key AWS_ACCESS_KEY_ID=fixture-private-access-id AWS_SECRET_ACCESS_KEY=fixture-private-cloud-key

FAKE_ACP_REAL="$FAKE_BIN/codex-acp-real"
mise exec -- go build -o "$FAKE_ACP_REAL" ./cmd/fake-codex-acp >/dev/null
cat > "$FAKE_BIN/codex-acp" <<EOF
#!/bin/sh
exec "$FAKE_ACP_REAL" "\$@"
EOF
cat > "$FAKE_BIN/fx" <<'EOF'
#!/bin/sh
exec "$(dirname "$0")/codex-acp" "$@"
EOF
cat > "$FAKE_BIN/opencode" <<'EOF'
#!/bin/sh
printf '%s|%s|openai=%s|aws=%s\n' "${XDG_DATA_HOME:-missing}" "$*" "${OPENAI_API_KEY:+present}" "${AWS_ACCESS_KEY_ID:+present}" >> "$TICKET33_OPENCODE_STORE_LOG"
case "$1:$2" in
  --version:*) printf 'opencode v2.0.22\n' ;;
  auth:list)
    mkdir -p "$XDG_DATA_HOME/opencode"
    if [ ! -e "$XDG_DATA_HOME/opencode/opencode.db" ]; then printf 'native-db-initialized\n' > "$XDG_DATA_HOME/opencode/opencode.db"; fi
    if [ "${FAKE_OPENCODE_AUTH_MODE:-ready}" = "missing" ]; then
      printf '[]\n'
    else
      printf 'openai provider authenticated\n'
    fi
    ;;
  auth:login)
    mkdir -p "$XDG_DATA_HOME/opencode"
    printf 'owner login fixture\n' > "$XDG_DATA_HOME/opencode/auth.json"
    printf 'owner login fixture\n'
    ;;
  *) exec "$(dirname "$0")/codex-acp" "$@" ;;
esac
EOF
chmod +x "$FAKE_BIN/codex-acp" "$FAKE_BIN/fx" "$FAKE_BIN/opencode"

print "Testing sex commands in isolated HOME: $TEST_HOME"

output="$($SEX)" || fail "usage command failed"
assert_contains "$output" "Usage: sex" "usage output"
pass "usage"

output="$($SEX status)" || fail "status command failed"
assert_contains "$output" "stopped" "initial status"
pass "status when stopped"

output="$($SEX setup)" || fail "setup command failed"
assert_contains "$output" "Setup complete" "setup output"
assert_executable "$BIN/secretaryd"
assert_executable "$BIN/secretaryctl"
assert_executable "$BIN/secretary-mcp"
assert_file "$STATE/config.toml"
assert_file "$STATE/environment"
assert_file "$STATE/profiles/secretary.md"
assert_file "$STATE/profiles/worker.md"
assert_file "$STATE/profiles/child-worker.md"
assert_contains "$(cat "$STATE/config.toml")" 'harness = "opencode"' "default Secretary harness"
assert_contains "$(cat "$STATE/config.toml")" 'secretary = "openai/gpt-6.1-sol"' "qualified default Secretary model"
assert_contains "$(cat "$STATE/config.toml")" 'reasoning = "xhigh"' "explicit default reasoning"
assert_contains "$(cat "$STATE/config.toml")" 'default_harness = "opencode"' "default Worker harness"
assert_contains "$(cat "$STATE/config.toml")" 'model = "openai/gpt-6-luna"' "qualified default Worker model"
assert_file "$STATE/opencode-native/opencode/opencode.db"
assert_file "$STATE/node/data/opencode-native/opencode/opencode.db"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE/opencode-native|auth list" "Secretary setup did not use its private native store"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE/node/data/opencode-native|auth list" "Node setup did not use its private native store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$TEST_HOME/.local/share/opencode" "setup accessed personal OpenCode store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|auth login" "setup started provider login without owner action"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|openai=|aws=" "OpenCode setup inherited ambient provider credentials"
[[ "$(cat "$TEST_HOME/.local/share/opencode/opencode.db")" == "personal-store-canary" ]] || fail "setup modified personal OpenCode DB"
[[ -L "$BIN/sex" ]] || fail "setup did not install sex symlink"
pass "setup"

if output="$(FAKE_OPENCODE_AUTH_MODE=missing "$SEX" doctor 2>&1)"; then fail "doctor accepted missing provider auth"; fi
assert_contains "$output" "missing: OpenCode provider auth in selected Secretary store" "doctor did not expose missing auth"
assert_not_contains "$output" "personal-store-canary" "doctor exposed personal store data"
output="$($SEX doctor)" || fail "doctor command failed"
assert_contains "$output" "ok: OpenCode provider auth in selected Secretary store" "doctor did not check selected auth store"
pass "doctor"

existing_code="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session 2>/dev/null || true)"
[[ "$existing_code" == "000" ]] || fail "port 8081 is already in use with HTTP $existing_code"

output="$($SEX start)" || fail "start command failed"
assert_contains "$output" "Secretary started" "start output"
assert_not_contains "$output" "code=000" "start readiness output"
status_output="$($SEX status)" || fail "status command failed while running"
assert_contains "$status_output" "mode=normal" "normal running status"
assert_http_code "http://127.0.0.1:8081/control-room" "404"
assert_file "$OPEN_LOG"
assert_contains "$(cat "$OPEN_LOG")" "#bootstrap=" "browser pairing URL"
pass "start in normal mode"

output="$($SEX start)" || fail "start reuse command failed"
assert_contains "$output" "already running" "start reuse output"
pass "start reuses an existing server"

logs_pid=""
$SEX logs >"$TEST_HOME/logs.out" 2>&1 &
logs_pid="$!"
sleep 0.5
stop_background "$logs_pid"
logs_pid=""
assert_file "$TEST_HOME/logs.out"
assert_contains "$(cat "$TEST_HOME/logs.out")" "web API listening" "logs output"
pass "logs"

output="$($SEX stop)" || fail "stop command failed"
assert_contains "$output" "Secretary stopped" "stop output"
output="$($SEX stop)" || fail "stop command failed when already stopped"
assert_contains "$output" "not running" "stopped stop output"
pass "stop"

output="$($SEX restart --debug)" || fail "restart command failed"
assert_contains "$output" "Secretary started" "restart output"
assert_not_contains "$output" "code=000" "restart readiness output"
status_output="$($SEX status)" || fail "status command failed after restart"
assert_contains "$status_output" "mode=debug" "debug running status"
assert_http_code "http://127.0.0.1:8081/control-room" "200"
pass "restart --debug"

output="$($SEX stop)" || fail "stop after restart failed"
assert_contains "$output" "Secretary stopped" "stop after restart output"

output="$($SEX opencode login)" || fail "explicit Secretary provider login failed"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE/opencode-native|auth login" "provider login did not use the selected Secretary store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$TEST_HOME/.local/share/opencode" "provider login accessed personal store"
[[ "$(cat "$TEST_HOME/.local/share/opencode/opencode.db")" == "personal-store-canary" ]] || fail "provider login modified personal OpenCode DB"
pass "explicit Secretary provider login"

output="$($SEX install-service --debug)" || fail "install-service command failed"
assert_contains "$output" "Secretary will start" "install-service output"
[[ "$(cat "$STATE/opencode-native/opencode/opencode.db")" == "native-db-initialized" ]] || fail "repeat setup reset Secretary native DB"
[[ "$(cat "$STATE/opencode-native/opencode/auth.json")" == "owner login fixture" ]] || fail "repeat setup reset Secretary native auth"
assert_file "$PLIST"
assert_contains "$(cat "$PLIST")" "<string>serve</string><string>--debug</string>" "debug launchd plist"
assert_contains "$(cat "$LAUNCHCTL_LOG")" "bootstrap" "launchd bootstrap call"
pass "install-service --debug"

output="$($SEX uninstall-service)" || fail "uninstall-service command failed"
assert_contains "$output" "Service removed" "uninstall-service output"
[[ ! -e "$PLIST" ]] || fail "uninstall-service left plist behind"
assert_contains "$(cat "$LAUNCHCTL_LOG")" "bootout" "launchd bootout call"
pass "uninstall-service"

serve_pid=""
$SEX serve --debug >"$TEST_HOME/serve.out" 2>&1 &
serve_pid="$!"
wait_for_server "http://127.0.0.1:8081/v1/web/session" || fail "serve did not become ready"
status_output="$($SEX status)" || fail "status command failed for serve"
assert_contains "$status_output" "mode=debug" "serve debug status"
assert_http_code "http://127.0.0.1:8081/control-room" "200"
stop_background "$serve_pid"
serve_pid=""
pass "serve --debug"

LEGACY_HOME="$TEST_HOME/legacy-home"
LEGACY_STATE="$LEGACY_HOME/.local/share/secretary"
LEGACY_XDG="$LEGACY_HOME/.local/share"
LEGACY_XDG_NORMALIZED="${LEGACY_XDG:a}"
LEGACY_NODE_DATA="$LEGACY_STATE/node/data"
mkdir -p "$LEGACY_STATE" "$LEGACY_NODE_DATA" "$LEGACY_HOME/.local/bin" "$LEGACY_XDG/opencode"
printf 'existing Secretary state\n' > "$LEGACY_STATE/secretary.db"
printf '{"existing":"node state"}\n' > "$LEGACY_NODE_DATA/node-state.json"
printf 'legacy native sessions\n' > "$LEGACY_XDG/opencode/opencode.db"
chmod 600 "$LEGACY_STATE/secretary.db" "$LEGACY_NODE_DATA/node-state.json" "$LEGACY_XDG/opencode/opencode.db"
output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" "$BIN/secretaryd" --data-dir "$LEGACY_STATE" --select-opencode-stores 2>&1)" || fail "legacy selection command failed"
assert_contains "$output" "legacy native store preserved" "legacy selector output"
assert_contains "$(cat "$LEGACY_STATE/opencode-native-selection.json")" "\"data_home\": \"$LEGACY_XDG_NORMALIZED\"" "Secretary selection did not pin the previous XDG data home"
assert_contains "$(cat "$LEGACY_NODE_DATA/opencode-native-selection.json")" "\"data_home\": \"$LEGACY_XDG_NORMALIZED\"" "local Worker selection did not pin the previous XDG data home"
[[ ! -e "$LEGACY_STATE/opencode-native/opencode" ]] || fail "legacy selection created a replacement Secretary DB"
[[ ! -e "$LEGACY_NODE_DATA/opencode-native/opencode" ]] || fail "legacy selection created a replacement Worker DB"
[[ "$(cat "$LEGACY_XDG/opencode/opencode.db")" == "legacy native sessions" ]] || fail "legacy selection changed existing native DB"
ln -s "$BIN/secretaryd" "$LEGACY_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$LEGACY_HOME/.local/bin/secretary-node"
ln -s "$BIN/secretary-mcp" "$LEGACY_HOME/.local/bin/secretary-mcp"
cp "$STATE/config.toml" "$LEGACY_STATE/config.toml"
cp "$STATE/environment" "$LEGACY_STATE/environment"
cat > "$LEGACY_STATE/node/config.json" <<EOF
{"server_url":"http://127.0.0.1:8081","node":"legacy-node","data_dir":"$LEGACY_NODE_DATA","capacity":1,"include_opencode":true,"workspaces":[]}
EOF
chmod 600 "$LEGACY_STATE/config.toml" "$LEGACY_STATE/environment" "$LEGACY_STATE/node/config.json"
log_lines_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SEX" doctor 2>&1)"; then fail "Secretary doctor accepted an unmigrated legacy store"; fi
assert_contains "$output" "migration required: preserving existing Secretary OpenCode store" "legacy Secretary doctor warning"
if output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SEX" node doctor 2>&1)"; then fail "Node doctor accepted an unmigrated legacy store"; fi
assert_contains "$output" "migration required: preserving existing OpenCode native store" "legacy Node doctor warning"
if HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SEX" opencode login >/dev/null 2>&1; then fail "Secretary login redirected from legacy store"; fi
if HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SEX" node opencode login >/dev/null 2>&1; then fail "Node login redirected from legacy store"; fi
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$log_lines_before" ]] || fail "legacy Doctor/login accessed OpenCode"
[[ "$(cat "$LEGACY_XDG/opencode/opencode.db")" == "legacy native sessions" ]] || fail "legacy Doctor/login changed native DB"
pass "legacy store preservation and owner-approved migration gate"

output="$($SEX status)" || fail "final status command failed"
assert_contains "$output" "stopped" "final status"
pass "final stopped status"

print "All sex CLI command tests passed."
