#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
ROOT="${SCRIPT_DIR:h}"
SECRETARY_CLI="$ROOT/secretary"
ORIGINAL_HOME="$HOME"
REAL_PATH="${PATH:-/usr/bin:/bin}"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/secretary-cli-test.XXXXXX")"
FAKE_BIN="$TEST_HOME/fake-bin"
STATE="$TEST_HOME/.local/share/secretary"
STATE_NORMALIZED="${STATE:a}"
BIN="$TEST_HOME/.local/bin"
PLIST="$TEST_HOME/Library/LaunchAgents/dev.secretary.cli.plist"
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
    HOME="$TEST_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" stop >/dev/null 2>&1 || true
  fi
  chmod -R u+w "$TEST_HOME" 2>/dev/null || true
  if [[ "${SECRETARY_CLI_TEST_KEEP:-0}" == "1" ]]; then
    print -u2 -- "Keeping test HOME: $TEST_HOME"
  else
    rm -rf "$TEST_HOME"
  fi
}
trap cleanup EXIT

[[ -x "$SECRETARY_CLI" ]] || fail "secretary is not executable: $SECRETARY_CLI"
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
  serve:--port)
    if [ "${FAKE_OPENCODE_INIT_MODE:-ok}" = "fail" ]; then exit 23; fi
    mkdir -p "$XDG_DATA_HOME/opencode"
    if [ ! -e "$XDG_DATA_HOME/opencode/opencode.db" ]; then printf 'native-db-initialized\n' > "$XDG_DATA_HOME/opencode/opencode.db"; fi
    ;;
  auth:list)
    if [ "$3" != "--format" ] || [ "$4" != "json" ] || [ "$5" != "--standalone" ]; then
      printf 'background service probe timed out\n' >&2
      exit 124
    fi
    if [ "${FAKE_OPENCODE_AUTH_MODE:-ready}" = "missing" ]; then
      printf '[]\n'
    else
      printf '[{"id":"openai","name":"OpenAI","connections":[{"type":"credential"}]}]\n'
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

print "Testing secretary commands in isolated HOME: $TEST_HOME"

output="$($SECRETARY_CLI)" || fail "usage command failed"
assert_contains "$output" "Usage: secretary" "usage output"
pass "usage"

output="$($SECRETARY_CLI status)" || fail "status command failed"
assert_contains "$output" "stopped" "initial status"
pass "status when stopped"

output="$($SECRETARY_CLI setup)" || fail "setup command failed"
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
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE_NORMALIZED/opencode-native|serve --port 0 --stdio" "setup did not initialize the shared native DB"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE_NORMALIZED/node/data/opencode-native" "local Node setup created a second native store"
[[ "$(grep -c '|serve --port 0 --stdio|' "$TICKET33_OPENCODE_STORE_LOG")" == "1" ]] || fail "fresh setup did not initialize exactly one shared native DB"
output="$($SECRETARY_CLI node setup --name local-node)" || fail "local Node setup failed"
assert_contains "$output" "Node setup complete: local-node" "local Node setup output"
node_manifest="$(cat "$STATE/node/data/opencode-native-selection.json")"
assert_contains "$node_manifest" '"mode": "shared"' "local Node did not keep the shared store selection"
assert_contains "$node_manifest" "\"data_home\": \"$STATE_NORMALIZED/opencode-native\"" "local Node setup diverged from Secretary store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE_NORMALIZED/node/data/opencode-native" "local Node setup initialized a second native store"
[[ "$(stat -f '%Lp' "$STATE/opencode-native")" == "700" ]] || fail "shared native store directory is not mode 0700"
[[ "$(stat -f '%Lp' "$STATE/opencode-native/opencode/opencode.db")" == "600" ]] || fail "shared native DB is not mode 0600"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$TEST_HOME/.local/share/opencode" "setup accessed personal OpenCode store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|auth login" "setup started provider login without owner action"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|openai=|aws=" "OpenCode setup inherited ambient provider credentials"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|auth list|" "setup used auth list as a native DB initialization substitute"
[[ "$(cat "$TEST_HOME/.local/share/opencode/opencode.db")" == "personal-store-canary" ]] || fail "setup modified personal OpenCode DB"
[[ -L "$BIN/secretary" ]] || fail "setup did not install secretary symlink"
pass "setup"

INIT_FAILURE_HOME="$TEST_HOME/native-init-failure-home"
INIT_FAILURE_DB="$INIT_FAILURE_HOME/.local/share/secretary/opencode-native/opencode/opencode.db"
if output="$(HOME="$INIT_FAILURE_HOME" PATH="$FAKE_BIN:$REAL_PATH" FAKE_OPENCODE_INIT_MODE=fail "$SECRETARY_CLI" setup 2>&1)"; then fail "setup hid a native DB initialization failure"; fi
assert_contains "$output" "OpenCode native DB initialization failed; setup stopped" "native initialization failure was not visible"
[[ ! -e "$INIT_FAILURE_DB" ]] || fail "failed native initialization left a purported DB"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "|auth list|" "setup fell back to auth list after initialization failure"
pass "setup fails visibly when native DB initialization fails"

if output="$(FAKE_OPENCODE_AUTH_MODE=missing "$SECRETARY_CLI" doctor 2>&1)"; then fail "doctor accepted missing provider auth"; fi
assert_contains "$output" "missing: OpenCode provider auth in selected Secretary store" "doctor did not expose missing auth"
assert_not_contains "$output" "personal-store-canary" "doctor exposed personal store data"
output="$($SECRETARY_CLI doctor)" || fail "doctor command failed"
assert_contains "$output" "ok: OpenCode provider auth in selected Secretary store" "doctor did not check selected auth store"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE_NORMALIZED/opencode-native|auth list --format json --standalone|openai=|aws=" "Secretary Doctor did not use standalone JSON in the selected store without ambient credentials"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$TEST_HOME/.local/share/opencode|auth list" "Secretary Doctor probed the personal OpenCode store"
output="$($SECRETARY_CLI node doctor)" || fail "Node doctor command failed"
assert_contains "$output" "ok: OpenCode provider auth in selected Node store" "Node doctor did not check the shared auth store"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE_NORMALIZED/opencode-native|auth list --format json --standalone|openai=|aws=" "Node Doctor did not use standalone JSON in the selected store without ambient credentials"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$TEST_HOME/.local/share/opencode|auth list" "Node Doctor probed the personal OpenCode store"
pass "doctor"

existing_code="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session 2>/dev/null || true)"
[[ "$existing_code" == "000" ]] || fail "port 8081 is already in use with HTTP $existing_code"

output="$($SECRETARY_CLI start)" || fail "start command failed"
assert_contains "$output" "Secretary started" "start output"
assert_not_contains "$output" "code=000" "start readiness output"
status_output="$($SECRETARY_CLI status)" || fail "status command failed while running"
assert_contains "$status_output" "mode=normal" "normal running status"
assert_http_code "http://127.0.0.1:8081/control-room" "404"
assert_file "$OPEN_LOG"
assert_contains "$(cat "$OPEN_LOG")" "#bootstrap=" "browser pairing URL"
pass "start in normal mode"

output="$($SECRETARY_CLI start)" || fail "start reuse command failed"
assert_contains "$output" "already running" "start reuse output"
pass "start reuses an existing server"

logs_pid=""
$SECRETARY_CLI logs >"$TEST_HOME/logs.out" 2>&1 &
logs_pid="$!"
sleep 0.5
stop_background "$logs_pid"
logs_pid=""
assert_file "$TEST_HOME/logs.out"
assert_contains "$(cat "$TEST_HOME/logs.out")" "web API listening" "logs output"
pass "logs"

output="$($SECRETARY_CLI stop)" || fail "stop command failed"
assert_contains "$output" "Secretary stopped" "stop output"
output="$($SECRETARY_CLI stop)" || fail "stop command failed when already stopped"
assert_contains "$output" "not running" "stopped stop output"
pass "stop"

output="$($SECRETARY_CLI restart --debug)" || fail "restart command failed"
assert_contains "$output" "Secretary started" "restart output"
assert_not_contains "$output" "code=000" "restart readiness output"
status_output="$($SECRETARY_CLI status)" || fail "status command failed after restart"
assert_contains "$status_output" "mode=debug" "debug running status"
assert_http_code "http://127.0.0.1:8081/control-room" "200"
pass "restart --debug"

output="$($SECRETARY_CLI stop)" || fail "stop after restart failed"
assert_contains "$output" "Secretary stopped" "stop after restart output"

output="$($SECRETARY_CLI opencode login)" || fail "explicit Secretary provider login failed"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$STATE_NORMALIZED/opencode-native|auth login" "provider login did not use the selected Secretary store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$TEST_HOME/.local/share/opencode" "provider login accessed personal store"
[[ "$(cat "$TEST_HOME/.local/share/opencode/opencode.db")" == "personal-store-canary" ]] || fail "provider login modified personal OpenCode DB"
pass "explicit Secretary provider login"

output="$($SECRETARY_CLI install-service --debug)" || fail "install-service command failed"
assert_contains "$output" "Secretary will start" "install-service output"
[[ "$(cat "$STATE/opencode-native/opencode/opencode.db")" == "native-db-initialized" ]] || fail "repeat setup reset Secretary native DB"
[[ "$(cat "$STATE/opencode-native/opencode/auth.json")" == "owner login fixture" ]] || fail "repeat setup reset Secretary native auth"
assert_file "$PLIST"
assert_contains "$(cat "$PLIST")" "<string>serve</string><string>--debug</string>" "debug launchd plist"
assert_contains "$(cat "$LAUNCHCTL_LOG")" "bootstrap" "launchd bootstrap call"
pass "install-service --debug"

output="$($SECRETARY_CLI uninstall-service)" || fail "uninstall-service command failed"
assert_contains "$output" "Service removed" "uninstall-service output"
[[ ! -e "$PLIST" ]] || fail "uninstall-service left plist behind"
assert_contains "$(cat "$LAUNCHCTL_LOG")" "bootout" "launchd bootout call"
pass "uninstall-service"

serve_pid=""
$SECRETARY_CLI serve --debug >"$TEST_HOME/serve.out" 2>&1 &
serve_pid="$!"
wait_for_server "http://127.0.0.1:8081/v1/web/session" || fail "serve did not become ready"
status_output="$($SECRETARY_CLI status)" || fail "status command failed for serve"
assert_contains "$status_output" "mode=debug" "serve debug status"
assert_http_code "http://127.0.0.1:8081/control-room" "200"
stop_background "$serve_pid"
serve_pid=""
pass "serve --debug"

LEGACY_HOME="$TEST_HOME/legacy-home"
LEGACY_STATE="$LEGACY_HOME/.local/share/secretary"
LEGACY_XDG="$LEGACY_HOME/.local/share"
LEGACY_XDG_NORMALIZED="${LEGACY_XDG:a}"
LEGACY_STATE_NORMALIZED="${LEGACY_STATE:a}"
LEGACY_NODE_DATA="$LEGACY_STATE/node/data"
LEGACY_NODE_DATA_NORMALIZED="${LEGACY_NODE_DATA:a}"
mkdir -p "$LEGACY_STATE" "$LEGACY_NODE_DATA" "$LEGACY_HOME/.local/bin" "$LEGACY_XDG/opencode"
printf 'existing Secretary state\n' > "$LEGACY_STATE/secretary.db"
printf '{"existing":"node state"}\n' > "$LEGACY_NODE_DATA/node-state.json"
printf '{"node":"legacy-node","connect_url":"http://127.0.0.1:8081"}\n' > "$LEGACY_NODE_DATA/identity.json"
printf 'legacy native sessions\n' > "$LEGACY_XDG/opencode/opencode.db"
chmod 600 "$LEGACY_STATE/secretary.db" "$LEGACY_NODE_DATA/node-state.json" "$LEGACY_NODE_DATA/identity.json" "$LEGACY_XDG/opencode/opencode.db"
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
python3 - "$LEGACY_STATE/config.toml" <<'PY'
from pathlib import Path
import sys
path = Path(sys.argv[1])
content = path.read_text()
path.write_text(content.replace('harness = "opencode"', 'harness = "fx"', 1))
PY
cp "$STATE/environment" "$LEGACY_STATE/environment"
cat > "$LEGACY_STATE/node/config.json" <<EOF
{
  "server_url": "http://127.0.0.1:8081",
  "node": "legacy-node",
  "data_dir": "$LEGACY_NODE_DATA",
  "capacity": 1,
  "include_opencode": true,
  "workspaces": []
}
EOF
chmod 600 "$LEGACY_STATE/config.toml" "$LEGACY_STATE/environment" "$LEGACY_STATE/node/config.json"
log_lines_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)"; then fail "Secretary doctor accepted an unmigrated legacy store"; fi
assert_contains "$output" "migration required: preserving existing Secretary OpenCode store" "legacy Secretary doctor warning"
if output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)"; then fail "Node doctor accepted an unmigrated legacy store"; fi
assert_contains "$output" "migration required: preserving existing OpenCode native store" "legacy Node doctor warning"
if HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login >/dev/null 2>&1; then fail "Secretary login redirected from legacy store"; fi
if HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login >/dev/null 2>&1; then fail "Node login redirected from legacy store"; fi
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$log_lines_before" ]] || fail "legacy Doctor/login accessed OpenCode"
[[ "$(cat "$LEGACY_XDG/opencode/opencode.db")" == "legacy native sessions" ]] || fail "legacy Doctor/login changed native DB"

output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)" || fail "owner could not select the shared store for an FX-only legacy installation"
assert_contains "$output" "shared Secretary and local Node store selected" "owner selection output"
shared_manifest="$(cat "$LEGACY_STATE/opencode-native-selection.json")"
node_shared_manifest="$(cat "$LEGACY_NODE_DATA/opencode-native-selection.json")"
assert_contains "$shared_manifest" "\"data_home\": \"$LEGACY_STATE_NORMALIZED/opencode-native\"" "Secretary did not select the new shared store"
assert_contains "$node_shared_manifest" "\"mode\": \"shared\"" "local Node did not record the shared selection"
assert_contains "$node_shared_manifest" "\"data_home\": \"$LEGACY_STATE_NORMALIZED/opencode-native\"" "local Node selected a different store"
[[ "$(cat "$LEGACY_XDG/opencode/opencode.db")" == "legacy native sessions" ]] || fail "owner selection changed the legacy OpenCode DB"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$log_lines_before" ]] || fail "owner selection opened OpenCode before explicit login"
before_idempotent_selection="$(cat "$LEGACY_STATE/opencode-native-selection.json")|$(cat "$LEGACY_NODE_DATA/opencode-native-selection.json")"
output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)" || fail "repeating owner selection was not idempotent"
assert_contains "$output" "shared Secretary and local Node store selected" "repeated owner selection output"
after_idempotent_selection="$(cat "$LEGACY_STATE/opencode-native-selection.json")|$(cat "$LEGACY_NODE_DATA/opencode-native-selection.json")"
[[ "$before_idempotent_selection" == "$after_idempotent_selection" ]] || fail "repeating owner selection changed the selection manifests"
output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login 2>&1)" || fail "owner could not log in to the selected shared store"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$LEGACY_STATE_NORMALIZED/opencode-native|auth login" "owner login did not use the selected shared store"
[[ "$(cat "$LEGACY_XDG/opencode/opencode.db")" == "legacy native sessions" ]] || fail "owner login changed the legacy OpenCode DB"
assert_contains "$(cat "$LEGACY_STATE/config.toml")" 'harness = "fx"' "shared-store preparation changed the active FX harness"
output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)" || fail "FX doctor failed while preparing shared OpenCode auth"
assert_contains "$output" "ok: fx" "FX doctor no longer recognizes the selected harness"
output="$(HOME="$LEGACY_HOME" XDG_DATA_HOME="$LEGACY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)" || fail "Node doctor did not accept the shared auth store: $output"
assert_contains "$output" "ok: OpenCode provider auth in selected Node store" "Node doctor did not see the owner-provisioned shared auth"
pass "legacy preservation, explicit shared-store selection, owner login, and FX continuity"

FX_ONLY_HOME="$TEST_HOME/config-only-fx-home"
FX_ONLY_STATE="$FX_ONLY_HOME/.local/share/secretary"
FX_ONLY_XDG="$FX_ONLY_HOME/.local/share"
mkdir -p "$FX_ONLY_HOME/.local/bin" "$FX_ONLY_STATE" "$FX_ONLY_XDG/opencode"
ln -s "$BIN/secretaryd" "$FX_ONLY_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$FX_ONLY_HOME/.local/bin/secretary-node"
printf '[runtime]\nharness = "fx"\n' > "$FX_ONLY_STATE/config.toml"
printf 'old FX-only host canary\n' > "$FX_ONLY_XDG/opencode/opencode.db"
chmod 600 "$FX_ONLY_STATE/config.toml" "$FX_ONLY_XDG/opencode/opencode.db"
fx_only_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$FX_ONLY_HOME" XDG_DATA_HOME="$FX_ONLY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)"; then fail "Doctor accepted a config-only FX installation without owner transition"; fi
assert_contains "$output" "migration required: preserving existing Secretary OpenCode store" "config-only FX Doctor did not require owner transition"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$fx_only_log_before" ]] || fail "config-only FX Doctor opened OpenCode"
output="$(HOME="$FX_ONLY_HOME" XDG_DATA_HOME="$FX_ONLY_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)" || fail "owner transition failed for config-only FX installation"
assert_contains "$output" "shared Secretary and local Node store selected" "config-only FX owner transition output"
assert_contains "$(cat "$FX_ONLY_STATE/opencode-native-selection.json")" '"mode": "shared"' "config-only FX Secretary did not select shared store"
assert_contains "$(cat "$FX_ONLY_STATE/node/data/opencode-native-selection.json")" '"mode": "shared"' "config-only FX Node did not select shared store"
[[ "$(cat "$FX_ONLY_XDG/opencode/opencode.db")" == "old FX-only host canary" ]] || fail "config-only FX transition changed old native data"
assert_contains "$(cat "$FX_ONLY_STATE/config.toml")" 'harness = "fx"' "config-only FX transition changed the active harness"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$fx_only_log_before" ]] || fail "config-only FX transition opened OpenCode before login"
pass "config-only FX install requires explicit shared-store owner transition"

UNTRUSTED_HOME="$TEST_HOME/untrusted-store-home"
UNTRUSTED_STATE="$UNTRUSTED_HOME/.local/share/secretary"
UNTRUSTED_XDG="$UNTRUSTED_HOME/personal/opencode-native"
UNTRUSTED_NODE_DATA="$UNTRUSTED_STATE/node/data"
mkdir -p "$UNTRUSTED_HOME/.local/bin" "$UNTRUSTED_XDG/opencode" "$UNTRUSTED_NODE_DATA"
ln -s "$BIN/secretaryd" "$UNTRUSTED_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$UNTRUSTED_HOME/.local/bin/secretary-node"
ln -s "$BIN/secretary-mcp" "$UNTRUSTED_HOME/.local/bin/secretary-mcp"
printf '[runtime]\nharness = "opencode"\n' > "$UNTRUSTED_STATE/config.toml"
printf 'personal DB canary\n' > "$UNTRUSTED_XDG/opencode/opencode.db"
printf '{\n  "version": 1,\n  "mode": "shared",\n  "data_home": "%s"\n}\n' "${UNTRUSTED_XDG:a}" > "$UNTRUSTED_STATE/opencode-native-selection.json"
printf '{\n  "version": 1,\n  "mode": "shared",\n  "data_home": "%s"\n}\n' "${UNTRUSTED_XDG:a}" > "$UNTRUSTED_NODE_DATA/opencode-native-selection.json"
cat > "$UNTRUSTED_STATE/node/config.json" <<EOF
{
  "server_url": "http://127.0.0.1:8081",
  "node": "untrusted-node",
  "data_dir": "$UNTRUSTED_NODE_DATA",
  "capacity": 1,
  "include_opencode": true,
  "workspaces": []
}
EOF
chmod 600 "$UNTRUSTED_STATE/config.toml" "$UNTRUSTED_STATE/opencode-native-selection.json" "$UNTRUSTED_NODE_DATA/opencode-native-selection.json" "$UNTRUSTED_STATE/node/config.json" "$UNTRUSTED_XDG/opencode/opencode.db"
untrusted_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$UNTRUSTED_HOME" XDG_DATA_HOME="$UNTRUSTED_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login 2>&1)"; then fail "login accepted a shared manifest pointing at personal XDG data"; fi
assert_contains "$output" "store selection is invalid" "untrusted Secretary login refusal was unclear"
if output="$(HOME="$UNTRUSTED_HOME" XDG_DATA_HOME="$UNTRUSTED_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then fail "Node login accepted a shared manifest pointing at personal XDG data"; fi
assert_contains "$output" "store selection is invalid" "untrusted Node login refusal was unclear"
if output="$(HOME="$UNTRUSTED_HOME" XDG_DATA_HOME="$UNTRUSTED_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)"; then fail "Doctor accepted shared manifests pointing at personal XDG data"; fi
assert_contains "$output" "store selection" "untrusted store Doctor refusal was unclear"
if output="$(HOME="$UNTRUSTED_HOME" XDG_DATA_HOME="$UNTRUSTED_XDG" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner selection accepted conflicting personal-store manifests"; fi
assert_contains "$output" "conflicting native store selection" "owner selection conflict refusal was unclear"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$untrusted_log_before" ]] || fail "Doctor/login opened the untrusted native store"
[[ "$(cat "$UNTRUSTED_XDG/opencode/opencode.db")" == "personal DB canary" && ! -e "$UNTRUSTED_XDG/opencode/auth.json" ]] || fail "untrusted manifest touched the personal store"
pass "Doctor/login/owner selection validate shared manifests before native CLI"

DUPLICATE_HOME="$TEST_HOME/duplicate key home"
DUPLICATE_STATE="$DUPLICATE_HOME/.local/share/secretary"
DUPLICATE_NODE_DATA="$DUPLICATE_STATE/node/data"
DUPLICATE_SHARED="${DUPLICATE_STATE:a}/opencode-native"
DUPLICATE_PERSONAL="$DUPLICATE_HOME/personal/opencode-native"
mkdir -p "$DUPLICATE_HOME/.local/bin" "$DUPLICATE_NODE_DATA" "$DUPLICATE_PERSONAL/opencode"
ln -s "$BIN/secretaryd" "$DUPLICATE_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$DUPLICATE_HOME/.local/bin/secretary-node"
printf '[runtime]\nharness = "opencode"\n' > "$DUPLICATE_STATE/config.toml"
cat > "$DUPLICATE_STATE/node/config.json" <<EOF
{
  "server_url": "http://127.0.0.1:8081",
  "node": "duplicate-key-node",
  "data_dir": "$DUPLICATE_NODE_DATA",
  "capacity": 1,
  "include_opencode": true,
  "standalone": false,
  "workspaces": []
}
EOF
cat > "$DUPLICATE_STATE/opencode-native-selection.json" <<EOF
{
  "version": 1,
  "mode": "shared",
  "data_home": "$DUPLICATE_PERSONAL",
  "data_home": "$DUPLICATE_SHARED"
}
EOF
cat > "$DUPLICATE_NODE_DATA/opencode-native-selection.json" <<EOF
{
  "version": 1,
  "mode": "shared",
  "data_home": "$DUPLICATE_PERSONAL",
  "data_home": "$DUPLICATE_SHARED"
}
EOF
printf 'personal duplicate-key canary\n' > "$DUPLICATE_PERSONAL/opencode/opencode.db"
chmod 600 "$DUPLICATE_STATE/config.toml" "$DUPLICATE_STATE/node/config.json" "$DUPLICATE_STATE/opencode-native-selection.json" "$DUPLICATE_NODE_DATA/opencode-native-selection.json" "$DUPLICATE_PERSONAL/opencode/opencode.db"
duplicate_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
duplicate_secretary_manifest="$(cat "$DUPLICATE_STATE/opencode-native-selection.json")"
duplicate_node_manifest="$(cat "$DUPLICATE_NODE_DATA/opencode-native-selection.json")"
if output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" setup 2>&1)"; then fail "setup accepted duplicate data_home keys"; fi
assert_contains "$output" "store selection is invalid" "duplicate-key setup refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node setup 2>&1)"; then fail "co-located Node setup accepted duplicate data_home keys"; fi
assert_contains "$output" "Node OpenCode store selection is invalid" "duplicate-key co-located Node setup refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node setup --standalone 2>&1)"; then fail "standalone Node setup accepted a conflicting duplicate shared selection"; fi
assert_contains "$output" "Node OpenCode store selection is invalid" "duplicate-key standalone Node setup refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" XDG_DATA_HOME="$DUPLICATE_HOME/personal" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login 2>&1)"; then fail "Secretary login accepted duplicate data_home keys and opened the first, personal value"; fi
assert_contains "$output" "store selection is invalid" "duplicate-key Secretary login refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" XDG_DATA_HOME="$DUPLICATE_HOME/personal" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then fail "Node login accepted duplicate data_home keys"; fi
assert_contains "$output" "store selection is invalid" "duplicate-key Node login refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" XDG_DATA_HOME="$DUPLICATE_HOME/personal" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)"; then fail "Secretary Doctor accepted duplicate data_home keys"; fi
assert_contains "$output" "store selection" "duplicate-key Secretary Doctor refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" XDG_DATA_HOME="$DUPLICATE_HOME/personal" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)"; then fail "Node Doctor accepted duplicate data_home keys"; fi
assert_contains "$output" "store selection" "duplicate-key Node Doctor refusal was unclear"
if output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner transition accepted duplicate data_home keys"; fi
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$duplicate_log_before" ]] || fail "duplicate-key CLI flow launched native OpenCode"
[[ "$(cat "$DUPLICATE_PERSONAL/opencode/opencode.db")" == "personal duplicate-key canary" && ! -e "$DUPLICATE_PERSONAL/opencode/auth.json" ]] || fail "duplicate-key CLI flow touched personal store"
[[ "$(cat "$DUPLICATE_STATE/opencode-native-selection.json")" == "$duplicate_secretary_manifest" && "$(cat "$DUPLICATE_NODE_DATA/opencode-native-selection.json")" == "$duplicate_node_manifest" ]] || fail "duplicate-key owner transition rewrote selection manifests"
pass "duplicate selection keys fail closed before setup, Doctor, login, and owner operations"

DEPLOYMENT_DUPLICATE_HOME="$TEST_HOME/deployment duplicate home"
DEPLOYMENT_DUPLICATE_STATE="$DEPLOYMENT_DUPLICATE_HOME/.local/share/secretary"
DEPLOYMENT_DUPLICATE_NODE_DATA="$DEPLOYMENT_DUPLICATE_STATE/node/data"
DEPLOYMENT_DUPLICATE_EXTERNAL="$DEPLOYMENT_DUPLICATE_HOME/external/node/data"
DEPLOYMENT_DUPLICATE_SHARED="$DEPLOYMENT_DUPLICATE_STATE/opencode-native"
DEPLOYMENT_DUPLICATE_LEGACY="$DEPLOYMENT_DUPLICATE_HOME/legacy-xdg"
DEPLOYMENT_DUPLICATE_HOME="${DEPLOYMENT_DUPLICATE_HOME:a}"
DEPLOYMENT_DUPLICATE_STATE="${DEPLOYMENT_DUPLICATE_STATE:a}"
DEPLOYMENT_DUPLICATE_NODE_DATA="${DEPLOYMENT_DUPLICATE_NODE_DATA:a}"
DEPLOYMENT_DUPLICATE_EXTERNAL="${DEPLOYMENT_DUPLICATE_EXTERNAL:a}"
DEPLOYMENT_DUPLICATE_SHARED="${DEPLOYMENT_DUPLICATE_SHARED:a}"
DEPLOYMENT_DUPLICATE_LEGACY="${DEPLOYMENT_DUPLICATE_LEGACY:a}"
mkdir -p "$DEPLOYMENT_DUPLICATE_HOME/.local/bin" "$DEPLOYMENT_DUPLICATE_NODE_DATA" "$DEPLOYMENT_DUPLICATE_EXTERNAL" "$DEPLOYMENT_DUPLICATE_LEGACY/opencode"
ln -s "$BIN/secretaryd" "$DEPLOYMENT_DUPLICATE_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$DEPLOYMENT_DUPLICATE_HOME/.local/bin/secretary-node"
printf '[runtime]\nharness = "opencode"\n' > "$DEPLOYMENT_DUPLICATE_STATE/config.toml"
cat > "$DEPLOYMENT_DUPLICATE_STATE/node/config.json" <<EOF
{
  "server_url": "http://127.0.0.1:8081",
  "node": "deployment-duplicate-node",
  "data_dir": "$DEPLOYMENT_DUPLICATE_EXTERNAL",
  "data_dir": "$DEPLOYMENT_DUPLICATE_NODE_DATA",
  "include_opencode": true,
  "standalone": false,
  "workspaces": []
}
EOF
printf '{\n  "version": 1,\n  "mode": "legacy",\n  "data_home": "%s"\n}\n' "$DEPLOYMENT_DUPLICATE_LEGACY" > "$DEPLOYMENT_DUPLICATE_STATE/opencode-native-selection.json"
printf '{\n  "version": 1,\n  "mode": "shared",\n  "data_home": "%s"\n}\n' "$DEPLOYMENT_DUPLICATE_SHARED" > "$DEPLOYMENT_DUPLICATE_NODE_DATA/opencode-native-selection.json"
printf 'legacy session canary\n' > "$DEPLOYMENT_DUPLICATE_LEGACY/opencode/opencode.db"
printf '{"mappings":{"attempt-a":{"harness_instance_id":"local/opencode","runtime_session_id":"retained-session"}}}\n' > "$DEPLOYMENT_DUPLICATE_NODE_DATA/node-state.json"
chmod 600 "$DEPLOYMENT_DUPLICATE_STATE/config.toml" "$DEPLOYMENT_DUPLICATE_STATE/node/config.json" "$DEPLOYMENT_DUPLICATE_STATE/opencode-native-selection.json" "$DEPLOYMENT_DUPLICATE_NODE_DATA/opencode-native-selection.json" "$DEPLOYMENT_DUPLICATE_NODE_DATA/node-state.json" "$DEPLOYMENT_DUPLICATE_LEGACY/opencode/opencode.db"
deployment_duplicate_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
deployment_duplicate_config_before="$(cat "$DEPLOYMENT_DUPLICATE_STATE/node/config.json")"
deployment_duplicate_secretary_manifest_before="$(cat "$DEPLOYMENT_DUPLICATE_STATE/opencode-native-selection.json")"
deployment_duplicate_node_manifest_before="$(cat "$DEPLOYMENT_DUPLICATE_NODE_DATA/opencode-native-selection.json")"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then
  fail "Node login bypassed the local Secretary pair check through duplicate data_dir"
fi
assert_contains "$output" "store selection is invalid" "duplicate deployment data_dir refusal was unclear"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node setup 2>&1)"; then fail "Node setup accepted duplicate deployment data_dir"; fi
assert_contains "$output" "Node OpenCode store selection is invalid" "duplicate deployment Node setup refusal was unclear"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" setup 2>&1)"; then fail "Secretary setup accepted duplicate Node deployment data_dir"; fi
assert_contains "$output" "store selection is invalid" "duplicate deployment Secretary setup refusal was unclear"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)"; then fail "Secretary Doctor accepted duplicate Node deployment data_dir"; fi
assert_contains "$output" "store selection" "duplicate deployment Secretary Doctor refusal was unclear"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)"; then fail "Node Doctor accepted duplicate deployment data_dir"; fi
assert_contains "$output" "store selection" "duplicate deployment Node Doctor refusal was unclear"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login 2>&1)"; then fail "Secretary login accepted duplicate Node deployment data_dir"; fi
assert_contains "$output" "store selection is invalid" "duplicate deployment Secretary login refusal was unclear"
if output="$(HOME="$DEPLOYMENT_DUPLICATE_HOME" XDG_DATA_HOME="$DEPLOYMENT_DUPLICATE_LEGACY" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner transition accepted duplicate Node deployment data_dir"; fi
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$deployment_duplicate_log_before" ]] || fail "duplicate deployment data_dir launched native CLI"
[[ "$(cat "$DEPLOYMENT_DUPLICATE_LEGACY/opencode/opencode.db")" == "legacy session canary" ]] || fail "duplicate deployment config changed legacy native history"
[[ "$(cat "$DEPLOYMENT_DUPLICATE_NODE_DATA/node-state.json")" == '{"mappings":{"attempt-a":{"harness_instance_id":"local/opencode","runtime_session_id":"retained-session"}}}' ]] || fail "duplicate deployment config changed managed session mappings"
[[ "$(cat "$DEPLOYMENT_DUPLICATE_STATE/node/config.json")" == "$deployment_duplicate_config_before" && "$(cat "$DEPLOYMENT_DUPLICATE_STATE/opencode-native-selection.json")" == "$deployment_duplicate_secretary_manifest_before" && "$(cat "$DEPLOYMENT_DUPLICATE_NODE_DATA/opencode-native-selection.json")" == "$deployment_duplicate_node_manifest_before" ]] || fail "duplicate deployment config refusal rewrote config or store selections"
pass "duplicate deployment data_dir fails closed across setup, Doctor, login, and owner operations"

UTF8_HOME="$TEST_HOME/invalid-utf8-deployment"
UTF8_HOME="${UTF8_HOME:a}"
UTF8_STATE="$UTF8_HOME/.local/share/secretary"
UTF8_CONFIG="$UTF8_STATE/node/config.json"
UTF8_BAD_DATA_DIR="$UTF8_STATE/node-INVALID/data"
UTF8_LOSSY_DATA_DIR="$(python3 -c 'import sys; sys.stdout.write(sys.argv[1].replace("INVALID", "\ufffd"))' "$UTF8_BAD_DATA_DIR")"
mkdir -p "$UTF8_HOME/.local/bin" "$UTF8_STATE/node"
ln -s "$BIN/secretary-node" "$UTF8_HOME/.local/bin/secretary-node"
python3 - "$UTF8_CONFIG" "$UTF8_BAD_DATA_DIR" <<'PY'
import sys
raw_data_dir = sys.argv[2].encode("utf-8").replace(b"INVALID", b"\xff")
assert b"\xff" in raw_data_dir
payload = (
    b'{"server_url":"http://127.0.0.1:8081","node":"invalid-utf8-node","data_dir":"'
    + raw_data_dir
    + b'","include_opencode":true,"standalone":false}'
)
with open(sys.argv[1], "wb") as config:
    config.write(payload)
PY
if output="$(HOME="$UTF8_HOME" XDG_DATA_HOME="$UTF8_HOME/legacy" PATH="$FAKE_BIN:$REAL_PATH" "$BIN/secretary-node" --config "$UTF8_CONFIG" --select-opencode-store --print-opencode-store 2>&1)"; then
  assert_contains "$output" "$UTF8_LOSSY_DATA_DIR/opencode-native" "invalid UTF-8 RED did not select the lossy replacement path"
  fail "invalid UTF-8 deployment data_dir was accepted and selected the lossy replacement path"
fi
pass "invalid UTF-8 deployment data_dir is rejected before store selection"
[[ ! -e "$UTF8_LOSSY_DATA_DIR" ]] || fail "invalid UTF-8 deployment data_dir created a lossy replacement directory"
utf8_native_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$UTF8_HOME" XDG_DATA_HOME="$UTF8_HOME/legacy" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then fail "Node login accepted invalid UTF-8 deployment data_dir"; fi
assert_contains "$output" "Node OpenCode store selection is invalid" "invalid UTF-8 deployment login refusal was unclear"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$utf8_native_log_before" ]] || fail "invalid UTF-8 deployment config launched native CLI"

UTF8_MANIFEST_HOME="$TEST_HOME/invalid-utf8-manifest"
UTF8_MANIFEST_HOME="${UTF8_MANIFEST_HOME:a}"
UTF8_MANIFEST_STATE="$UTF8_MANIFEST_HOME/.local/share/secretary"
UTF8_MANIFEST_CONFIG="$UTF8_MANIFEST_STATE/node/config.json"
UTF8_MANIFEST_DATA_DIR="$UTF8_MANIFEST_STATE/node/data"
UTF8_MANIFEST_PATH="$UTF8_MANIFEST_DATA_DIR/opencode-native-selection.json"
UTF8_MANIFEST_BAD_HOME="$UTF8_MANIFEST_HOME/legacy-INVALID/opencode-native"
UTF8_MANIFEST_LOSSY_HOME="$(python3 -c 'import sys; sys.stdout.write(sys.argv[1].replace("INVALID", "\ufffd"))' "$UTF8_MANIFEST_BAD_HOME")"
mkdir -p "$UTF8_MANIFEST_HOME/.local/bin" "$UTF8_MANIFEST_DATA_DIR"
ln -s "$BIN/secretary-node" "$UTF8_MANIFEST_HOME/.local/bin/secretary-node"
printf '{"server_url":"http://127.0.0.1:8081","node":"invalid-utf8-manifest","data_dir":"%s","include_opencode":true,"standalone":false}\n' "$UTF8_MANIFEST_DATA_DIR" > "$UTF8_MANIFEST_CONFIG"
for invalid_unicode in raw-byte unpaired-high unpaired-low; do
  python3 - "$UTF8_MANIFEST_PATH" "$invalid_unicode" "$UTF8_MANIFEST_BAD_HOME" <<'PY'
import sys
manifest_path, malformed, data_home = sys.argv[1:]
encoded_home = data_home.encode("utf-8")
if malformed == "raw-byte":
    encoded_home = encoded_home.replace(b"INVALID", b"\xff")
elif malformed == "unpaired-high":
    encoded_home = encoded_home.replace(b"INVALID", b"\\uD800")
else:
    encoded_home = encoded_home.replace(b"INVALID", b"\\uDC00")
payload = b'{"version":1,"mode":"legacy","data_home":"' + encoded_home + b'"}'
with open(manifest_path, "wb") as manifest:
    manifest.write(payload)
PY
  if output="$(HOME="$UTF8_MANIFEST_HOME" XDG_DATA_HOME="$UTF8_MANIFEST_HOME/legacy-xdg" PATH="$FAKE_BIN:$REAL_PATH" "$BIN/secretary-node" --config "$UTF8_MANIFEST_CONFIG" --select-opencode-store --print-opencode-store 2>&1)"; then
    assert_contains "$output" "$UTF8_MANIFEST_LOSSY_HOME" "malformed manifest RED did not resolve to its lossy path ($invalid_unicode)"
    fail "Node manifest accepted lossy Unicode ($invalid_unicode)"
  fi
  assert_contains "$output" "invalid native store selection" "malformed manifest refusal was unclear ($invalid_unicode)"
  [[ ! -e "$UTF8_MANIFEST_LOSSY_HOME" ]] || fail "malformed manifest created the lossy legacy path ($invalid_unicode)"
  utf8_manifest_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
  if output="$(HOME="$UTF8_MANIFEST_HOME" XDG_DATA_HOME="$UTF8_MANIFEST_HOME/legacy-xdg" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then fail "Node login accepted malformed manifest ($invalid_unicode)"; fi
  assert_contains "$output" "Node OpenCode store selection is invalid" "malformed manifest login refusal was unclear ($invalid_unicode)"
  [[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$utf8_manifest_log_before" ]] || fail "malformed manifest launched native CLI ($invalid_unicode)"
done
pass "invalid UTF-8 and unpaired surrogate manifests fail closed before Node native CLI"

UTF8_UNICODE_HOME="$TEST_HOME/valid-unicode-path"
UTF8_UNICODE_HOME="${UTF8_UNICODE_HOME:a}"
UTF8_UNICODE_STATE="$UTF8_UNICODE_HOME/.local/share/secretary"
UTF8_UNICODE_CONFIG="$UTF8_UNICODE_STATE/node/config.json"
UTF8_UNICODE_DATA_DIR="$(python3 -c 'import sys; sys.stdout.write(sys.argv[1] + "/node-\u00df-\U0001f600/data")' "$UTF8_UNICODE_STATE")"
UTF8_UNICODE_STORE="$UTF8_UNICODE_DATA_DIR/opencode-native"
mkdir -p "$UTF8_UNICODE_HOME/.local/bin" "$UTF8_UNICODE_STATE/node"
ln -s "$BIN/secretary-node" "$UTF8_UNICODE_HOME/.local/bin/secretary-node"
python3 - "$UTF8_UNICODE_CONFIG" "$UTF8_UNICODE_DATA_DIR" <<'PY'
import json
import sys
config = {"server_url": "http://127.0.0.1:8081", "node": "valid-unicode-node", "data_dir": sys.argv[2], "include_opencode": True, "standalone": False}
with open(sys.argv[1], "w", encoding="utf-8") as output:
    json.dump(config, output, ensure_ascii=True)
PY
if output="$(HOME="$UTF8_UNICODE_HOME" XDG_DATA_HOME="$UTF8_UNICODE_HOME/legacy" PATH="$FAKE_BIN:$REAL_PATH" "$BIN/secretary-node" --config "$UTF8_UNICODE_CONFIG" --select-opencode-store --print-opencode-store 2>&1)"; then
  assert_contains "$output" "$UTF8_UNICODE_STORE" "valid paired-surrogate deployment path changed during selection"
else
  fail "valid Unicode deployment path was rejected: $output"
fi
for unicode_manifest in raw paired; do
  python3 - "$UTF8_UNICODE_DATA_DIR/opencode-native-selection.json" "$UTF8_UNICODE_STORE" "$unicode_manifest" <<'PY'
import json
import sys
manifest = {"version": 1, "mode": "isolated", "data_home": sys.argv[2]}
with open(sys.argv[1], "w", encoding="utf-8") as output:
    json.dump(manifest, output, ensure_ascii=(sys.argv[3] == "paired"))
PY
  if output="$(HOME="$UTF8_UNICODE_HOME" XDG_DATA_HOME="$UTF8_UNICODE_HOME/legacy" PATH="$FAKE_BIN:$REAL_PATH" "$BIN/secretary-node" --config "$UTF8_UNICODE_CONFIG" --select-opencode-store --print-opencode-store 2>&1)"; then
    assert_contains "$output" "$UTF8_UNICODE_STORE" "valid Unicode manifest path changed during selection ($unicode_manifest)"
  else
    fail "valid Unicode manifest was rejected ($unicode_manifest): $output"
  fi
done
pass "raw Unicode and valid paired surrogate escapes preserve canonical Node paths"

PAIR_HOME="$TEST_HOME/co-located-store-conflict"
PAIR_STATE="$PAIR_HOME/.local/share/secretary"
PAIR_NODE_DATA="$PAIR_STATE/node/data"
PAIR_SECRETARY_STORE="$PAIR_STATE/opencode-native"
PAIR_NODE_STORE="$PAIR_NODE_DATA/opencode-native"
PAIR_PERSONAL_STORE="$PAIR_HOME/.local/share/opencode"
PAIR_HOME="${PAIR_HOME:a}"
PAIR_STATE="${PAIR_STATE:a}"
PAIR_NODE_DATA="${PAIR_NODE_DATA:a}"
PAIR_SECRETARY_STORE="${PAIR_SECRETARY_STORE:a}"
PAIR_NODE_STORE="${PAIR_NODE_STORE:a}"
PAIR_PERSONAL_STORE="${PAIR_PERSONAL_STORE:a}"
mkdir -p "$PAIR_HOME/.local/bin" "$PAIR_NODE_DATA" "$PAIR_SECRETARY_STORE/opencode" "$PAIR_NODE_STORE/opencode" "$PAIR_PERSONAL_STORE"
ln -s "$BIN/secretaryd" "$PAIR_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$PAIR_HOME/.local/bin/secretary-node"
printf '[runtime]\nharness = "opencode"\n' > "$PAIR_STATE/config.toml"
cat > "$PAIR_STATE/node/config.json" <<EOF
{"server_url":"http://127.0.0.1:8081","node":"pair-check-node","data_dir":"$PAIR_NODE_DATA","include_opencode":true,"standalone":false}
EOF
printf '{"version":1,"mode":"isolated","data_home":"%s"}\n' "$PAIR_SECRETARY_STORE" > "$PAIR_STATE/opencode-native-selection.json"
printf '{"version":1,"mode":"isolated","data_home":"%s"}\n' "$PAIR_NODE_STORE" > "$PAIR_NODE_DATA/opencode-native-selection.json"
printf 'secretary native canary\n' > "$PAIR_SECRETARY_STORE/opencode/opencode.db"
printf 'node native canary\n' > "$PAIR_NODE_STORE/opencode/opencode.db"
printf 'personal native canary\n' > "$PAIR_PERSONAL_STORE/opencode.db"
chmod 600 "$PAIR_STATE/config.toml" "$PAIR_STATE/node/config.json" "$PAIR_STATE/opencode-native-selection.json" "$PAIR_NODE_DATA/opencode-native-selection.json" "$PAIR_SECRETARY_STORE/opencode/opencode.db" "$PAIR_NODE_STORE/opencode/opencode.db" "$PAIR_PERSONAL_STORE/opencode.db"
pair_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$PAIR_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then fail "co-located Node login ignored conflicting Secretary store"; fi
assert_contains "$output" "Co-located Secretary/Node OpenCode store selection is invalid" "co-located Node login conflict was unclear"
if output="$(HOME="$PAIR_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)"; then fail "co-located Node Doctor ignored conflicting Secretary store"; fi
assert_contains "$output" "invalid: Node OpenCode store selection" "co-located Node Doctor conflict was unclear"
if output="$(HOME="$PAIR_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node setup 2>&1)"; then fail "co-located Node setup ignored conflicting Secretary store"; fi
assert_contains "$output" "Co-located Secretary/Node OpenCode store selection is invalid" "co-located Node setup conflict was unclear"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$pair_log_before" ]] || fail "co-located conflict invoked native OpenCode"
[[ "$(cat "$PAIR_NODE_STORE/opencode/opencode.db")" == "node native canary" ]] || fail "co-located conflict changed the Node store"
rm -f "$PAIR_NODE_STORE/opencode/opencode.db"
[[ ! -e "$PAIR_NODE_STORE/opencode/opencode.db" ]] || fail "could not clear the private Node DB before setup acceptance"
cat > "$PAIR_STATE/node/config.json" <<EOF
{"server_url":"http://127.0.0.1:8081","node":"pair-check-node","data_dir":"$PAIR_NODE_DATA","include_opencode":true,"standalone":true}
EOF
output="$(HOME="$PAIR_HOME" PATH="$FAKE_BIN:$REAL_PATH" SECRETARY_OPENCODE_COMMAND="$FAKE_BIN/opencode" "$SECRETARY_CLI" node setup --standalone --name pair-check-node 2>&1)" || fail "standalone Node setup was not independent of Secretary: $output"
assert_contains "$output" "Node setup complete: pair-check-node" "standalone Node setup output was unclear"
assert_contains "$output" "OpenCode Node native store: $PAIR_NODE_STORE" "standalone setup selected an unexpected native store"
output="$(HOME="$PAIR_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)" || fail "standalone Node login was not independent of Secretary: $output"
output="$(HOME="$PAIR_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)" || fail "standalone Node Doctor was not independent of Secretary: $output"
assert_contains "$output" "ok: standalone Node uses an independent OpenCode store" "standalone Node Doctor did not report its scope"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$PAIR_NODE_STORE|serve --port 0 --stdio" "standalone setup did not initialize its selected native DB"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$PAIR_NODE_STORE|auth login" "standalone login used a different native store"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$PAIR_PERSONAL_STORE" "standalone Node accessed the personal native store"
[[ "$(cat "$PAIR_PERSONAL_STORE/opencode.db")" == "personal native canary" && "$(cat "$PAIR_SECRETARY_STORE/opencode/opencode.db")" == "secretary native canary" ]] || fail "standalone Node changed Secretary or personal native data"
pass "co-located Node login/Doctor/setup enforce the Secretary pair; standalone remains independent"

DUPLICATE_ESCAPED_SHARED="$(python3 -c 'import sys; print(sys.argv[1].replace("/", r"\u002f"))' "$DUPLICATE_SHARED")"
printf '{\n  "version": 1,\n  "mode": "shared",\n  "data_home": "%s"\n}\n' "$DUPLICATE_ESCAPED_SHARED" > "$DUPLICATE_STATE/opencode-native-selection.json"
printf '{\n  "version": 1,\n  "mode": "shared",\n  "data_home": "%s"\n}\n' "$DUPLICATE_SHARED" > "$DUPLICATE_NODE_DATA/opencode-native-selection.json"
output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" setup 2>&1)" || fail "setup rejected a valid escaped canonical path: $output"
assert_contains "$output" "Setup complete" "setup did not consume validated escaped selection"
output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login 2>&1)" || fail "Secretary login rejected a valid escaped canonical path"
output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)" || fail "Node login rejected a valid escaped canonical path"
output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)" || fail "Secretary Doctor rejected a valid escaped canonical path: $output"
output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)" || fail "Node Doctor rejected a valid escaped canonical path: $output"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$DUPLICATE_SHARED|auth login" "escaped Secretary path was not resolved by Go producer"
assert_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$DUPLICATE_SHARED|auth list --format json --standalone" "escaped Doctor path was not resolved by Go producer"
assert_not_contains "$(cat "$TICKET33_OPENCODE_STORE_LOG")" "$DUPLICATE_PERSONAL" "valid escaped selection accessed the personal store"
[[ "$(cat "$DUPLICATE_PERSONAL/opencode/opencode.db")" == "personal duplicate-key canary" && ! -e "$DUPLICATE_PERSONAL/opencode/auth.json" ]] || fail "valid escaped selection touched personal store"
output="$(HOME="$DUPLICATE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)" || fail "owner transition rejected valid escaped canonical selection"
assert_contains "$output" "shared Secretary and local Node store selected" "owner transition did not normalize the Go-selected store"
pass "setup, Doctor, login, and owner use one Go-resolved escaped canonical store"

write_selection_manifest() {
  local selection_path="$1" selection_mode="$2" selected_home="$3"
  printf '{\n  "version": 1,\n  "mode": "%s",\n  "data_home": "%s"\n}\n' "$selection_mode" "$selected_home" > "$selection_path"
  chmod 600 "$selection_path"
}

for invalid_kind in malformed escaped-duplicate-key malformed-json escaped-control conflict symlink-selection symlink-target; do
  INVALID_HOME="$TEST_HOME/invalid-$invalid_kind-home"
  INVALID_STATE="$INVALID_HOME/.local/share/secretary"
  INVALID_NODE_DATA="$INVALID_STATE/node/data"
  INVALID_SHARED="$INVALID_STATE/opencode-native"
  INVALID_NODE_STORE="$INVALID_NODE_DATA/opencode-native"
  INVALID_PERSONAL="$INVALID_HOME/personal/opencode-native"
  mkdir -p "$INVALID_HOME/.local/bin" "$INVALID_NODE_DATA" "$INVALID_PERSONAL/opencode"
  ln -s "$BIN/secretaryd" "$INVALID_HOME/.local/bin/secretaryd"
  ln -s "$BIN/secretary-node" "$INVALID_HOME/.local/bin/secretary-node"
  ln -s "$BIN/secretary-mcp" "$INVALID_HOME/.local/bin/secretary-mcp"
  printf '[runtime]\nharness = "opencode"\n' > "$INVALID_STATE/config.toml"
  cat > "$INVALID_STATE/node/config.json" <<EOF
{
  "server_url": "http://127.0.0.1:8081",
  "node": "invalid-$invalid_kind",
  "data_dir": "$INVALID_NODE_DATA",
  "capacity": 1,
  "include_opencode": true,
  "standalone": false,
  "workspaces": []
}
EOF
  printf 'personal DB canary\n' > "$INVALID_PERSONAL/opencode/opencode.db"
  case "$invalid_kind" in
    malformed)
      write_selection_manifest "$INVALID_STATE/opencode-native-selection.json" malformed "$INVALID_SHARED"
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" shared "$INVALID_SHARED"
      ;;
    escaped-duplicate-key)
      cat > "$INVALID_STATE/opencode-native-selection.json" <<EOF
{
  "version": 1,
  "mode": "shared",
  "data_home": "$INVALID_PERSONAL",
  "data\u005fhome": "$INVALID_SHARED"
}
EOF
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" shared "$INVALID_SHARED"
      ;;
    malformed-json)
      cat > "$INVALID_STATE/opencode-native-selection.json" <<'EOF'
{"version":1,"mode":"shared","data_home":"\uZZZZ"}
EOF
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" shared "$INVALID_SHARED"
      ;;
    escaped-control)
      printf '{"version":1,"mode":"shared","data_home":"%s\\u000a"}\n' "$INVALID_SHARED" > "$INVALID_STATE/opencode-native-selection.json"
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" shared "$INVALID_SHARED"
      ;;
    conflict)
      write_selection_manifest "$INVALID_STATE/opencode-native-selection.json" shared "$INVALID_SHARED"
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" isolated "$INVALID_NODE_STORE"
      ;;
    symlink-selection)
      write_selection_manifest "$INVALID_HOME/personal-selection.json" shared "$INVALID_PERSONAL"
      ln -s "$INVALID_HOME/personal-selection.json" "$INVALID_STATE/opencode-native-selection.json"
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" shared "$INVALID_SHARED"
      ;;
    symlink-target)
      ln -s "$INVALID_PERSONAL" "$INVALID_SHARED"
      write_selection_manifest "$INVALID_STATE/opencode-native-selection.json" shared "$INVALID_SHARED"
      write_selection_manifest "$INVALID_NODE_DATA/opencode-native-selection.json" shared "$INVALID_SHARED"
      ;;
  esac
  invalid_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
  if output="$(HOME="$INVALID_HOME" XDG_DATA_HOME="$INVALID_PERSONAL" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode login 2>&1)"; then fail "$invalid_kind manifest passed Secretary login validation"; fi
  assert_contains "$output" "store selection is invalid" "$invalid_kind Secretary login refusal was unclear"
  if output="$(HOME="$INVALID_HOME" XDG_DATA_HOME="$INVALID_PERSONAL" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node opencode login 2>&1)"; then fail "$invalid_kind manifest passed Node login validation"; fi
  assert_contains "$output" "native CLI was not launched" "$invalid_kind Node login refusal was unclear"
  if output="$(HOME="$INVALID_HOME" XDG_DATA_HOME="$INVALID_PERSONAL" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" doctor 2>&1)"; then fail "$invalid_kind manifest passed Doctor validation"; fi
  assert_contains "$output" "store selection" "$invalid_kind Doctor refusal was unclear"
  if output="$(HOME="$INVALID_HOME" XDG_DATA_HOME="$INVALID_PERSONAL" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "$invalid_kind manifest passed owner validation"; fi
  [[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$invalid_log_before" ]] || fail "$invalid_kind manifest launched OpenCode CLI"
  [[ "$(cat "$INVALID_PERSONAL/opencode/opencode.db")" == "personal DB canary" && ! -e "$INVALID_PERSONAL/opencode/auth.json" ]] || fail "$invalid_kind manifest modified the personal store"
done
pass "malformed, conflicting, and symlink selections fail closed before CLI operations"

STANDALONE_CONFLICT_HOME="$TEST_HOME/standalone-shared-conflict-home"
STANDALONE_CONFLICT_STATE="$STANDALONE_CONFLICT_HOME/.local/share/secretary"
STANDALONE_CONFLICT_NODE_DATA="$STANDALONE_CONFLICT_STATE/node/data"
STANDALONE_CONFLICT_STORE="$STANDALONE_CONFLICT_STATE/opencode-native"
mkdir -p "$STANDALONE_CONFLICT_HOME/.local/bin" "$STANDALONE_CONFLICT_NODE_DATA" "$STANDALONE_CONFLICT_STORE/opencode"
ln -s "$BIN/secretary-node" "$STANDALONE_CONFLICT_HOME/.local/bin/secretary-node"
cat > "$STANDALONE_CONFLICT_STATE/node/config.json" <<EOF
{
  "server_url": "https://secretary.example.invalid",
  "node": "standalone-conflict",
  "data_dir": "$STANDALONE_CONFLICT_NODE_DATA",
  "capacity": 1,
  "include_opencode": true,
  "standalone": true,
  "workspaces": []
}
EOF
printf '{\n  "version": 1,\n  "mode": "shared",\n  "data_home": "%s"\n}\n' "${STANDALONE_CONFLICT_STORE:a}" > "$STANDALONE_CONFLICT_NODE_DATA/opencode-native-selection.json"
printf 'selected Secretary DB canary\n' > "$STANDALONE_CONFLICT_STORE/opencode/opencode.db"
chmod 600 "$STANDALONE_CONFLICT_STATE/node/config.json" "$STANDALONE_CONFLICT_NODE_DATA/opencode-native-selection.json" "$STANDALONE_CONFLICT_STORE/opencode/opencode.db"
standalone_log_before="$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")"
if output="$(HOME="$STANDALONE_CONFLICT_HOME" XDG_DATA_HOME="$STANDALONE_CONFLICT_HOME/personal" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node doctor 2>&1)"; then fail "standalone Node Doctor accepted Secretary shared-store selection"; fi
assert_contains "$output" "invalid: Node OpenCode store selection" "standalone Node Doctor conflict refusal was unclear"
[[ "$(wc -l < "$TICKET33_OPENCODE_STORE_LOG")" == "$standalone_log_before" ]] || fail "standalone Node Doctor probed shared Secretary auth"
if output="$(HOME="$STANDALONE_CONFLICT_HOME" "$BIN/secretary-node" --config "$STANDALONE_CONFLICT_STATE/node/config.json" 2>&1)"; then fail "standalone runtime accepted a Secretary shared-store selection"; fi
assert_contains "$output" "standalone Node cannot use the shared Secretary store" "standalone shared-store conflict was not rejected before runtime"
[[ "$(cat "$STANDALONE_CONFLICT_STORE/opencode/opencode.db")" == "selected Secretary DB canary" ]] || fail "standalone conflict modified the selected Secretary DB"
pass "standalone Node runtime rejects a pre-existing shared manifest"

MANAGED_HOME="$TEST_HOME/managed-session-home"
MANAGED_STATE="$MANAGED_HOME/.local/share/secretary"
MANAGED_NODE_DATA="$MANAGED_STATE/node/data"
mkdir -p "$MANAGED_HOME/.local/bin" "$MANAGED_NODE_DATA"
ln -s "$BIN/secretaryd" "$MANAGED_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$MANAGED_HOME/.local/bin/secretary-node"
printf 'existing server state\n' > "$MANAGED_STATE/secretary.db"
printf '{"mappings":{"attempt-1":{"harness_instance_id":"local/opencode","runtime_session_id":"private-session-id"}}}\n' > "$MANAGED_NODE_DATA/node-state.json"
if output="$(HOME="$MANAGED_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner selection replaced a managed OpenCode session"; fi
assert_contains "$output" "managed OpenCode sessions exist in Node mappings" "managed session refusal was unclear"
assert_not_contains "$output" "private-session-id" "managed native session identity leaked to CLI output"
[[ ! -e "$MANAGED_STATE/opencode-native-selection.json" && ! -e "$MANAGED_NODE_DATA/opencode-native-selection.json" ]] || fail "managed-session refusal partially changed selection"
pass "managed OpenCode session blocks owner selection without exposing native IDs"

CONFLICT_HOME="$TEST_HOME/conflicting-selection-home"
CONFLICT_STATE="$CONFLICT_HOME/.local/share/secretary"
CONFLICT_NODE_DATA="$CONFLICT_STATE/node/data"
CONFLICT_SECRETARY_STORE="$CONFLICT_STATE/opencode-native"
CONFLICT_NODE_STORE="$CONFLICT_NODE_DATA/opencode-native"
mkdir -p "$CONFLICT_HOME/.local/bin" "$CONFLICT_NODE_STORE/opencode"
ln -s "$BIN/secretaryd" "$CONFLICT_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$CONFLICT_HOME/.local/bin/secretary-node"
printf '{"version":1,"mode":"isolated","data_home":"%s"}\n' "${CONFLICT_SECRETARY_STORE:a}" > "$CONFLICT_STATE/opencode-native-selection.json"
printf '{"version":1,"mode":"isolated","data_home":"%s"}\n' "${CONFLICT_NODE_STORE:a}" > "$CONFLICT_NODE_DATA/opencode-native-selection.json"
printf 'old selected database\n' > "$CONFLICT_NODE_STORE/opencode/opencode.db"
before_selection="$(shasum -a 256 "$CONFLICT_STATE/opencode-native-selection.json" "$CONFLICT_NODE_DATA/opencode-native-selection.json" "$CONFLICT_NODE_STORE/opencode/opencode.db")"
if output="$(HOME="$CONFLICT_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner selection replaced conflicting role-specific stores"; fi
assert_contains "$output" "Node has a conflicting native store selection" "conflicting selection refusal was unclear"
after_selection="$(shasum -a 256 "$CONFLICT_STATE/opencode-native-selection.json" "$CONFLICT_NODE_DATA/opencode-native-selection.json" "$CONFLICT_NODE_STORE/opencode/opencode.db")"
[[ "$before_selection" == "$after_selection" ]] || fail "conflicting selection failure changed an existing store or manifest"
pass "conflicting nonempty role selection fails closed"

PARTIAL_HOME="$TEST_HOME/partial-selection-home"
PARTIAL_STATE="$PARTIAL_HOME/.local/share/secretary"
PARTIAL_NODE_DATA="$PARTIAL_STATE/node/data"
PARTIAL_LEGACY="$PARTIAL_HOME/.local/share"
PARTIAL_CANARY="$PARTIAL_HOME/private-canary.db"
mkdir -p "$PARTIAL_HOME/.local/bin" "$PARTIAL_NODE_DATA"
ln -s "$BIN/secretaryd" "$PARTIAL_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$PARTIAL_HOME/.local/bin/secretary-node"
printf '{"version":1,"mode":"legacy","data_home":"%s"}\n' "${PARTIAL_LEGACY:a}" > "$PARTIAL_STATE/opencode-native-selection.json"
printf '{"version":1,"mode":"legacy","data_home":"%s"}\n' "${PARTIAL_LEGACY:a}" > "$PARTIAL_NODE_DATA/opencode-native-selection.json"
printf 'must stay private\n' > "$PARTIAL_CANARY"
ln -s "$PARTIAL_CANARY" "$PARTIAL_NODE_DATA/opencode-native-selection.json.tmp"
before_partial="$(shasum -a 256 "$PARTIAL_STATE/opencode-native-selection.json" "$PARTIAL_NODE_DATA/opencode-native-selection.json" "$PARTIAL_CANARY")"
if output="$(HOME="$PARTIAL_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner selection ignored a partial-write obstruction"; fi
assert_contains "$output" "selection is incomplete" "partial selection failure was unclear"
after_partial="$(shasum -a 256 "$PARTIAL_STATE/opencode-native-selection.json" "$PARTIAL_NODE_DATA/opencode-native-selection.json" "$PARTIAL_CANARY")"
[[ "$before_partial" == "$after_partial" ]] || fail "partial selection failure changed old records or canary"
[[ ! -e "$PARTIAL_STATE/opencode-native-selection.json.tmp" ]] || fail "partial selection left a Secretary staging file"
pass "partial owner selection leaves prior records and canary unchanged"

CRASH_HOME="$TEST_HOME/crash-selection-home"
CRASH_STATE="$CRASH_HOME/.local/share/secretary"
CRASH_NODE_DATA="$CRASH_STATE/node/data"
CRASH_SHARED="$CRASH_STATE/opencode-native"
mkdir -p "$CRASH_HOME/.local/bin" "$CRASH_SHARED/opencode" "$CRASH_NODE_DATA"
ln -s "$BIN/secretaryd" "$CRASH_HOME/.local/bin/secretaryd"
ln -s "$BIN/secretary-node" "$CRASH_HOME/.local/bin/secretary-node"
printf '{"version":1,"mode":"shared","data_home":"%s"}\n' "${CRASH_SHARED:a}" > "$CRASH_STATE/opencode-native-selection.json"
printf 'selected native session canary\n' > "$CRASH_SHARED/opencode/opencode.db"
before_crash_repair="$(shasum -a 256 "$CRASH_STATE/opencode-native-selection.json" "$CRASH_SHARED/opencode/opencode.db")"
if output="$(HOME="$CRASH_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" opencode select-shared-store 2>&1)"; then fail "owner command silently completed a partial selection over a nonempty target"; fi
assert_contains "$output" "target native store is nonempty and unselected" "partial active selection refusal was unclear"
after_crash_repair="$(shasum -a 256 "$CRASH_STATE/opencode-native-selection.json" "$CRASH_SHARED/opencode/opencode.db")"
[[ "$before_crash_repair" == "$after_crash_repair" && ! -e "$CRASH_NODE_DATA/opencode-native-selection.json" ]] || fail "partial active selection refusal changed selected data"
pass "partial active selection with nonempty store fails closed"

SYMLINK_ROOT="$TEST_HOME/symlink-install"
SYMLINK_STATE="$SYMLINK_ROOT/secretary"
SYMLINK_PERSONAL="$TEST_HOME/symlink-personal-store"
mkdir -p "$SYMLINK_STATE" "$SYMLINK_PERSONAL/opencode"
printf 'personal store canary\n' > "$SYMLINK_PERSONAL/opencode/opencode.db"
ln -s "$SYMLINK_PERSONAL" "$SYMLINK_STATE/opencode-native"
if output="$(HOME="$TEST_HOME" "$BIN/secretaryd" --data-dir "$SYMLINK_STATE" --select-opencode-stores 2>&1)"; then fail "setup followed a symlink to a personal store"; fi
assert_contains "$output" "shared native store path must not be a symlink" "symlink refusal was unclear"
[[ "$(cat "$SYMLINK_PERSONAL/opencode/opencode.db")" == "personal store canary" ]] || fail "symlink refusal changed personal native data"
[[ ! -e "$SYMLINK_STATE/opencode-native-selection.json" ]] || fail "symlink refusal recorded an unsafe selection"
pass "symlink target and personal-store canary remain untouched"

REMOTE_HOME="$TEST_HOME/standalone-node-home"
REMOTE_STATE="$REMOTE_HOME/.local/share/secretary"
REMOTE_NODE_DATA="$REMOTE_STATE/node/data"
mkdir -p "$REMOTE_HOME/.local/bin"
ln -s "$BIN/secretary-node" "$REMOTE_HOME/.local/bin/secretary-node"
output="$(HOME="$REMOTE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node setup --standalone --server https://secretary.example.invalid --name remote-node 2>&1)" || fail "standalone remote Node setup failed"
assert_contains "$output" "Node setup complete: remote-node" "standalone Node setup output"
assert_contains "$(cat "$REMOTE_STATE/node/config.json")" '"standalone": true' "standalone Node scope was not persisted"
remote_manifest="$(cat "$REMOTE_NODE_DATA/opencode-native-selection.json")"
assert_contains "$remote_manifest" '"mode": "isolated"' "remote Node did not select its own isolated store"
assert_contains "$remote_manifest" "\"data_home\": \"${REMOTE_NODE_DATA:a}/opencode-native\"" "remote Node selected a non-local data home"
[[ ! -e "$REMOTE_STATE/opencode-native-selection.json" ]] || fail "standalone Node created a local Secretary store selection"
output="$(HOME="$REMOTE_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SECRETARY_CLI" node install-service 2>&1)" || fail "standalone Node service installation failed"
assert_contains "$output" "Node will start after macOS login" "standalone Node service output"
[[ ! -e "$REMOTE_STATE/config.toml" && ! -e "$REMOTE_STATE/opencode-native-selection.json" ]] || fail "remote Node service installation created a local Secretary installation"
pass "standalone remote Node keeps its own native store"

output="$($SECRETARY_CLI status)" || fail "final status command failed"
assert_contains "$output" "stopped" "final status"
pass "final stopped status"

print "All secretary CLI command tests passed."
